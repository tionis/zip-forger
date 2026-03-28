package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"zip-forger/internal/filter"
)

type mockTreeDB struct {
	mu      sync.RWMutex
	indexed map[string]bool
	entries map[string][]struct {
		Path string
		Type string
		Size int64
		SHA  string
	}
}

func newMockTreeDB() *mockTreeDB {
	return &mockTreeDB{
		indexed: make(map[string]bool),
		entries: make(map[string][]struct {
			Path string
			Type string
			Size int64
			SHA  string
		}),
	}
}

func (m *mockTreeDB) IsIndexed(_ context.Context, sha string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.indexed[sha], nil
}

func (m *mockTreeDB) MarkIndexed(_ context.Context, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexed[sha] = true
	return nil
}

func (m *mockTreeDB) SaveEntries(_ context.Context, parentSHA string, entries []struct {
	Path string
	Type string
	Size int64
	SHA  string
}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[parentSHA] = entries
	return nil
}

func (m *mockTreeDB) GetFullTree(_ context.Context, rootSHA string) ([]Entry, error) {
	return m.Search(context.Background(), rootSHA, filter.Criteria{})
}

func (m *mockTreeDB) Search(_ context.Context, rootSHA string, criteria filter.Criteria) ([]Entry, error) {
	var out []Entry
	var walk func(sha string, prefix string)
	walk = func(sha string, prefix string) {
		m.mu.RLock()
		entries := m.entries[sha]
		m.mu.RUnlock()
		for _, e := range entries {
			fullPath := e.Path
			if prefix != "" {
				fullPath = prefix + "/" + e.Path
			}

			if e.Type == "blob" {
				// Simple mock filter logic
				matches := true
				if len(criteria.Extensions) > 0 {
					matches = false
					for _, ext := range criteria.Extensions {
						if strings.HasSuffix(fullPath, ext) {
							matches = true
							break
						}
					}
				}
				if matches && len(criteria.PathPrefixes) > 0 {
					matches = false
					for _, p := range criteria.PathPrefixes {
						if strings.HasPrefix(fullPath, p) {
							matches = true
							break
						}
					}
				}

				if matches {
					out = append(out, Entry{Path: fullPath, Size: e.Size})
				}
			} else if e.Type == "tree" {
				walk(e.SHA, fullPath)
			}
		}
	}
	walk(rootSHA, "")
	return out, nil
}

func TestForgejoResolveListAndOpen(t *testing.T) {
	db := newMockTreeDB()
	progressCount := int64(0)
	finalizingCount := int64(0)

	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		TreeDB:  db,
		OnProgress: func(owner, repo, commit string, count int64) {
			if owner != "acme" || repo != "rules" || commit != "commit-sha" {
				t.Fatalf("unexpected progress notification owner=%s repo=%s commit=%s count=%d", owner, repo, commit, count)
			}
			progressCount = count
		},
		OnFinalizing: func(owner, repo, commit string, count int64) {
			if owner != "acme" || repo != "rules" || commit != "commit-sha" {
				t.Fatalf("unexpected finalizing notification owner=%s repo=%s commit=%s count=%d", owner, repo, commit, count)
			}
			finalizingCount = count
		},
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules":
					return response(http.StatusOK, `{"default_branch":"main"}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/branches/main":
					return response(http.StatusOK, `{"commit":{"id":"commit-sha"}}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/commit-sha" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"commit-sha",
  "truncated":false,
  "tree":[
    {"path":"rules","type":"tree", "sha":"sub-sha-rules"}
  ]
}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/sub-sha-rules" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"sub-sha-rules",
  "truncated":false,
  "tree":[
    {"path":"core","type":"tree", "sha":"sub-sha-core"}
  ]
}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/sub-sha-core" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"sub-sha-core",
  "truncated":false,
  "tree":[
    {"path":"docs","type":"tree", "sha":"sub-sha-docs"},
    {"path":"guide.pdf","type":"blob","size":12},
    {"path":"notes.txt","type":"blob","size":9}
  ]
}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/sub-sha-docs" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"sub-sha-docs",
  "truncated":false,
  "tree":[
    {"path":"manual.pdf","type":"blob","size":100}
  ]
}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/media/rules/core/guide.pdf" && r.URL.Query().Get("ref") == "commit-sha":
					return response(http.StatusOK, "pdf-content"), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	commit, err := client.ResolveRef(ctx, "acme", "rules", "")
	if err != nil {
		t.Fatalf("ResolveRef failed: %v", err)
	}
	if commit != "commit-sha" {
		t.Fatalf("unexpected commit: %s", commit)
	}

	entries, err := client.ListFiles(ctx, "acme", "rules", commit, filter.Criteria{})
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 blob entries, got %d", len(entries))
	}
	if progressCount != 3 {
		t.Fatalf("expected final progress count 3, got %d", progressCount)
	}
	if finalizingCount != 3 {
		t.Fatalf("expected finalizing count 3, got %d", finalizingCount)
	}
	if entries[0].Path != "README.md" && entries[0].Path != "rules/core/docs/manual.pdf" {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}

	reader, err := client.OpenFile(ctx, "acme", "rules", commit, "rules/core/guide.pdf")
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	if strings.TrimSpace(string(body)) != "pdf-content" {
		t.Fatalf("unexpected file contents: %q", string(body))
	}
}

func TestForgejoUnauthorized(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				return response(http.StatusUnauthorized, "nope"), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	_, err = client.ResolveRef(context.Background(), "acme", "rules", "main")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestForgejoResolveRefFallsBackToCommitQuery(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/branches/release-2024":
					return response(http.StatusNotFound, `{"message":"branch not found"}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/commits":
					query := r.URL.Query()
					if query.Get("sha") != "release-2024" || query.Get("page") != "1" || query.Get("limit") != "1" {
						return response(http.StatusBadRequest, `{"message":"unexpected query"}`), nil
					}
					return response(http.StatusOK, `[{"sha":"commit-from-query"}]`), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	commit, err := client.ResolveRef(ctx, "acme", "rules", "release-2024")
	if err != nil {
		t.Fatalf("ResolveRef failed: %v", err)
	}
	if commit != "commit-from-query" {
		t.Fatalf("unexpected commit: %q", commit)
	}
}

func TestForgejoListFilesFallbackWhenTreeTruncated(t *testing.T) {
	db := newMockTreeDB()

	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		TreeDB:  db,
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				// Initial root tree fetch (always non-recursive)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/commit-sha" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"commit-sha",
  "truncated":false,
  "tree":[
    {"path":"rules","type":"tree","sha":"rules-sha"},
    {"path":"README.md","type":"blob","size":4}
  ]
}`), nil

				// Sub-tree fetch (now strictly non-recursive)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/rules-sha" && r.URL.Query().Get("recursive") == "":
					return response(http.StatusOK, `{
  "sha":"rules-sha",
  "truncated":false,
  "tree":[
    {"path":"core.pdf","type":"blob","size":12}
  ]
}`), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	entries, err := client.ListFiles(ctx, "acme", "rules", "commit-sha", filter.Criteria{})
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	// We expect 2 blobs: README.md and rules/core.pdf
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries from iterative discovery, got %d: %#v", len(entries), entries)
	}
}
func TestForgejoListAndUpsertHelpers(t *testing.T) {
	var putPayload map[string]any
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/search":
					return response(http.StatusOK, `{"data":[
  {"full_name":"acme/rules"},
  {"full_name":"acme/notes"},
  {"full_name":"other/misc"}
]}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/branches":
					return response(http.StatusOK, `[
  {"name":"main"},
  {"name":"develop"}
]`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/contents/.zip-forger.yaml":
					return response(http.StatusOK, `{"sha":"existing-sha"}`), nil

				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/repos/acme/rules/contents/.zip-forger.yaml":
					defer r.Body.Close()
					raw, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(raw, &putPayload)
					return response(http.StatusCreated, `{"ok":true}`), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	repos, err := client.SearchRepos(ctx, "")
	if err != nil {
		t.Fatalf("SearchRepos failed: %v", err)
	}
	if len(repos) != 3 || repos[0] != "acme/rules" || repos[1] != "acme/notes" || repos[2] != "other/misc" {
		t.Fatalf("unexpected repos: %#v", repos)
	}

	branches, err := client.ListBranches(ctx, "acme", "rules")
	if err != nil {
		t.Fatalf("ListBranches failed: %v", err)
	}
	if len(branches) != 2 || branches[0] != "develop" || branches[1] != "main" {
		t.Fatalf("unexpected branches: %#v", branches)
	}

	configYAML := []byte("version: 1\n")
	if err := client.UpsertFile(ctx, "acme", "rules", "main", ".zip-forger.yaml", configYAML, "update", "current-sha"); err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	if putPayload["branch"] != "main" {
		t.Fatalf("unexpected branch payload: %#v", putPayload)
	}
	if putPayload["sha"] != "current-sha" {
		t.Fatalf("unexpected sha payload: %#v", putPayload)
	}
	content, _ := putPayload["content"].(string)
	if content != base64.StdEncoding.EncodeToString(configYAML) {
		t.Fatalf("unexpected base64 payload content: %q", content)
	}
}

func TestForgejoUpsertFileCreatesWithPost(t *testing.T) {
	var postPayload map[string]any
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/contents/.zip-forger.yaml":
					return response(http.StatusNotFound, `{"message":"not found"}`), nil

				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/rules/contents/.zip-forger.yaml":
					defer r.Body.Close()
					raw, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(raw, &postPayload)
					return response(http.StatusCreated, `{"ok":true}`), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	configYAML := []byte("version: 1\n")
	if err := client.UpsertFile(ctx, "acme", "rules", "main", ".zip-forger.yaml", configYAML, "create", ""); err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	if postPayload["branch"] != "main" {
		t.Fatalf("unexpected branch payload: %#v", postPayload)
	}
	if _, ok := postPayload["sha"]; ok {
		t.Fatalf("create payload should not include sha: %#v", postPayload)
	}
	content, _ := postPayload["content"].(string)
	if content != base64.StdEncoding.EncodeToString(configYAML) {
		t.Fatalf("unexpected base64 payload content: %q", content)
	}
}

func TestForgejoOpenFileUsesMediaEndpoint(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/media/assets/book.pdf" && r.URL.Query().Get("ref") == "main":
					return response(http.StatusOK, "real-binary-content"), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	reader, err := client.OpenFile(ctx, "acme", "rules", "main", "assets/book.pdf")
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if strings.TrimSpace(string(body)) != "real-binary-content" {
		t.Fatalf("unexpected media content: %q", string(body))
	}
}

func TestForgejoOpenFileMediaFallsBackToTokenAuthForLegacyServers(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/media/assets/book.pdf" && r.URL.Query().Get("ref") == "main":
					if r.Header.Get("Authorization") != "Bearer tok-123" {
						if r.Header.Get("Authorization") == "token tok-123" {
							return response(http.StatusOK, "real-binary-content"), nil
						}
						return response(http.StatusUnauthorized, "missing auth"), nil
					}
					return response(http.StatusUnauthorized, "bearer not accepted for legacy media"), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	reader, err := client.OpenFile(ctx, "acme", "rules", "main", "assets/book.pdf")
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if strings.TrimSpace(string(body)) != "real-binary-content" {
		t.Fatalf("unexpected media content: %q", string(body))
	}
}

func TestForgejoResolveEntrySizesUsesLFSPointerSize(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/raw/docs/manual.pdf" && r.URL.Query().Get("ref") == "commit-sha":
					return response(http.StatusOK, strings.Join([]string{
						"version https://git-lfs.github.com/spec/v1",
						"oid sha256:deadbeef",
						"size 424242",
						"",
					}, "\n")), nil
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/raw/docs/readme.txt" && r.URL.Query().Get("ref") == "commit-sha":
					return response(http.StatusOK, "plain text"), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	entries, err := client.ResolveEntrySizes(ctx, "acme", "rules", "commit-sha", []Entry{
		{Path: "docs/manual.pdf", Size: 128},
		{Path: "docs/readme.txt", Size: 10},
		{Path: "docs/already-large.bin", Size: 9000},
	})
	if err != nil {
		t.Fatalf("ResolveEntrySizes failed: %v", err)
	}
	if entries[0].Size != 424242 {
		t.Fatalf("expected LFS pointer size 424242, got %d", entries[0].Size)
	}
	if entries[1].Size != 10 {
		t.Fatalf("expected regular file size to remain 10, got %d", entries[1].Size)
	}
	if entries[2].Size != 9000 {
		t.Fatalf("expected large file size to remain unchanged, got %d", entries[2].Size)
	}
}

func TestForgejoOpenFileMediaNotFound(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	_, err = client.OpenFile(ctx, "acme", "rules", "main", "assets/missing.bin")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
