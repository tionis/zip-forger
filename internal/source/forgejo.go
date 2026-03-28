package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"zip-forger/internal/filter"
)

type ForgejoConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	TreeDB     TreeDB
}

type TreeDB interface {
	IsIndexed(ctx context.Context, sha string) (bool, error)
	MarkIndexed(ctx context.Context, sha string) error
	SaveEntries(ctx context.Context, parentSHA string, entries []struct {
		Path string
		Type string
		Size int64
		SHA  string
	}) error
	GetFullTree(ctx context.Context, rootSHA string) ([]Entry, error)
	Search(ctx context.Context, rootSHA string, criteria filter.Criteria) ([]Entry, error)
}

type Forgejo struct {
	baseURL string
	client  *http.Client
	db      TreeDB
}

func NewForgejo(cfg ForgejoConfig) (*Forgejo, error) {
	baseURL := strings.TrimSpace(strings.TrimSuffix(cfg.BaseURL, "/"))
	if baseURL == "" {
		return nil, errors.New("source: forgejo base URL is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}

	return &Forgejo{
		baseURL: baseURL,
		client:  client,
		db:      cfg.TreeDB,
	}, nil
}

func (s *Forgejo) ResolveRef(ctx context.Context, owner, repo, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		defaultBranch, err := s.getDefaultBranch(ctx, owner, repo)
		if err != nil {
			return "", err
		}
		ref = defaultBranch
	}

	sha, err := s.getCommitSHA(ctx, owner, repo, ref)
	if err == nil {
		return sha, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	// Fallback for instances that do not expose commit lookup as expected.
	tree, treeErr := s.getTree(ctx, owner, repo, ref, false)
	if treeErr != nil {
		return "", treeErr
	}
	if strings.TrimSpace(tree.SHA) == "" {
		return "", fmt.Errorf("source: unable to resolve ref %q", ref)
	}
	return tree.SHA, nil
}

func (s *Forgejo) ReadFile(ctx context.Context, owner, repo, commit, filePath string) ([]byte, error) {
	reader, err := s.OpenFile(ctx, owner, repo, commit, filePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (s *Forgejo) ListFiles(ctx context.Context, owner, repo, commit string, criteria filter.Criteria) ([]Entry, error) {
	return s.listFilesByTrees(ctx, owner, repo, commit, criteria)
}

func (s *Forgejo) OpenFile(ctx context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
	relativePath := normalizePath(filePath)
	if relativePath == "" {
		return nil, errors.New("source: file path is required")
	}

	query := url.Values{}
	query.Set("ref", commit)
	rawURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/raw/%s?%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapePath(relativePath),
		query.Encode(),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	s.addAuthHeader(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, ErrUnauthorized
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("source: forgejo raw read failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	reader := bufio.NewReaderSize(resp.Body, 8<<10)
	pointer, isPointer := parseLFSPointer(reader)
	if !isPointer {
		return struct {
			io.Reader
			io.Closer
		}{
			Reader: reader,
			Closer: resp.Body,
		}, nil
	}

	resp.Body.Close()
	return s.downloadLFSObject(ctx, owner, repo, pointer.OID, pointer.Size)
}

func (s *Forgejo) SearchRepos(ctx context.Context, query string) ([]string, error) {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	q.Set("limit", "50")
	endpoint := s.baseURL + "/api/v1/repos/search?" + q.Encode()

	var payload struct {
		Data []struct {
			FullName string `json:"full_name"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(payload.Data))
	for _, repo := range payload.Data {
		if repo.FullName != "" {
			out = append(out, repo.FullName)
		}
	}
	return out, nil
}

func (s *Forgejo) ListBranches(ctx context.Context, owner, repo string) ([]string, error) {
	const pageLimit = 100
	page := 1
	out := make([]string, 0, pageLimit)
	seen := make(map[string]struct{})

	for {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(pageLimit))
		endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches?%s",
			s.baseURL,
			url.PathEscape(owner),
			url.PathEscape(repo),
			query.Encode(),
		)

		var payload []struct {
			Name string `json:"name"`
		}
		if err := s.getJSON(ctx, endpoint, &payload); err != nil {
			return nil, err
		}
		for _, branch := range payload {
			name := strings.TrimSpace(branch.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
		if len(payload) < pageLimit {
			break
		}
		page++
	}

	sort.Strings(out)
	return out, nil
}

func (s *Forgejo) UpsertFile(ctx context.Context, owner, repo, branch, filePath string, data []byte, message string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	filePath = normalizePath(filePath)
	if filePath == "" {
		return errors.New("source: file path is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "chore(zip-forger): update " + filePath
	}

	sha, err := s.getFileSHA(ctx, owner, repo, branch, filePath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, ErrNotFound) {
		sha = ""
	}

	requestPayload := map[string]any{
		"branch":  branch,
		"message": message,
		"content": base64.StdEncoding.EncodeToString(data),
	}
	method := http.MethodPost
	if sha != "" {
		requestPayload["sha"] = sha
		method = http.MethodPut
	}
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapePath(filePath),
	)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	s.addAuthHeader(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	}
	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("source: forgejo upsert failed method=%s status=%d body=%q", method, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

type lfsPointer struct {
	OID  string
	Size int64
}

func parseLFSPointer(reader *bufio.Reader) (lfsPointer, bool) {
	peek, err := reader.Peek(8 << 10)
	if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
		return lfsPointer{}, false
	}

	text := string(peek)
	if !strings.HasPrefix(text, "version https://git-lfs.github.com/spec/v1") {
		return lfsPointer{}, false
	}

	var pointer lfsPointer
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "oid sha256:"):
			pointer.OID = strings.TrimPrefix(line, "oid sha256:")
		case strings.HasPrefix(line, "size "):
			size, parseErr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "size ")), 10, 64)
			if parseErr == nil {
				pointer.Size = size
			}
		}
	}

	if pointer.OID == "" {
		return lfsPointer{}, false
	}
	return pointer, true
}

func (s *Forgejo) downloadLFSObject(ctx context.Context, owner, repo, oid string, size int64) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("%s/%s/%s.git/info/lfs/objects/batch",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
	)

	body, err := json.Marshal(map[string]any{
		"operation": "download",
		"transfers": []string{"basic"},
		"objects": []map[string]any{
			{
				"oid":  oid,
				"size": size,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/vnd.git-lfs+json")
	req.Header.Set("Accept", "application/vnd.git-lfs+json")
	s.addAuthHeader(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	}
	if resp.StatusCode >= http.StatusBadRequest {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("source: lfs batch request failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	var batch struct {
		Objects []struct {
			OID   string `json:"oid"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Actions map[string]struct {
				Href   string            `json:"href"`
				Header map[string]string `json:"header"`
			} `json:"actions"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return nil, fmt.Errorf("source: failed to decode lfs batch response: %w", err)
	}

	var object *struct {
		OID   string `json:"oid"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Actions map[string]struct {
			Href   string            `json:"href"`
			Header map[string]string `json:"header"`
		} `json:"actions"`
	}
	for idx := range batch.Objects {
		if batch.Objects[idx].OID == oid {
			object = &batch.Objects[idx]
			break
		}
	}
	if object == nil {
		return nil, errors.New("source: lfs batch response did not include object")
	}
	if object.Error != nil {
		return nil, fmt.Errorf("source: lfs object error code=%d message=%q", object.Error.Code, object.Error.Message)
	}

	downloadAction, ok := object.Actions["download"]
	if !ok || strings.TrimSpace(downloadAction.Href) == "" {
		return nil, errors.New("source: lfs download action missing")
	}

	downloadURL := downloadAction.Href
	if parsed, parseErr := url.Parse(downloadURL); parseErr == nil && !parsed.IsAbs() {
		base, _ := url.Parse(s.baseURL + "/")
		downloadURL = base.ResolveReference(parsed).String()
	}

	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range downloadAction.Header {
		downloadReq.Header.Set(key, value)
	}

	downloadResp, err := s.client.Do(downloadReq)
	if err != nil {
		return nil, err
	}
	if downloadResp.StatusCode == http.StatusNotFound {
		downloadResp.Body.Close()
		return nil, ErrNotFound
	}
	if downloadResp.StatusCode == http.StatusUnauthorized || downloadResp.StatusCode == http.StatusForbidden {
		downloadResp.Body.Close()
		return nil, ErrUnauthorized
	}
	if downloadResp.StatusCode >= http.StatusBadRequest {
		errBody, _ := io.ReadAll(io.LimitReader(downloadResp.Body, 4<<10))
		downloadResp.Body.Close()
		return nil, fmt.Errorf("source: lfs download failed status=%d body=%q", downloadResp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	return downloadResp.Body, nil
}

func (s *Forgejo) getDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s", s.baseURL, url.PathEscape(owner), url.PathEscape(repo))

	var payload struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.DefaultBranch) == "" {
		return "", errors.New("source: forgejo repo endpoint did not return default_branch")
	}
	return payload.DefaultBranch, nil
}

func (s *Forgejo) getCommitSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("source: ref is required")
	}

	branchSHA, err := s.getBranchCommitSHA(ctx, owner, repo, ref)
	if err == nil {
		return branchSHA, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	return s.getCommitSHAByQuery(ctx, owner, repo, ref)
}

func (s *Forgejo) getBranchCommitSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(branch),
	)
	var payload struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return "", err
	}

	sha := strings.TrimSpace(payload.Commit.ID)
	if sha == "" {
		return "", errors.New("source: branch lookup returned empty commit id")
	}
	return sha, nil
}

func (s *Forgejo) getCommitSHAByQuery(ctx context.Context, owner, repo, ref string) (string, error) {
	query := url.Values{}
	query.Set("sha", ref)
	query.Set("page", "1")
	query.Set("limit", "1")
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits?%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		query.Encode(),
	)

	var payload []struct {
		SHA string `json:"sha"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return "", err
	}
	if len(payload) == 0 {
		return "", ErrNotFound
	}

	sha := strings.TrimSpace(payload[0].SHA)
	if sha == "" {
		return "", errors.New("source: commit query returned empty sha")
	}
	return sha, nil
}

type treeResponse struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

func (s *Forgejo) getTree(ctx context.Context, owner, repo, ref string, recursive bool) (treeResponse, error) {
	query := url.Values{}
	if recursive {
		query.Set("recursive", "true")
	}

	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/trees/%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(ref),
	)
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	var payload treeResponse
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return treeResponse{}, err
	}
	return payload, nil
}

type treeTask struct {
	path   string
	sha    string
	isRoot bool
}

func (s *Forgejo) listFilesByTrees(ctx context.Context, owner, repo, commit string, criteria filter.Criteria) ([]Entry, error) {
	// 1. Resolve root tree SHA
	rootTree, err := s.getTree(ctx, owner, repo, commit, false)
	if err != nil {
		return nil, err
	}
	rootSHA := rootTree.SHA

	// 2. Check if this root tree is indexed
	if s.db != nil {
		if indexed, _ := s.db.IsIndexed(ctx, rootSHA); indexed {
			return s.db.Search(ctx, rootSHA, criteria)
		}
	}

	// 3. Concurrent Walk
	var (
		sem = make(chan struct{}, 20)
		count int64
	)

	g, ctx := errgroup.WithContext(ctx)
	var wg sync.WaitGroup
	
	var spawnTask func(task treeTask)
	spawnTask = func(task treeTask) {
		wg.Add(1)
		g.Go(func() error {
			defer wg.Done()
			
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return nil // Don't return ctx.Err() as it masks the real error
			}

			var currentTree treeResponse
			var fetchErr error
			if task.isRoot {
				currentTree = rootTree
			} else {
				// Use a dedicated timeout for each individual fetch
				fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				currentTree, fetchErr = s.getTree(fetchCtx, owner, repo, task.sha, false)
				if fetchErr != nil {
					return fmt.Errorf("fetch %s failed: %w", task.path, fetchErr)
				}
			}

			var entriesToSave []struct {
				Path string
				Type string
				Size int64
				SHA  string
			}

			for _, node := range currentTree.Tree {
				name := path.Base(normalizePath(node.Path))
				if name == "" || name == "." {
					continue
				}

				entriesToSave = append(entriesToSave, struct {
					Path string
					Type string
					Size int64
					SHA  string
				}{Path: name, Type: node.Type, Size: node.Size, SHA: node.SHA})

				fullPath := name
				if task.path != "" {
					fullPath = task.path + "/" + name
				}

				if node.Type == "blob" {
					c := atomic.AddInt64(&count, 1)
					if c%1000 == 0 {
						log.Printf("[INDEXER] Indexed %d files...", c)
					}
				} else if node.Type == "tree" && node.SHA != "" {
					spawnTask(treeTask{path: fullPath, sha: node.SHA, isRoot: false})
				}
			}

			if s.db != nil {
				_ = s.db.SaveEntries(ctx, task.sha, entriesToSave)
			}
			return nil
		})
	}

	spawnTask(treeTask{path: "", sha: rootSHA, isRoot: true})

	go func() {
		wg.Wait()
	}()

	if err := g.Wait(); err != nil {
		return nil, err
	}

	if s.db != nil {
		_ = s.db.MarkIndexed(ctx, rootSHA)
		// Now that we have indexed everything, return the filtered results from DB.
		return s.db.Search(ctx, rootSHA, criteria)
	}

	// Fallback if no DB: This path is technically impossible now as we always search DB at end,
	// but we need to return something. Since we didn't collect 'out' in memory anymore,
	// we just return empty if DB is missing (which shouldn't happen).
	return nil, nil
}

func (s *Forgejo) getJSON(ctx context.Context, endpoint string, into any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// Backoff before retry
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := s.doGetJSON(ctx, endpoint, into)
		if err == nil {
			return nil
		}
		
		// Don't retry on certain errors
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUnsupportedSearchMode) {
			return err
		}
		
		lastErr = err
	}
	return fmt.Errorf("after 3 attempts: %w", lastErr)
}

func (s *Forgejo) doGetJSON(ctx context.Context, endpoint string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	s.addAuthHeader(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if len(bytes.TrimSpace(body)) > 0 {
			return fmt.Errorf("%w: forgejo %d: %s", ErrUnauthorized, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return ErrUnauthorized
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return ErrUnsupportedSearchMode
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("source: forgejo request failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("source: decode failed: %w", err)
	}
	return nil
}

func (s *Forgejo) addAuthHeader(req *http.Request, ctx context.Context) {
	token, ok := AccessTokenFromContext(ctx)
	if !ok {
		return
	}
	// Use Bearer scheme for OAuth2 access tokens (Forgejo also accepts "token"
	// for PATs, but Bearer is correct for OAuth2 and works for both).
	req.Header.Set("Authorization", "Bearer "+token)
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func normalizePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	value = path.Clean("/" + value)
	value = strings.TrimPrefix(value, "/")
	if value == "." {
		return ""
	}
	return value
}

func (s *Forgejo) getFileSHA(ctx context.Context, owner, repo, branch, filePath string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s?ref=%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapePath(filePath),
		url.QueryEscape(branch),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	s.addAuthHeader(req, ctx)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrUnauthorized
	}
	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("source: forgejo get file metadata failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.SHA) == "" {
		return "", ErrNotFound
	}
	return payload.SHA, nil
}
