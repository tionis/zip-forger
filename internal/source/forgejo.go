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
	BaseURL      string
	HTTPClient   *http.Client
	TreeDB       TreeDB
	OnProgress   func(owner, repo, commit string, count int64)
	OnFinalizing func(owner, repo, commit string, count int64)
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
	baseURL      string
	client       *http.Client
	db           TreeDB
	onProgress   func(owner, repo, commit string, count int64)
	onFinalizing func(owner, repo, commit string, count int64)
}

type authScheme int

const (
	authSchemeNone authScheme = iota
	authSchemeBearer
	authSchemeToken
	authSchemeBasicOAuth2
)

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
		baseURL:      baseURL,
		client:       client,
		db:           cfg.TreeDB,
		onProgress:   cfg.OnProgress,
		onFinalizing: cfg.OnFinalizing,
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
	return s.openMediaReader(ctx, owner, repo, commit, filePath)
}

func (s *Forgejo) openMediaReader(ctx context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
	relativePath := normalizePath(filePath)
	if relativePath == "" {
		return nil, errors.New("source: file path is required")
	}

	query := url.Values{}
	query.Set("ref", commit)
	mediaURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/media/%s?%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapePath(relativePath),
		query.Encode(),
	)

	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		if len(bytes.TrimSpace(body)) > 0 {
			return nil, fmt.Errorf("%w: forgejo media read %s %d: %s", ErrUnauthorized, mediaURL, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil, fmt.Errorf("%w: forgejo media read %s %d", ErrUnauthorized, mediaURL, resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("source: forgejo media read failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func (s *Forgejo) ResolveEntrySizes(ctx context.Context, owner, repo, commit string, entries []Entry) ([]Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	out := make([]Entry, len(entries))
	copy(out, entries)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for idx := range out {
		if out[idx].Size > 8<<10 {
			continue
		}

		idx := idx
		group.Go(func() error {
			pointer, isPointer, err := s.detectLFSPointer(groupCtx, owner, repo, commit, out[idx].Path)
			if err != nil {
				return err
			}
			if isPointer && pointer.Size > 0 {
				out[idx].Size = pointer.Size
			}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Forgejo) openRawReader(ctx context.Context, owner, repo, commit, filePath string) (*bufio.Reader, io.ReadCloser, error) {
	relativePath := normalizePath(filePath)
	if relativePath == "" {
		return nil, nil, errors.New("source: file path is required")
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

	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		if len(bytes.TrimSpace(body)) > 0 {
			return nil, nil, fmt.Errorf("%w: forgejo raw read %s %d: %s", ErrUnauthorized, rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil, nil, fmt.Errorf("%w: forgejo raw read %s %d", ErrUnauthorized, rawURL, resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		return nil, nil, fmt.Errorf("source: forgejo raw read failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	reader := bufio.NewReaderSize(resp.Body, 8<<10)
	return reader, resp.Body, nil
}

func (s *Forgejo) detectLFSPointer(ctx context.Context, owner, repo, commit, filePath string) (lfsPointer, bool, error) {
	reader, body, err := s.openRawReader(ctx, owner, repo, commit, filePath)
	if err != nil {
		return lfsPointer{}, false, err
	}
	defer body.Close()

	pointer, isPointer := parseLFSPointer(reader)
	return pointer, isPointer, nil
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

func (s *Forgejo) UpsertFile(ctx context.Context, owner, repo, branch, filePath string, data []byte, message, sha string) error {
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
	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("source: forbidden (check OAuth scopes): %s", strings.TrimSpace(string(respBody)))
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

	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/vnd.git-lfs+json")
		req.Header.Set("Accept", "application/vnd.git-lfs+json")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if len(bytes.TrimSpace(errBody)) > 0 {
			return nil, fmt.Errorf("%w: forgejo lfs batch %s %d: %s", ErrUnauthorized, endpoint, resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
		return nil, fmt.Errorf("%w: forgejo lfs batch %s %d", ErrUnauthorized, endpoint, resp.StatusCode)
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

	downloadResp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return nil, err
		}
		for key, value := range downloadAction.Header {
			downloadReq.Header.Set(key, value)
		}
		return downloadReq, nil
	})
	if err != nil {
		return nil, err
	}
	if downloadResp.StatusCode == http.StatusNotFound {
		downloadResp.Body.Close()
		return nil, ErrNotFound
	}
	if downloadResp.StatusCode == http.StatusUnauthorized || downloadResp.StatusCode == http.StatusForbidden {
		errBody, _ := io.ReadAll(io.LimitReader(downloadResp.Body, 4<<10))
		downloadResp.Body.Close()
		if len(bytes.TrimSpace(errBody)) > 0 {
			return nil, fmt.Errorf("%w: forgejo lfs download %s %d: %s", ErrUnauthorized, downloadURL, downloadResp.StatusCode, strings.TrimSpace(string(errBody)))
		}
		return nil, fmt.Errorf("%w: forgejo lfs download %s %d", ErrUnauthorized, downloadURL, downloadResp.StatusCode)
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

	// 2. Check if this root tree is fully indexed
	if s.db != nil {
		if indexed, _ := s.db.IsIndexed(ctx, rootSHA); indexed {
			entries, err := s.db.Search(ctx, rootSHA, criteria)
			if err == nil && s.onProgress != nil {
				// Report the total discovered count from the cache
				s.onProgress(owner, repo, commit, int64(len(entries)))
			}
			return entries, err
		}
	}

	// 3. Worker Pool Setup
	const numWorkers = 5
	tasks := make(chan treeTask, 500000)

	var (
		count     int64
		firstErr  error
		errMu     sync.Once
		seenTrees sync.Map

		taskWG sync.WaitGroup
	)

	setFirstErr := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		errMu.Do(func() {
			firstErr = err
		})
	}

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-tasks:
					if !ok {
						return
					}

					func() {
						defer taskWG.Done()

						// Avoid re-indexing the same directory SHA
						if !task.isRoot {
							if _, loaded := seenTrees.LoadOrStore(task.sha, true); loaded {
								return
							}
						}

						var currentTree treeResponse
						if task.isRoot {
							currentTree = rootTree
						} else {
							fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
							defer cancel()
							var fetchErr error
							currentTree, fetchErr = s.getTree(fetchCtx, owner, repo, task.sha, false)
							if fetchErr != nil {
								setFirstErr(fetchErr)
								log.Printf("[INDEXER FATAL ERROR] Fetch failed for %s: %v", task.path, fetchErr)
								return
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
									if s.onProgress != nil {
										s.onProgress(owner, repo, commit, c)
									}
								}
							} else if node.Type == "tree" && node.SHA != "" {
								taskWG.Add(1)
								tasks <- treeTask{path: fullPath, sha: node.SHA, isRoot: false}
							}
						}

						if s.db != nil {
							if err := s.db.SaveEntries(ctx, task.sha, entriesToSave); err != nil {
								setFirstErr(err)
								log.Printf("[INDEXER DB ERROR] Failed to save entries for %s: %v", task.path, err)
							}
						}
					}()
				}
			}
		}()
	}

	// Queue the root task
	taskWG.Add(1)
	tasks <- treeTask{path: "", sha: rootSHA, isRoot: true}

	// Wait for all tasks to be finished and close channel
	go func() {
		taskWG.Wait()
		close(tasks)
	}()

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	if s.db != nil {
		if s.onFinalizing != nil {
			s.onFinalizing(owner, repo, commit, count)
		}
		_ = s.db.MarkIndexed(ctx, rootSHA)
		entries, err := s.db.Search(ctx, rootSHA, criteria)
		if err == nil && s.onProgress != nil {
			s.onProgress(owner, repo, commit, count)
		}
		return entries, err
	}

	if s.onProgress != nil {
		s.onProgress(owner, repo, commit, count)
	}
	return nil, nil
}

func (s *Forgejo) getJSON(ctx context.Context, endpoint string, into any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
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

		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUnsupportedSearchMode) {
			return err
		}

		lastErr = err
	}
	return fmt.Errorf("after 3 attempts: %w", lastErr)
}

func (s *Forgejo) doGetJSON(ctx context.Context, endpoint string, into any) error {
	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	})
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
			return fmt.Errorf("%w: forgejo request %s %d: %s", ErrUnauthorized, endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("%w: forgejo request %s %d", ErrUnauthorized, endpoint, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return ErrUnsupportedSearchMode
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("source: forgejo request failed (url: %s) status=%d body=%q", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("source: decode failed: %w", err)
	}
	return nil
}

func (s *Forgejo) doWithAuthFallback(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	token, ok := AccessTokenFromContext(ctx)
	schemes := []authScheme{authSchemeNone}
	if ok && strings.TrimSpace(token) != "" {
		schemes = []authScheme{
			authSchemeBearer,
			authSchemeToken,
			authSchemeBasicOAuth2,
		}
	}

	for idx, scheme := range schemes {
		req, err := build()
		if err != nil {
			return nil, err
		}
		s.applyAuthScheme(req, token, scheme)

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return resp, nil
		}
		if idx == len(schemes)-1 {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
	}

	return nil, errors.New("source: no auth schemes available")
}

func (s *Forgejo) applyAuthScheme(req *http.Request, token string, scheme authScheme) {
	switch scheme {
	case authSchemeBearer:
		req.Header.Set("Authorization", "Bearer "+token)
	case authSchemeToken:
		req.Header.Set("Authorization", "token "+token)
	case authSchemeBasicOAuth2:
		req.SetBasicAuth("oauth2", token)
	}
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

func (s *Forgejo) GetFileSHA(ctx context.Context, owner, repo, branch, filePath string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s?ref=%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		escapePath(filePath),
		url.QueryEscape(branch),
	)

	resp, err := s.doWithAuthFallback(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if len(bytes.TrimSpace(respBody)) > 0 {
			return "", fmt.Errorf("%w: forgejo metadata %s %d: %s", ErrUnauthorized, endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return "", fmt.Errorf("%w: forgejo metadata %s %d", ErrUnauthorized, endpoint, resp.StatusCode)
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
