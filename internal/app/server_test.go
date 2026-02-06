package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zip-forger/internal/cache"
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
		Source:        source.NewLocalFS(root),
		ManifestCache: cache.NewManifestCache(time.Minute, 128),
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

	ownersReq := httptest.NewRequest(http.MethodGet, "/api/owners", nil)
	ownersResp := httptest.NewRecorder()
	handler.ServeHTTP(ownersResp, ownersReq)
	if ownersResp.Code != http.StatusOK {
		t.Fatalf("owners status=%d body=%s", ownersResp.Code, ownersResp.Body.String())
	}
	var ownersPayload struct {
		Owners []string `json:"owners"`
	}
	if err := json.Unmarshal(ownersResp.Body.Bytes(), &ownersPayload); err != nil {
		t.Fatalf("owners json decode failed: %v", err)
	}
	if len(ownersPayload.Owners) != 1 || ownersPayload.Owners[0] != "acme" {
		t.Fatalf("unexpected owners payload: %#v", ownersPayload)
	}

	reposReq := httptest.NewRequest(http.MethodGet, "/api/owners/acme/repos", nil)
	reposResp := httptest.NewRecorder()
	handler.ServeHTTP(reposResp, reposReq)
	if reposResp.Code != http.StatusOK {
		t.Fatalf("repos status=%d body=%s", reposResp.Code, reposResp.Body.String())
	}
	var reposPayload struct {
		Repos []string `json:"repos"`
	}
	if err := json.Unmarshal(reposResp.Body.Bytes(), &reposPayload); err != nil {
		t.Fatalf("repos json decode failed: %v", err)
	}
	if len(reposPayload.Repos) != 1 || reposPayload.Repos[0] != "rules" {
		t.Fatalf("unexpected repos payload: %#v", reposPayload)
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

func writeTestFile(t *testing.T, filePath string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", filePath, err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", filePath, err)
	}
}
