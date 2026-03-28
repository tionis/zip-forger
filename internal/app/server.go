package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"zip-forger/internal/auth"
	"zip-forger/internal/cache"
	"zip-forger/internal/config"
	"zip-forger/internal/filter"
	"zip-forger/internal/source"
	"zip-forger/internal/ui"
	"zip-forger/internal/zipstream"
)

type Dependencies struct {
	Source        source.RepositorySource
	ManifestCache *cache.ManifestCache
	Auth          *auth.Manager
	Progress      *ProgressManager
	ArtifactStore *ArtifactStore
	PrivateURL    *PrivateDownloadCodec
	Logger        *log.Logger
}

type Server struct {
	source        source.RepositorySource
	manifestCache *cache.ManifestCache
	auth          *auth.Manager
	progress      *ProgressManager
	artifactStore *ArtifactStore
	privateURL    *PrivateDownloadCodec
	logger        *log.Logger
}

type previewRequest struct {
	Ref    string          `json:"ref"`
	Preset string          `json:"preset"`
	Adhoc  filter.Criteria `json:"adhoc"`
}

type previewResponse struct {
	Commit           string          `json:"commit"`
	Preset           string          `json:"preset,omitempty"`
	Criteria         filter.Criteria `json:"criteria"`
	SelectedFiles    int             `json:"selectedFiles"`
	TotalBytes       int64           `json:"totalBytes"`
	FromCache        bool            `json:"fromCache"`
	Entries          []string        `json:"entries,omitempty"`
	EntriesTruncated bool            `json:"entriesTruncated"`
	DownloadURL      string          `json:"downloadUrl,omitempty"`
	DownloadURLUntil *time.Time      `json:"downloadUrlUntil,omitempty"`
}

type configResponse struct {
	Commit string            `json:"commit"`
	Config config.RepoConfig `json:"config"`
}

type updateConfigRequest struct {
	Ref           string            `json:"ref"`
	Config        config.RepoConfig `json:"config"`
	CommitMessage string            `json:"commitMessage"`
}

type selection struct {
	Commit    string
	Criteria  filter.Criteria
	Manifest  cache.Manifest
	FromCache bool
}

type downloadRequest struct {
	Owner  string
	Repo   string
	Ref    string
	Preset string
	Adhoc  filter.Criteria
}

type apiError struct {
	status  int
	code    string
	message string
	err     error
}

const previewEntriesLimit = 2000

func (e *apiError) Error() string {
	return e.message
}

func (e *apiError) Unwrap() error {
	return e.err
}

func NewServer(deps Dependencies) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = log.Default()
	}

	manifestCache := deps.ManifestCache
	if manifestCache == nil {
		manifestCache = cache.NewManifestCache(0, 1)
	}

	artifactStore := deps.ArtifactStore
	if artifactStore == nil {
		artifactStore = NewArtifactStore(filepath.Join(os.TempDir(), "zip-forger-downloads"))
	}

	return &Server{
		source:        deps.Source,
		manifestCache: manifestCache,
		auth:          deps.Auth,
		progress:      deps.Progress,
		artifactStore: artifactStore,
		privateURL:    deps.PrivateURL,
		logger:        logger,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleUI)
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	if s.auth != nil {
		s.auth.RegisterRoutes(mux)
	}

	previewHandler := http.Handler(http.HandlerFunc(s.handlePreview))
	downloadHandler := http.Handler(http.HandlerFunc(s.handleDownload))
	progressHandler := http.Handler(http.HandlerFunc(s.handleIndexProgress))
	configGetHandler := http.Handler(http.HandlerFunc(s.handleConfig))
	configPutHandler := http.Handler(http.HandlerFunc(s.handleUpdateConfig))
	searchReposHandler := http.Handler(http.HandlerFunc(s.handleSearchRepos))
	branchesHandler := http.Handler(http.HandlerFunc(s.handleBranches))
	if s.auth != nil {
		previewHandler = s.auth.Middleware(previewHandler)
		downloadHandler = s.auth.Middleware(downloadHandler)
		progressHandler = s.auth.Middleware(progressHandler)
		configGetHandler = s.auth.Middleware(configGetHandler)
		configPutHandler = s.auth.Middleware(configPutHandler)
		searchReposHandler = s.auth.Middleware(searchReposHandler)
		branchesHandler = s.auth.Middleware(branchesHandler)
	}

	mux.Handle("GET /api/repos/search", searchReposHandler)
	mux.Handle("GET /api/repos/{owner}/{repo}/index-progress", progressHandler)
	mux.Handle("GET /api/repos/{owner}/{repo}/branches", branchesHandler)
	mux.Handle("POST /api/repos/{owner}/{repo}/preview", previewHandler)
	mux.Handle("GET /api/repos/{owner}/{repo}/download.zip", downloadHandler)
	mux.HandleFunc("GET /api/downloads/private.zip", s.handlePrivateDownload)
	mux.Handle("GET /api/repos/{owner}/{repo}/config", configGetHandler)
	mux.Handle("PUT /api/repos/{owner}/{repo}/config", configPutHandler)
	return mux
}

func (s *Server) handleUI(w http.ResponseWriter, _ *http.Request) {
	ui.RenderIndex(w, ui.PageData{
		AuthEnabled:  s.auth != nil && s.auth.Enabled(),
		AuthRequired: s.auth != nil && s.auth.Required(),
	})
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleIndexProgress(w http.ResponseWriter, r *http.Request) {
	if s.progress == nil {
		http.Error(w, "progress reporting disabled", http.StatusNotImplemented)
		return
	}

	owner := strings.TrimSpace(r.PathValue("owner"))
	repo := strings.TrimSpace(r.PathValue("repo"))
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		ref = strings.TrimSpace(r.URL.Query().Get("commit"))
	}

	aliases := []string{ref}
	if ref != "" && s.source != nil {
		if commit, err := s.source.ResolveRef(r.Context(), owner, repo, ref); err == nil && strings.TrimSpace(commit) != "" {
			aliases = append(aliases, commit)
		}
	}

	s.progress.HandleSSE(w, r, owner, repo, aliases...)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")

	req, err := decodePreviewRequest(r)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	sel, err := s.buildSelection(r.Context(), owner, repo, req.Ref, req.Preset, req.Adhoc)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	entries, truncated := manifestEntryPaths(sel.Manifest.Entries, previewEntriesLimit)
	downloadURL, downloadURLUntil := s.buildDownloadURL(r, downloadRequest{
		Owner:  owner,
		Repo:   repo,
		Ref:    sel.Commit,
		Preset: req.Preset,
		Adhoc:  req.Adhoc,
	})
	writeJSON(w, http.StatusOK, previewResponse{
		Commit:           sel.Commit,
		Preset:           req.Preset,
		Criteria:         sel.Criteria,
		SelectedFiles:    len(sel.Manifest.Entries),
		TotalBytes:       sel.Manifest.TotalBytes,
		FromCache:        sel.FromCache,
		Entries:          entries,
		EntriesTruncated: truncated,
		DownloadURL:      downloadURL,
		DownloadURLUntil: downloadURLUntil,
	})
}

func (s *Server) handleSearchRepos(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	repos, err := s.source.SearchRepos(r.Context(), q)
	if err != nil {
		s.writeAPIError(w, sourceErrorToAPI(err, "unable to search repositories"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repos": repos,
	})
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.PathValue("owner"))
	repo := strings.TrimSpace(r.PathValue("repo"))
	if owner == "" || repo == "" {
		s.writeAPIError(w, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_repository",
			message: "owner and repository are required",
		})
		return
	}

	branches, err := s.source.ListBranches(r.Context(), owner, repo)
	if err != nil {
		s.writeAPIError(w, sourceErrorToAPI(err, "unable to list branches"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"owner":    owner,
		"repo":     repo,
		"branches": branches,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))

	commit, err := s.resolveCommit(r.Context(), owner, repo, ref)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	repoConfig, err := s.loadRepoConfig(r.Context(), owner, repo, commit)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, configResponse{
		Commit: commit,
		Config: repoConfig,
	})
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.PathValue("owner"))
	repo := strings.TrimSpace(r.PathValue("repo"))
	if owner == "" || repo == "" {
		s.writeAPIError(w, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_repository",
			message: "owner and repository are required",
		})
		return
	}

	var req updateConfigRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.writeAPIError(w, err)
		return
	}
	branch := strings.TrimSpace(req.Ref)
	if branch == "" {
		branch = "main"
	}
	if err := config.NormalizeAndValidate(&req.Config); err != nil {
		s.writeAPIError(w, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_repo_config",
			message: "repository config is invalid",
			err:     err,
		})
		return
	}
	data, err := yaml.Marshal(req.Config)
	if err != nil {
		s.writeAPIError(w, &apiError{
			status:  http.StatusInternalServerError,
			code:    "config_serialize_failed",
			message: "unable to serialize config",
			err:     err,
		})
		return
	}

	message := strings.TrimSpace(req.CommitMessage)
	if message == "" {
		message = "chore(zip-forger): update .zip-forger.yaml"
	}

	// Fetch current SHA to prevent accidental overwrites
	currentSHA, err := s.source.GetFileSHA(r.Context(), owner, repo, branch, config.FileName)
	if err != nil && !errors.Is(err, source.ErrNotFound) {
		s.writeAPIError(w, sourceErrorToAPI(err, "unable to fetch current config SHA"))
		return
	}

	if err := s.source.UpsertFile(r.Context(), owner, repo, branch, config.FileName, data, message, currentSHA); err != nil {
		s.writeAPIError(w, sourceErrorToAPI(err, "unable to save repository config"))
		return
	}

	commit, resolveErr := s.resolveCommit(r.Context(), owner, repo, branch)
	if resolveErr != nil {
		s.logger.Printf("config save resolve warning owner=%s repo=%s branch=%s err=%v", owner, repo, branch, resolveErr)
		commit = ""
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"ref":    branch,
		"commit": commit,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	req := downloadRequest{
		Owner:  owner,
		Repo:   repo,
		Ref:    strings.TrimSpace(r.URL.Query().Get("ref")),
		Preset: strings.TrimSpace(r.URL.Query().Get("preset")),
		Adhoc: filter.Criteria{
			IncludeGlobs: collectQueryValues(r.URL.Query(), "include"),
			ExcludeGlobs: collectQueryValues(r.URL.Query(), "exclude"),
			Extensions:   collectQueryValues(r.URL.Query(), "ext"),
			PathPrefixes: collectQueryValues(r.URL.Query(), "prefix"),
		},
	}

	sel, err := s.buildSelection(r.Context(), owner, repo, req.Ref, req.Preset, req.Adhoc)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	token, _ := source.AccessTokenFromContext(r.Context())
	if err := s.serveSelectionArchive(w, r, req.Owner, req.Repo, sel, token); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.writeAPIError(w, err)
		return
	}
}

func (s *Server) handlePrivateDownload(w http.ResponseWriter, r *http.Request) {
	if s.privateURL == nil {
		s.writeAPIError(w, &apiError{
			status:  http.StatusNotFound,
			code:    "private_downloads_disabled",
			message: "private download URLs are not enabled for this instance",
		})
		return
	}

	payload, err := s.privateURL.Decode(r.URL.Query().Get(privateDownloadTokenParam))
	if err != nil {
		s.writeAPIError(w, &apiError{
			status:  http.StatusUnauthorized,
			code:    "invalid_private_download_token",
			message: "private download URL is invalid or expired",
			err:     err,
		})
		return
	}

	sel, err := s.buildSelectionForCommit(r.Context(), payload.Owner, payload.Repo, payload.Commit, payload.Preset, payload.Adhoc)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	if err := s.serveSelectionArchive(w, r, payload.Owner, payload.Repo, sel, payload.AccessToken); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.writeAPIError(w, err)
		return
	}
}

func (s *Server) buildSelection(ctx context.Context, owner, repo, ref, preset string, adhoc filter.Criteria) (selection, error) {
	commit, err := s.resolveCommit(ctx, owner, repo, ref)
	if err != nil {
		return selection{}, err
	}
	return s.buildSelectionForCommit(ctx, owner, repo, commit, preset, adhoc)
}

func (s *Server) buildSelectionForCommit(ctx context.Context, owner, repo, commit, preset string, adhoc filter.Criteria) (selection, error) {
	repoConfig, err := s.loadRepoConfig(ctx, owner, repo, commit)
	if err != nil {
		return selection{}, err
	}

	presetCriteria, err := presetToCriteria(repoConfig, preset)
	if err != nil {
		return selection{}, err
	}

	if !repoConfig.AllowAdhocFilters() && !adhoc.IsZero() {
		return selection{}, &apiError{
			status:  http.StatusForbidden,
			code:    "adhoc_filters_disabled",
			message: "ad-hoc filters are disabled for this repository",
		}
	}

	finalCriteria := filter.Merge(presetCriteria, adhoc)
	compiledCriteria, err := filter.Compile(finalCriteria)
	if err != nil {
		return selection{}, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_filter",
			message: "filter patterns are invalid",
			err:     err,
		}
	}

	cacheKey := selectionCacheKey(owner, repo, commit, finalCriteria)
	if manifest, ok := s.manifestCache.Get(cacheKey); ok {
		if err := enforceLimits(repoConfig, manifest); err != nil {
			return selection{}, err
		}
		return selection{
			Commit:    commit,
			Criteria:  finalCriteria,
			Manifest:  manifest,
			FromCache: true,
		}, nil
	}

	allFiles, err := s.source.ListFiles(ctx, owner, repo, commit, finalCriteria)
	if err != nil {
		if errors.Is(err, source.ErrUnauthorized) {
			return selection{}, &apiError{
				status:  http.StatusUnauthorized,
				code:    "source_unauthorized",
				message: "not authorized to list repository files",
				err:     err,
			}
		}
		return selection{}, &apiError{
			status:  http.StatusBadGateway,
			code:    "source_error",
			message: "unable to list repository files",
			err:     err,
		}
	}

	selected := make([]source.Entry, 0, len(allFiles))
	totalBytes := int64(0)
	for _, entry := range allFiles {
		if compiledCriteria.Match(entry.Path) {
			selected = append(selected, entry)
			totalBytes += entry.Size
		}
	}

	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Path < selected[j].Path
	})

	if resolver, ok := s.source.(source.EntrySizeResolver); ok && len(selected) > 0 {
		resolved, resolveErr := resolver.ResolveEntrySizes(ctx, owner, repo, commit, selected)
		if resolveErr != nil {
			s.logger.Printf("size resolution warning owner=%s repo=%s commit=%s err=%v", owner, repo, commit, resolveErr)
		} else {
			selected = resolved
		}
	}

	totalBytes = 0
	for _, entry := range selected {
		totalBytes += entry.Size
	}

	manifest := cache.Manifest{
		Entries:    selected,
		TotalBytes: totalBytes,
	}
	if err := enforceLimits(repoConfig, manifest); err != nil {
		return selection{}, err
	}

	s.manifestCache.Set(cacheKey, manifest)
	return selection{
		Commit:    commit,
		Criteria:  finalCriteria,
		Manifest:  manifest,
		FromCache: false,
	}, nil
}

func (s *Server) serveSelectionArchive(w http.ResponseWriter, r *http.Request, owner, repo string, sel selection, accessToken string) error {
	artifact, err := s.prepareArtifact(r.Context(), owner, repo, sel, accessToken)
	if err != nil {
		return err
	}

	file, err := os.Open(artifact.Path)
	if err != nil {
		return &apiError{
			status:  http.StatusInternalServerError,
			code:    "archive_open_failed",
			message: "unable to open generated archive",
			err:     err,
		}
	}
	defer file.Close()

	archiveName := sanitizeFilename(fmt.Sprintf("%s-%s.zip", repo, shortRef(sel.Commit)))
	w.Header().Set("X-Zip-Total-Size", strconv.FormatInt(sel.Manifest.TotalBytes, 10))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, archiveName))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Zip-Forger-Commit", sel.Commit)
	w.Header().Set("X-Zip-Forger-Resume", "bytes")
	w.Header().Set("ETag", `"`+artifact.Key+`"`)
	http.ServeContent(w, r, archiveName, artifact.ModTime, file)
	return nil
}

func (s *Server) prepareArtifact(ctx context.Context, owner, repo string, sel selection, accessToken string) (Artifact, error) {
	if s.artifactStore == nil {
		return Artifact{}, &apiError{
			status:  http.StatusInternalServerError,
			code:    "archive_store_unavailable",
			message: "download archive store is unavailable",
		}
	}

	artifact, err := s.artifactStore.Ensure(ctx, downloadArtifactKey(owner, repo, sel, accessToken), func(ctx context.Context, w io.Writer) error {
		return zipstream.Stream(ctx, w, sel.Manifest.Entries, func(ctx context.Context, filePath string) (io.ReadCloser, error) {
			return s.source.OpenFile(source.WithAccessToken(ctx, accessToken), owner, repo, sel.Commit, filePath)
		}, &zipstream.Options{
			OnFileError: func(path string, err error) error {
				if errors.Is(err, source.ErrNotFound) {
					s.logger.Printf("stream skip missing file owner=%s repo=%s commit=%s path=%s", owner, repo, sel.Commit, path)
					return nil
				}
				return err
			},
		})
	})
	if err == nil {
		return artifact, nil
	}
	if errors.Is(err, source.ErrUnauthorized) {
		return Artifact{}, &apiError{
			status:  http.StatusUnauthorized,
			code:    "source_unauthorized",
			message: "not authorized to read repository files",
			err:     err,
		}
	}
	if errors.Is(err, source.ErrNotFound) {
		return Artifact{}, &apiError{
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "requested resource was not found",
			err:     err,
		}
	}
	return Artifact{}, &apiError{
		status:  http.StatusBadGateway,
		code:    "archive_build_failed",
		message: "unable to create download archive",
		err:     err,
	}
}

func (s *Server) buildDownloadURL(r *http.Request, req downloadRequest) (string, *time.Time) {
	if r == nil {
		return "", nil
	}

	token, _ := source.AccessTokenFromContext(r.Context())
	if s.privateURL != nil && token != "" {
		privateToken, expiresAt, err := s.privateURL.Encode(req.Owner, req.Repo, req.Ref, req.Preset, token, req.Adhoc)
		if err == nil {
			values := url.Values{}
			values.Set(privateDownloadTokenParam, privateToken)
			expiresAt := expiresAt
			return requestBaseURL(r) + "/api/downloads/private.zip?" + values.Encode(), &expiresAt
		}
	}

	values := url.Values{}
	if req.Ref != "" {
		values.Set("ref", req.Ref)
	}
	if req.Preset != "" {
		values.Set("preset", req.Preset)
	}
	for _, value := range req.Adhoc.IncludeGlobs {
		values.Add("include", value)
	}
	for _, value := range req.Adhoc.ExcludeGlobs {
		values.Add("exclude", value)
	}
	for _, value := range req.Adhoc.Extensions {
		values.Add("ext", value)
	}
	for _, value := range req.Adhoc.PathPrefixes {
		values.Add("prefix", value)
	}

	path := fmt.Sprintf("/api/repos/%s/%s/download.zip", url.PathEscape(req.Owner), url.PathEscape(req.Repo))
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return requestBaseURL(r) + path, nil
}

func (s *Server) resolveCommit(ctx context.Context, owner, repo, ref string) (string, error) {
	commit, err := s.source.ResolveRef(ctx, owner, repo, ref)
	if err == nil {
		return commit, nil
	}

	if errors.Is(err, source.ErrNotFound) {
		return "", &apiError{
			status:  http.StatusNotFound,
			code:    "repo_or_ref_not_found",
			message: "repository or ref not found",
			err:     err,
		}
	}
	if errors.Is(err, source.ErrUnauthorized) {
		return "", &apiError{
			status:  http.StatusUnauthorized,
			code:    "source_unauthorized",
			message: "not authorized to access this repository",
			err:     err,
		}
	}
	return "", &apiError{
		status:  http.StatusBadGateway,
		code:    "source_error",
		message: "unable to resolve repository ref",
		err:     err,
	}
}

func sourceErrorToAPI(err error, fallbackMessage string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, source.ErrUnauthorized) {
		return &apiError{
			status:  http.StatusUnauthorized,
			code:    "source_unauthorized",
			message: "not authorized to access this repository",
			err:     err,
		}
	}
	if errors.Is(err, source.ErrNotFound) {
		return &apiError{
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "requested resource was not found",
			err:     err,
		}
	}
	return &apiError{
		status:  http.StatusBadGateway,
		code:    "source_error",
		message: fallbackMessage,
		err:     err,
	}
}

func (s *Server) loadRepoConfig(ctx context.Context, owner, repo, commit string) (config.RepoConfig, error) {
	data, err := s.source.ReadFile(ctx, owner, repo, commit, config.FileName)
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			return config.Default(), nil
		}
		if errors.Is(err, source.ErrUnauthorized) {
			return config.RepoConfig{}, &apiError{
				status:  http.StatusUnauthorized,
				code:    "source_unauthorized",
				message: "not authorized to read repository config",
				err:     err,
			}
		}
		return config.RepoConfig{}, &apiError{
			status:  http.StatusBadGateway,
			code:    "source_error",
			message: "unable to read repository config",
			err:     err,
		}
	}

	parsed, err := config.Parse(data)
	if err != nil {
		return config.RepoConfig{}, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_repo_config",
			message: "repository config is invalid",
			err:     err,
		}
	}
	return parsed, nil
}

func presetToCriteria(cfg config.RepoConfig, presetID string) (filter.Criteria, error) {
	if presetID == "" {
		return filter.Criteria{}, nil
	}
	preset, ok := cfg.PresetByID(presetID)
	if !ok {
		return filter.Criteria{}, &apiError{
			status:  http.StatusBadRequest,
			code:    "preset_not_found",
			message: "requested preset does not exist in .zip-forger.yaml",
		}
	}
	return preset.Criteria(), nil
}

func enforceLimits(cfg config.RepoConfig, manifest cache.Manifest) error {
	if cfg.Options.MaxFilesPerDownload > 0 && len(manifest.Entries) > cfg.Options.MaxFilesPerDownload {
		return &apiError{
			status:  http.StatusRequestEntityTooLarge,
			code:    "max_files_exceeded",
			message: "selected files exceed repository download limit",
		}
	}
	if cfg.Options.MaxBytesPerDownload > 0 && manifest.TotalBytes > cfg.Options.MaxBytesPerDownload {
		return &apiError{
			status:  http.StatusRequestEntityTooLarge,
			code:    "max_bytes_exceeded",
			message: "selected bytes exceed repository download limit",
		}
	}
	return nil
}

func selectionCacheKey(owner, repo, commit string, criteria filter.Criteria) string {
	payload, err := json.Marshal(criteria)
	if err != nil {
		payload = []byte("{}")
	}
	sum := sha256.Sum256(payload)
	return owner + "/" + repo + "@" + commit + ":" + hex.EncodeToString(sum[:])
}

func downloadArtifactKey(owner, repo string, sel selection, accessToken string) string {
	selectionKey := selectionCacheKey(owner, repo, sel.Commit, sel.Criteria)
	tokenHash := sha256.Sum256([]byte(accessToken))
	sum := sha256.Sum256([]byte(selectionKey + ":" + hex.EncodeToString(tokenHash[:])))
	return hex.EncodeToString(sum[:])
}

func decodePreviewRequest(r *http.Request) (previewRequest, error) {
	var req previewRequest

	if r.Body == nil {
		return req, nil
	}

	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, nil
		}
		return previewRequest{}, &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_json",
			message: "request body must be valid JSON",
			err:     err,
		}
	}
	return req, nil
}

func decodeJSONBody(r *http.Request, out any) error {
	if r.Body == nil {
		return &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_json",
			message: "request body is required",
		}
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return &apiError{
				status:  http.StatusBadRequest,
				code:    "invalid_json",
				message: "request body is required",
			}
		}
		return &apiError{
			status:  http.StatusBadRequest,
			code:    "invalid_json",
			message: "request body must be valid JSON",
			err:     err,
		}
	}
	return nil
}

func collectQueryValues(values url.Values, key string) []string {
	raw := values[key]
	if len(raw) == 0 {
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (s *Server) writeAPIError(w http.ResponseWriter, err error) {
	apiErr := &apiError{
		status:  http.StatusInternalServerError,
		code:    "internal_error",
		message: "internal server error",
		err:     err,
	}
	if errors.As(err, &apiErr) {
		if apiErr.err != nil {
			s.logger.Printf("api error status=%d code=%s err=%v", apiErr.status, apiErr.code, apiErr.err)
		}
	} else if err != nil {
		s.logger.Printf("api error status=%d code=%s err=%v", apiErr.status, apiErr.code, err)
	}

	writeJSON(w, apiErr.status, map[string]any{
		"error": map[string]string{
			"code":    apiErr.code,
			"message": apiErr.message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		"\"", "",
		"\n", "",
		"\r", "",
	)
	name = replacer.Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "download.zip"
	}
	return name
}

func shortRef(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func manifestEntryPaths(entries []source.Entry, limit int) ([]string, bool) {
	if limit <= 0 || len(entries) <= limit {
		out := make([]string, 0, len(entries))
		for _, entry := range entries {
			out = append(out, entry.Path)
		}
		return out, false
	}
	out := make([]string, 0, limit)
	for _, entry := range entries[:limit] {
		out = append(out, entry.Path)
	}
	return out, true
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	scheme := "http"
	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}
