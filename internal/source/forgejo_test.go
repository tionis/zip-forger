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
)

func TestForgejoResolveListAndOpen(t *testing.T) {
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
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

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/commit-sha" && r.URL.Query().Get("recursive") == "true":
					return response(http.StatusOK, `{
  "sha":"commit-sha",
  "truncated":false,
  "tree":[
    {"path":"rules/core","type":"tree"},
    {"path":"rules/core/guide.pdf","type":"blob","size":12},
    {"path":"rules/core/notes.txt","type":"blob","size":9}
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

	entries, err := client.ListFiles(ctx, "acme", "rules", commit)
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 blob entries, got %d", len(entries))
	}
	if entries[0].Path != "rules/core/guide.pdf" {
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
	client, err := NewForgejo(ForgejoConfig{
		BaseURL: "http://forgejo.local",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer tok-123" {
					return response(http.StatusUnauthorized, "missing token"), nil
				}

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/git/trees/commit-sha" && r.URL.Query().Get("recursive") == "true":
					return response(http.StatusOK, `{"sha":"commit-sha","truncated":true,"tree":[]}`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/contents" && r.URL.Query().Get("ref") == "commit-sha":
					return response(http.StatusOK, `[
  {"type":"dir","path":"rules"},
  {"type":"file","path":"README.md","size":4}
]`), nil

				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/rules/contents/rules" && r.URL.Query().Get("ref") == "commit-sha":
					return response(http.StatusOK, `[
  {"type":"file","path":"rules/core.pdf","size":12}
]`), nil
				}
				return response(http.StatusNotFound, `{"message":"not found"}`), nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewForgejo failed: %v", err)
	}

	ctx := WithAccessToken(context.Background(), "tok-123")
	entries, err := client.ListFiles(ctx, "acme", "rules", "commit-sha")
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries from contents fallback, got %d", len(entries))
	}
	if entries[0].Path != "README.md" {
		t.Fatalf("unexpected first path: %s", entries[0].Path)
	}
	if entries[1].Path != "rules/core.pdf" {
		t.Fatalf("unexpected second path: %s", entries[1].Path)
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
					switch r.URL.Query().Get("mode") {
					case "collaborative":
						return response(http.StatusOK, `{"data":[
  {"name":"collab-repo","owner":{"login":"third"}}
]}`), nil
					case "member":
						return response(http.StatusOK, `{"data":[
  {"name":"org-repo","owner":{"login":"myorg"}}
]}`), nil
					default:
						return response(http.StatusOK, `{"data":[
  {"name":"rules","owner":{"login":"acme"}},
  {"name":"notes","owner":{"login":"acme"}},
  {"name":"misc","owner":{"login":"other"}}
]}`), nil
					}

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
	owners, err := client.ListOwners(ctx)
	if err != nil {
		t.Fatalf("ListOwners failed: %v", err)
	}
	if len(owners) != 4 || owners[0] != "acme" || owners[1] != "myorg" || owners[2] != "other" || owners[3] != "third" {
		t.Fatalf("unexpected owners: %#v", owners)
	}

	repos, err := client.ListRepos(ctx, "acme")
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}
	if len(repos) != 2 || repos[0] != "notes" || repos[1] != "rules" {
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
