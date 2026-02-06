package source

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ForgejoConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

type Forgejo struct {
	baseURL string
	client  *http.Client
}

type repoSummary struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func NewForgejo(cfg ForgejoConfig) (*Forgejo, error) {
	baseURL := strings.TrimSpace(strings.TrimSuffix(cfg.BaseURL, "/"))
	if baseURL == "" {
		return nil, errors.New("source: forgejo base URL is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	return &Forgejo{
		baseURL: baseURL,
		client:  client,
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

func (s *Forgejo) ListFiles(ctx context.Context, owner, repo, commit string) ([]Entry, error) {
	tree, err := s.getTree(ctx, owner, repo, commit, true)
	if err != nil {
		return nil, err
	}

	if !tree.Truncated {
		entries := make([]Entry, 0, len(tree.Tree))
		for _, node := range tree.Tree {
			if node.Type != "blob" {
				continue
			}
			p := normalizePath(node.Path)
			if p == "" {
				continue
			}
			entries = append(entries, Entry{
				Path: p,
				Size: node.Size,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Path < entries[j].Path
		})
		return entries, nil
	}

	// Recursive tree endpoint can truncate for large repositories; walk directories instead.
	return s.listFilesByContents(ctx, owner, repo, commit)
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
	return resp.Body, nil
}

func (s *Forgejo) ListOwners(ctx context.Context) ([]string, error) {
	repos, err := s.listUserRepos(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(repos))
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		owner := strings.TrimSpace(repo.Owner.Login)
		if owner == "" {
			continue
		}
		if _, ok := seen[owner]; ok {
			continue
		}
		seen[owner] = struct{}{}
		out = append(out, owner)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Forgejo) ListRepos(ctx context.Context, owner string) ([]string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, nil
	}

	repos, err := s.listUserRepos(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		if !strings.EqualFold(strings.TrimSpace(repo.Owner.Login), owner) {
			continue
		}
		name := strings.TrimSpace(repo.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
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
	if sha != "" {
		requestPayload["sha"] = sha
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
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
		return fmt.Errorf("source: forgejo upsert failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
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
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/commits/%s",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(ref),
	)
	var payload struct {
		SHA string `json:"sha"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return "", err
	}
	payload.SHA = strings.TrimSpace(payload.SHA)
	if payload.SHA == "" {
		return "", errors.New("source: commit lookup returned empty SHA")
	}
	return payload.SHA, nil
}

type treeResponse struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
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

type contentItem struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (s *Forgejo) listFilesByContents(ctx context.Context, owner, repo, commit string) ([]Entry, error) {
	queue := []string{""}
	out := make([]Entry, 0, 1024)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		items, err := s.getDirectoryContents(ctx, owner, repo, commit, current)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			switch item.Type {
			case "dir":
				if item.Path != "" {
					queue = append(queue, item.Path)
				}
			case "file":
				filePath := normalizePath(item.Path)
				if filePath != "" {
					out = append(out, Entry{
						Path: filePath,
						Size: item.Size,
					})
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func (s *Forgejo) getDirectoryContents(ctx context.Context, owner, repo, commit, dir string) ([]contentItem, error) {
	dir = normalizePath(dir)
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents",
		s.baseURL,
		url.PathEscape(owner),
		url.PathEscape(repo),
	)
	if dir != "" {
		endpoint += "/" + escapePath(dir)
	}
	query := url.Values{}
	query.Set("ref", commit)
	endpoint += "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("source: forgejo contents request failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	rawTrim := strings.TrimSpace(string(raw))
	if rawTrim == "" {
		return nil, nil
	}

	if strings.HasPrefix(rawTrim, "[") {
		var items []contentItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("source: unable to decode directory contents: %w", err)
		}
		return items, nil
	}

	var one contentItem
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("source: unable to decode contents response: %w", err)
	}
	// A single-object response means the path is a file; return it as a one-item list.
	return []contentItem{one}, nil
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

func (s *Forgejo) listUserRepos(ctx context.Context) ([]repoSummary, error) {
	const pageLimit = 100
	page := 1
	out := make([]repoSummary, 0, pageLimit)

	for {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(pageLimit))
		endpoint := s.baseURL + "/api/v1/user/repos?" + query.Encode()

		var payload []repoSummary
		if err := s.getJSON(ctx, endpoint, &payload); err != nil {
			return nil, err
		}
		out = append(out, payload...)
		if len(payload) < pageLimit {
			break
		}
		page++
	}
	return out, nil
}

func (s *Forgejo) getJSON(ctx context.Context, endpoint string, into any) error {
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
		return ErrUnauthorized
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
	// Forgejo accepts OAuth/PAT tokens via the legacy "token" scheme.
	req.Header.Set("Authorization", "token "+token)
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
