package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"zip-forger/internal/filter"
)

type mockTreeDB struct {
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
	return m.indexed[sha], nil
}

func (m *mockTreeDB) MarkIndexed(_ context.Context, sha string) error {
	m.indexed[sha] = true
	return nil
}

func (m *mockTreeDB) SaveEntries(_ context.Context, parentSHA string, entries []struct {
	Path string
	Type string
	Size int64
	SHA  string
}) error {
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
		entries := m.entries[sha]
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

	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		TreeDB:  db,
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

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/raw/rules/core/guide.pdf" && r.URL.Query().Get("ref") == "commit-sha":
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
	if err := client.UpsertFile(ctx, "acme", "rules", "main", ".zip-forger.yaml", configYAML, "update"); err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	if putPayload["branch"] != "main" {
		t.Fatalf("unexpected branch payload: %#v", putPayload)
	}
	if putPayload["sha"] != "existing-sha" {
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
	if err := client.UpsertFile(ctx, "acme", "rules", "main", ".zip-forger.yaml", configYAML, "create"); err != nil {
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

func TestForgejoOpenFileLFSFallback(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" && !strings.HasPrefix(r.URL.Path, "/lfs-download/") {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/raw/assets/book.pdf":
					return response(http.StatusOK, `version https://git-lfs.github.com/spec/v1
oid sha256:deadbeef
size 12345
`), nil

				case r.Method == http.MethodPost && r.URL.Path == "/acme/rules.git/info/lfs/objects/batch":
					defer r.Body.Close()
					raw, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(raw), `"oid":"deadbeef"`) {
						return response(http.StatusBadRequest, `{"message":"missing oid"}`), nil
					}
					return response(http.StatusOK, `{
  "objects":[
    {
      "oid":"deadbeef",
      "actions":{
        "download":{
          "href":"http://forgejo.local/lfs-download/deadbeef",
          "header":{"X-Test":"1"}
        }
      }
    }
  ]
}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/lfs-download/deadbeef":
					if r.Header.Get("X-Test") != "1" {
						return response(http.StatusForbidden, "missing header"), nil
					}
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
		t.Fatalf("unexpected LFS fallback content: %q", string(body))
	}
}

func TestForgejoLFSRejectsOIDMismatch(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/raw/assets/big.bin":
					return response(http.StatusOK, "version https://git-lfs.github.com/spec/v1\noid sha256:aaaa1111\nsize 999\n"), nil

				case r.Method == http.MethodPost && r.URL.Path == "/acme/rules.git/info/lfs/objects/batch":
					// Return an object with a DIFFERENT OID than requested
					return response(http.StatusOK, `{
  "objects":[
    {
      "oid":"wrong-oid-bbbb2222",
      "actions":{
        "download":{
          "href":"http://forgejo.local/lfs-download/wrong",
          "header":{}
        }
      }
    }
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
	_, err = client.OpenFile(ctx, "acme", "rules", "main", "assets/big.bin")
	if err == nil {
		t.Fatal("expected error when LFS batch returns mismatched OID, got nil")
	}
	if !strings.Contains(err.Error(), "did not include object") {
		t.Fatalf("expected 'did not include object' error, got: %v", err)
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
