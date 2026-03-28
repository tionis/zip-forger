package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"zip-forger/internal/auth"
	"zip-forger/internal/cache"
	"zip-forger/internal/filter"
	"zip-forger/internal/source"
)

func TestPreviewAndDownload(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", ".zip-forger.yaml"), []byte(`
version: 1
options:
  allowAdhocFilters: false
presets:
  - id: core-pdfs
    includeGlobs: ["rules/core/**/*.pdf"]
`))
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", "rules", "core", "guide.pdf"), []byte("pdf-content"))
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", "rules", "core", "notes.txt"), []byte("txt-content"))

	server := NewServer(Dependencies{
		Source:        newTestFSSource(root),
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})
	handler := server.Handler()

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResp := httptest.NewRecorder()
	handler.ServeHTTP(rootResp, rootReq)
	if rootResp.Code != http.StatusOK {
		t.Fatalf("root status=%d body=%s", rootResp.Code, rootResp.Body.String())
	}
	if !strings.Contains(rootResp.Body.String(), "zip-forger") {
		t.Fatalf("expected ui response to contain app title")
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/repos/search", nil)
	searchResp := httptest.NewRecorder()
	handler.ServeHTTP(searchResp, searchReq)
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", searchResp.Code, searchResp.Body.String())
	}
	var searchPayload struct {
		Repos []string `json:"repos"`
	}
	if err := json.Unmarshal(searchResp.Body.Bytes(), &searchPayload); err != nil {
		t.Fatalf("search json decode failed: %v", err)
	}
	if len(searchPayload.Repos) != 1 || searchPayload.Repos[0] != "acme/rules" {
		t.Fatalf("unexpected search payload: %#v", searchPayload)
	}

	branchesReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/branches", nil)
	branchesResp := httptest.NewRecorder()
	handler.ServeHTTP(branchesResp, branchesReq)
	if branchesResp.Code != http.StatusOK {
		t.Fatalf("branches status=%d body=%s", branchesResp.Code, branchesResp.Body.String())
	}
	var branchesPayload struct {
		Branches []string `json:"branches"`
	}
	if err := json.Unmarshal(branchesResp.Body.Bytes(), &branchesPayload); err != nil {
		t.Fatalf("branches json decode failed: %v", err)
	}
	if len(branchesPayload.Branches) != 1 || branchesPayload.Branches[0] != "main" {
		t.Fatalf("unexpected branches payload: %#v", branchesPayload)
	}

	previewReq := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main","preset":"core-pdfs"}`))
	previewResp := httptest.NewRecorder()
	handler.ServeHTTP(previewResp, previewReq)

	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResp.Code, previewResp.Body.String())
	}

	var preview struct {
		SelectedFiles int      `json:"selectedFiles"`
		Entries       []string `json:"entries"`
	}
	if err := json.Unmarshal(previewResp.Body.Bytes(), &preview); err != nil {
		t.Fatalf("preview json decode failed: %v", err)
	}
	if preview.SelectedFiles != 1 {
		t.Fatalf("expected selectedFiles=1, got %d", preview.SelectedFiles)
	}
	if len(preview.Entries) != 1 || preview.Entries[0] != "rules/core/guide.pdf" {
		t.Fatalf("unexpected preview entries: %#v", preview.Entries)
	}

	adhocReq := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main","adhoc":{"extensions":[".pdf"]}}`))
	adhocResp := httptest.NewRecorder()
	handler.ServeHTTP(adhocResp, adhocReq)
	if adhocResp.Code != http.StatusForbidden {
		t.Fatalf("expected adhoc disable status=403, got %d body=%s", adhocResp.Code, adhocResp.Body.String())
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/config?ref=main", nil)
	configResp := httptest.NewRecorder()
	handler.ServeHTTP(configResp, configReq)
	if configResp.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", configResp.Code, configResp.Body.String())
	}

	var cfgPayload struct {
		Commit string `json:"commit"`
		Config struct {
			Options struct {
				AllowAdhocFilters bool `json:"allowAdhocFilters"`
			} `json:"options"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configResp.Body.Bytes(), &cfgPayload); err != nil {
		t.Fatalf("config json decode failed: %v", err)
	}
	if cfgPayload.Commit != "main" {
		t.Fatalf("unexpected commit from config endpoint: %q", cfgPayload.Commit)
	}
	if cfgPayload.Config.Options.AllowAdhocFilters {
		t.Fatalf("expected allowAdhocFilters=false")
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/repos/acme/rules/config", bytes.NewBufferString(`{
  "ref":"main",
  "config":{
    "version":1,
    "options":{
      "allowAdhocFilters":true,
      "maxFilesPerDownload":123,
      "maxBytesPerDownload":456789
    },
    "presets":[
      {"id":"all-pdf","includeGlobs":["**/*.pdf"]}
    ]
  }
}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	handler.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update config status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	configReq2 := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/config?ref=main", nil)
	configResp2 := httptest.NewRecorder()
	handler.ServeHTTP(configResp2, configReq2)
	if configResp2.Code != http.StatusOK {
		t.Fatalf("config2 status=%d body=%s", configResp2.Code, configResp2.Body.String())
	}
	var cfgPayload2 struct {
		Config struct {
			Options struct {
				AllowAdhocFilters   bool  `json:"allowAdhocFilters"`
				MaxFilesPerDownload int   `json:"maxFilesPerDownload"`
				MaxBytesPerDownload int64 `json:"maxBytesPerDownload"`
			} `json:"options"`
			Presets []struct {
				ID string `json:"id"`
			} `json:"presets"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configResp2.Body.Bytes(), &cfgPayload2); err != nil {
		t.Fatalf("config2 json decode failed: %v", err)
	}
	if !cfgPayload2.Config.Options.AllowAdhocFilters {
		t.Fatalf("expected allowAdhocFilters=true after update")
	}
	if cfgPayload2.Config.Options.MaxFilesPerDownload != 123 {
		t.Fatalf("expected maxFilesPerDownload=123, got %d", cfgPayload2.Config.Options.MaxFilesPerDownload)
	}
	if cfgPayload2.Config.Options.MaxBytesPerDownload != 456789 {
		t.Fatalf("expected maxBytesPerDownload=456789, got %d", cfgPayload2.Config.Options.MaxBytesPerDownload)
	}
	if len(cfgPayload2.Config.Presets) != 1 || cfgPayload2.Config.Presets[0].ID != "all-pdf" {
		t.Fatalf("unexpected presets after update: %#v", cfgPayload2.Config.Presets)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/download.zip?ref=main&preset=all-pdf", nil)
	downloadResp := httptest.NewRecorder()
	handler.ServeHTTP(downloadResp, downloadReq)

	if downloadResp.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
	if downloadResp.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("expected byte range support, got %q", downloadResp.Header().Get("Accept-Ranges"))
	}

	zipBytes := downloadResp.Body.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}
	if len(reader.File) != 1 {
		t.Fatalf("expected one file in archive, got %d", len(reader.File))
	}
	if reader.File[0].Name != "rules/core/guide.pdf" {
		t.Fatalf("unexpected zip file name: %s", reader.File[0].Name)
	}
}

func TestIndexProgressResolvesRefAlias(t *testing.T) {
	progress := NewProgressManager()
	server := NewServer(Dependencies{
		Source: &stubSource{
			resolveRef: func(_ context.Context, owner, repo, ref string) (string, error) {
				if owner != "acme" || repo != "rules" || ref != "main" {
					t.Fatalf("unexpected ResolveRef call owner=%s repo=%s ref=%s", owner, repo, ref)
				}
				return "commit-sha", nil
			},
		},
		Progress:      progress,
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/repos/acme/rules/index-progress?ref=main", nil)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("http.Do failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	if got := readSSEDataLine(t, reader); got != `{"count": 0, "phase": "indexing"}` {
		t.Fatalf("unexpected initial SSE payload: %s", got)
	}

	progress.Notify("acme", "rules", "commit-sha", 7)
	if got := readSSEDataLine(t, reader); got != `{"count": 7, "phase": "indexing"}` {
		t.Fatalf("unexpected progress SSE payload: %s", got)
	}
}

func TestIndexProgressSendsFinalizingPhase(t *testing.T) {
	progress := NewProgressManager()
	server := NewServer(Dependencies{
		Progress:      progress,
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/repos/acme/rules/index-progress?ref=main", nil)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("http.Do failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	if got := readSSEDataLine(t, reader); got != `{"count": 0, "phase": "indexing"}` {
		t.Fatalf("unexpected initial SSE payload: %s", got)
	}

	progress.Finalize("acme", "rules", "main", 42)
	if got := readSSEDataLine(t, reader); got != `{"count": 42, "phase": "finalizing"}` {
		t.Fatalf("unexpected finalizing SSE payload: %s", got)
	}
}

func TestDownloadReturnsAPIErrorWhenArchiveBuildFails(t *testing.T) {
	server := NewServer(Dependencies{
		Source: &stubSource{
			resolveRef: func(_ context.Context, owner, repo, ref string) (string, error) {
				return "commit-123", nil
			},
			readFile: func(_ context.Context, owner, repo, commit, filePath string) ([]byte, error) {
				return nil, source.ErrNotFound
			},
			listFiles: func(_ context.Context, owner, repo, commit string, criteria filter.Criteria) ([]source.Entry, error) {
				return []source.Entry{{Path: "broken.txt", Size: 7}}, nil
			},
			openFile: func(_ context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
				return nil, errors.New("boom")
			},
		},
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/download.zip?ref=main", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := apiErrorCode(t, resp.Body.Bytes()); got != "archive_stream_failed" {
		t.Fatalf("unexpected error code %q", got)
	}
}

func TestDownloadSupportsRangeRequests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", "docs", "guide.txt"), []byte(strings.Repeat("0123456789", 20)))

	server := NewServer(Dependencies{
		Source:        newTestFSSource(root),
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})
	handler := server.Handler()

	fullReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/download.zip?ref=main&prefix=docs", nil)
	fullResp := httptest.NewRecorder()
	handler.ServeHTTP(fullResp, fullReq)
	if fullResp.Code != http.StatusOK {
		t.Fatalf("full download status=%d body=%s", fullResp.Code, fullResp.Body.String())
	}
	fullBody := fullResp.Body.Bytes()
	if len(fullBody) < 64 {
		t.Fatalf("expected larger archive, got %d bytes", len(fullBody))
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/download.zip?ref=main&prefix=docs", nil)
	rangeReq.Header.Set("Range", "bytes=10-39")
	rangeResp := httptest.NewRecorder()
	handler.ServeHTTP(rangeResp, rangeReq)

	if rangeResp.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d body=%s", rangeResp.Code, rangeResp.Body.String())
	}
	if got := rangeResp.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("expected Accept-Ranges=bytes, got %q", got)
	}
	if got := rangeResp.Header().Get("Content-Range"); got != "bytes 10-39/"+strconv.Itoa(len(fullBody)) {
		t.Fatalf("unexpected Content-Range %q", got)
	}
	if !bytes.Equal(rangeResp.Body.Bytes(), fullBody[10:40]) {
		t.Fatalf("range body mismatch")
	}
}

func TestDownloadRangeUsesSourceRangeReader(t *testing.T) {
	content := "0123456789ABCDEFGHIJ"
	entry := source.Entry{Path: "docs/guide.txt", Size: int64(len(content))}

	var rangeCalls int
	server := NewServer(Dependencies{
		Source: &stubSource{
			resolveRef: func(_ context.Context, owner, repo, ref string) (string, error) {
				return "commit-123", nil
			},
			readFile: func(_ context.Context, owner, repo, commit, filePath string) ([]byte, error) {
				return nil, source.ErrNotFound
			},
			listFiles: func(_ context.Context, owner, repo, commit string, criteria filter.Criteria) ([]source.Entry, error) {
				return []source.Entry{entry}, nil
			},
			openFile: func(_ context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(content)), nil
			},
			openFileRange: func(_ context.Context, owner, repo, commit, filePath string, start, end int64) (io.ReadCloser, error) {
				rangeCalls++
				if start != 5 || end != 10 {
					t.Fatalf("unexpected file range %d-%d", start, end)
				}
				return io.NopCloser(strings.NewReader(content[start:end])), nil
			},
		},
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})
	handler := server.Handler()

	rangeStart := int64(len(buildTestLocalHeader(entry.Path))) + 5
	rangeEnd := rangeStart + 5

	rangeReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/download.zip?ref=main", nil)
	rangeReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd-1))
	rangeResp := httptest.NewRecorder()
	handler.ServeHTTP(rangeResp, rangeReq)

	if rangeResp.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d body=%s", rangeResp.Code, rangeResp.Body.String())
	}
	if rangeCalls != 1 {
		t.Fatalf("expected one ranged file read, got %d", rangeCalls)
	}
	if got := rangeResp.Body.String(); got != content[5:10] {
		t.Fatalf("unexpected ranged body %q", got)
	}
}

func TestPreviewPrivateDownloadURLWorksWithoutAuthorizationHeader(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", ".zip-forger.yaml"), []byte(`
version: 1
presets:
  - id: docs
    includeGlobs: ["docs/**/*.pdf"]
`))
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", "docs", "guide.pdf"), []byte("pdf-content"))

	manager, err := auth.NewManager(auth.Config{
		ForgejoBaseURL: "http://forgejo.local",
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		RedirectURL:    "http://example.local/auth/callback",
		SessionSecret:  "session-secret",
		CookieSecure:   false,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	privateURL, err := NewPrivateDownloadCodec("download-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewPrivateDownloadCodec failed: %v", err)
	}

	server := NewServer(Dependencies{
		Source:        newTestFSSource(root),
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		Auth:          manager,
		ArtifactStore: NewArtifactStore(t.TempDir()),
		PrivateURL:    privateURL,
		Logger:        log.New(io.Discard, "", 0),
	})
	handler := server.Handler()

	publicPreviewReq := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main","preset":"docs"}`))
	publicPreviewReq.Header.Set("Authorization", "Bearer tok-123")
	publicPreviewResp := httptest.NewRecorder()
	handler.ServeHTTP(publicPreviewResp, publicPreviewReq)
	if publicPreviewResp.Code != http.StatusOK {
		t.Fatalf("public preview status=%d body=%s", publicPreviewResp.Code, publicPreviewResp.Body.String())
	}

	var publicPreview struct {
		DownloadURL      string     `json:"downloadUrl"`
		DownloadURLUntil *time.Time `json:"downloadUrlUntil"`
	}
	if err := json.Unmarshal(publicPreviewResp.Body.Bytes(), &publicPreview); err != nil {
		t.Fatalf("public preview json decode failed: %v", err)
	}
	if !strings.HasPrefix(publicPreview.DownloadURL, "/api/repos/acme/rules/download.zip?") {
		t.Fatalf("expected public download URL by default, got %q", publicPreview.DownloadURL)
	}
	if publicPreview.DownloadURLUntil != nil {
		t.Fatalf("expected no expiry for public download URL")
	}

	previewReq := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main","preset":"docs","privateDownloadUrl":true}`))
	previewReq.Header.Set("Authorization", "Bearer tok-123")
	previewResp := httptest.NewRecorder()
	handler.ServeHTTP(previewResp, previewReq)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResp.Code, previewResp.Body.String())
	}

	var preview struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.Unmarshal(previewResp.Body.Bytes(), &preview); err != nil {
		t.Fatalf("preview json decode failed: %v", err)
	}
	if !strings.HasPrefix(preview.DownloadURL, "/api/downloads/private.zip?token=") {
		t.Fatalf("expected private download URL, got %q", preview.DownloadURL)
	}

	parsed, err := url.Parse(preview.DownloadURL)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	downloadResp := httptest.NewRecorder()
	handler.ServeHTTP(downloadResp, downloadReq)
	if downloadResp.Code != http.StatusOK {
		t.Fatalf("private download status=%d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
	if downloadResp.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("expected private download to support byte ranges")
	}

	reader, err := zip.NewReader(bytes.NewReader(downloadResp.Body.Bytes()), int64(downloadResp.Body.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "docs/guide.pdf" {
		t.Fatalf("unexpected zip contents: %#v", reader.File)
	}
}

func TestPrivateDownloadURLPropagatesEmbeddedAccessToken(t *testing.T) {
	manager, err := auth.NewManager(auth.Config{
		ForgejoBaseURL: "http://forgejo.local",
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		RedirectURL:    "http://example.local/auth/callback",
		SessionSecret:  "session-secret",
		CookieSecure:   false,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	privateURL, err := NewPrivateDownloadCodec("download-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewPrivateDownloadCodec failed: %v", err)
	}

	checkToken := func(ctx context.Context) error {
		token, ok := source.AccessTokenFromContext(ctx)
		if !ok || token != "tok-123" {
			return errors.New("missing embedded access token")
		}
		return nil
	}

	server := NewServer(Dependencies{
		Source: &stubSource{
			resolveRef: func(ctx context.Context, owner, repo, ref string) (string, error) {
				if err := checkToken(ctx); err != nil {
					return "", err
				}
				return "commit-123", nil
			},
			readFile: func(ctx context.Context, owner, repo, commit, filePath string) ([]byte, error) {
				if err := checkToken(ctx); err != nil {
					return nil, err
				}
				return nil, source.ErrNotFound
			},
			listFiles: func(ctx context.Context, owner, repo, commit string, criteria filter.Criteria) ([]source.Entry, error) {
				if err := checkToken(ctx); err != nil {
					return nil, err
				}
				return []source.Entry{{Path: "docs/guide.pdf", Size: 11}}, nil
			},
			openFile: func(ctx context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
				if err := checkToken(ctx); err != nil {
					return nil, err
				}
				return io.NopCloser(strings.NewReader("pdf-content")), nil
			},
		},
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		Auth:          manager,
		ArtifactStore: NewArtifactStore(t.TempDir()),
		PrivateURL:    privateURL,
		Logger:        log.New(io.Discard, "", 0),
	})
	handler := server.Handler()

	previewReq := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main","privateDownloadUrl":true}`))
	previewReq.Header.Set("Authorization", "Bearer tok-123")
	previewResp := httptest.NewRecorder()
	handler.ServeHTTP(previewResp, previewReq)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResp.Code, previewResp.Body.String())
	}

	var preview struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.Unmarshal(previewResp.Body.Bytes(), &preview); err != nil {
		t.Fatalf("preview json decode failed: %v", err)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, preview.DownloadURL, nil)
	downloadResp := httptest.NewRecorder()
	handler.ServeHTTP(downloadResp, downloadReq)
	if downloadResp.Code != http.StatusOK {
		t.Fatalf("private download status=%d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
}

func TestPreviewUsesResolvedEntrySizes(t *testing.T) {
	server := NewServer(Dependencies{
		Source: &stubSource{
			resolveRef: func(_ context.Context, owner, repo, ref string) (string, error) {
				return "commit-123", nil
			},
			readFile: func(_ context.Context, owner, repo, commit, filePath string) ([]byte, error) {
				return nil, source.ErrNotFound
			},
			listFiles: func(_ context.Context, owner, repo, commit string, criteria filter.Criteria) ([]source.Entry, error) {
				return []source.Entry{{Path: "large.bin", Size: 128}}, nil
			},
			resolveSizes: func(_ context.Context, owner, repo, commit string, entries []source.Entry) ([]source.Entry, error) {
				return []source.Entry{{Path: "large.bin", Size: 10_000}}, nil
			},
		},
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		Logger:        log.New(io.Discard, "", 0),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/acme/rules/preview", bytes.NewBufferString(`{"ref":"main"}`))
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", resp.Code, resp.Body.String())
	}

	var payload struct {
		TotalBytes int64 `json:"totalBytes"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("preview json decode failed: %v", err)
	}
	if payload.TotalBytes != 10_000 {
		t.Fatalf("expected resolved totalBytes=10000, got %d", payload.TotalBytes)
	}
}

func TestPrivateDownloadURLRejectsExpiredToken(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "acme", "rules", "main", "docs", "guide.txt"), []byte("hello"))

	privateURL, err := NewPrivateDownloadCodec("download-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewPrivateDownloadCodec failed: %v", err)
	}
	token, err := privateURL.encodePayload(privateDownloadPayload{
		Owner:       "acme",
		Repo:        "rules",
		Commit:      "main",
		AccessToken: "tok-123",
		ExpiresAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("encodePayload failed: %v", err)
	}

	server := NewServer(Dependencies{
		Source:        newTestFSSource(root),
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
		ArtifactStore: NewArtifactStore(t.TempDir()),
		PrivateURL:    privateURL,
		Logger:        log.New(io.Discard, "", 0),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/downloads/private.zip?token="+url.QueryEscape(token), nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := apiErrorCode(t, resp.Body.Bytes()); got != "invalid_private_download_token" {
		t.Fatalf("unexpected error code %q", got)
	}
}

func writeTestFile(t *testing.T, filePath string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", filePath, err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", filePath, err)
	}
}

func apiErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unable to decode api error: %v body=%s", err, string(body))
	}
	return payload.Error.Code
}

func readSSEDataLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- "ERR:" + err.Error()
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				done <- strings.TrimPrefix(line, "data: ")
				return
			}
		}
	}()

	select {
	case value := <-done:
		if strings.HasPrefix(value, "ERR:") {
			t.Fatalf("failed reading SSE: %s", strings.TrimPrefix(value, "ERR:"))
		}
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE data")
		return ""
	}
}

type stubSource struct {
	resolveRef    func(context.Context, string, string, string) (string, error)
	readFile      func(context.Context, string, string, string, string) ([]byte, error)
	listFiles     func(context.Context, string, string, string, filter.Criteria) ([]source.Entry, error)
	openFile      func(context.Context, string, string, string, string) (io.ReadCloser, error)
	openFileRange func(context.Context, string, string, string, string, int64, int64) (io.ReadCloser, error)
	resolveSizes  func(context.Context, string, string, string, []source.Entry) ([]source.Entry, error)
}

func (s *stubSource) ResolveRef(ctx context.Context, owner, repo, ref string) (string, error) {
	if s.resolveRef != nil {
		return s.resolveRef(ctx, owner, repo, ref)
	}
	return "", source.ErrNotFound
}

func (s *stubSource) ReadFile(ctx context.Context, owner, repo, commit, filePath string) ([]byte, error) {
	if s.readFile != nil {
		return s.readFile(ctx, owner, repo, commit, filePath)
	}
	return nil, source.ErrNotFound
}

func (s *stubSource) ListFiles(ctx context.Context, owner, repo, commit string, criteria filter.Criteria) ([]source.Entry, error) {
	if s.listFiles != nil {
		return s.listFiles(ctx, owner, repo, commit, criteria)
	}
	return nil, nil
}

func (s *stubSource) OpenFile(ctx context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
	if s.openFile != nil {
		return s.openFile(ctx, owner, repo, commit, filePath)
	}
	return nil, source.ErrNotFound
}

func (s *stubSource) OpenFileRange(ctx context.Context, owner, repo, commit, filePath string, start, end int64) (io.ReadCloser, error) {
	if s.openFileRange != nil {
		return s.openFileRange(ctx, owner, repo, commit, filePath, start, end)
	}
	if s.openFile == nil {
		return nil, source.ErrNotFound
	}
	reader, err := s.openFile(ctx, owner, repo, commit, filePath)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if _, err := io.CopyN(io.Discard, reader, start); err != nil {
			reader.Close()
			return nil, err
		}
	}
	if end >= 0 {
		return testReadCloser{
			Reader: io.LimitReader(reader, end-start),
			Closer: reader,
		}, nil
	}
	return reader, nil
}

func (s *stubSource) ResolveEntrySizes(ctx context.Context, owner, repo, commit string, entries []source.Entry) ([]source.Entry, error) {
	if s.resolveSizes != nil {
		return s.resolveSizes(ctx, owner, repo, commit, entries)
	}
	out := make([]source.Entry, len(entries))
	copy(out, entries)
	return out, nil
}

func buildTestLocalHeader(name string) []byte {
	header := make([]byte, 30+len(name))
	copy(header[30:], name)
	return header
}

type testReadCloser struct {
	io.Reader
	io.Closer
}

func (s *stubSource) SearchRepos(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *stubSource) ListBranches(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (s *stubSource) GetFileSHA(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s *stubSource) UpsertFile(context.Context, string, string, string, string, []byte, string, string) error {
	return nil
}

type testFSSource struct {
	root string
}

func newTestFSSource(root string) *testFSSource {
	return &testFSSource{root: root}
}

func (s *testFSSource) ResolveRef(_ context.Context, owner, repo, ref string) (string, error) {
	repoRoot := filepath.Join(s.root, owner, repo)
	info, err := os.Stat(repoRoot)
	if err != nil || !info.IsDir() {
		return "", source.ErrNotFound
	}

	if ref != "" {
		refRoot := filepath.Join(repoRoot, ref)
		info, err := os.Stat(refRoot)
		if err != nil || !info.IsDir() {
			return "", source.ErrNotFound
		}
		return ref, nil
	}

	mainRoot := filepath.Join(repoRoot, "main")
	if info, err := os.Stat(mainRoot); err == nil && info.IsDir() {
		return "main", nil
	}

	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return "", err
	}
	refs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			refs = append(refs, entry.Name())
		}
	}
	sort.Strings(refs)
	if len(refs) == 0 {
		return "", source.ErrNotFound
	}
	return refs[0], nil
}

func (s *testFSSource) ReadFile(_ context.Context, owner, repo, commit, filePath string) ([]byte, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	fullPath, err := safeTestJoin(base, filePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, source.ErrNotFound
	}
	return data, err
}

func (s *testFSSource) ListFiles(_ context.Context, owner, repo, commit string, _ filter.Criteria) ([]source.Entry, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, source.ErrNotFound
	}

	entries := make([]source.Entry, 0, 1024)
	err = filepath.WalkDir(base, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(base, current)
		if err != nil {
			return err
		}
		entries = append(entries, source.Entry{
			Path: filepath.ToSlash(relative),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func (s *testFSSource) OpenFile(_ context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	fullPath, err := safeTestJoin(base, filePath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, source.ErrNotFound
	}
	return f, err
}

func (s *testFSSource) OpenFileRange(_ context.Context, owner, repo, commit, filePath string, start, end int64) (io.ReadCloser, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	fullPath, err := safeTestJoin(base, filePath)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, source.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if end >= 0 && end < start {
		f.Close()
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if end < 0 {
		return f, nil
	}
	return testReadCloser{
		Reader: io.LimitReader(f, end-start),
		Closer: f,
	}, nil
}

func (s *testFSSource) SearchRepos(_ context.Context, query string) ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	query = strings.ToLower(query)
	for _, ownerEntry := range entries {
		if !ownerEntry.IsDir() {
			continue
		}
		owner := ownerEntry.Name()
		repoEntries, err := os.ReadDir(filepath.Join(s.root, owner))
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			repo := repoEntry.Name()
			fullName := owner + "/" + repo
			if query == "" || strings.Contains(strings.ToLower(fullName), query) {
				out = append(out, fullName)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *testFSSource) ListBranches(_ context.Context, owner, repo string) ([]string, error) {
	root := filepath.Join(s.root, owner, repo)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, source.ErrNotFound
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *testFSSource) GetFileSHA(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s *testFSSource) UpsertFile(_ context.Context, owner, repo, branch, filePath string, data []byte, _, _ string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	base := filepath.Join(s.root, owner, repo, branch)
	targetPath, err := safeTestJoin(base, filePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func safeTestJoin(base, filePath string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(filePath))
	fullPath := filepath.Join(base, cleaned)
	relative, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", source.ErrNotFound
	}
	return fullPath, nil
}
