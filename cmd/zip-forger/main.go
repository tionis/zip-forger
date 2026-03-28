package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"zip-forger/internal/app"
	"zip-forger/internal/auth"
	"zip-forger/internal/cache"
	"zip-forger/internal/source"
)

func main() {
	cacheDir := getenv("ZIP_FORGER_CACHE_DIR", "./.cache/zip-forger")
	treeDB, err := cache.NewTreeDB(filepath.Join(cacheDir, "trees.db"), nil)
	if err != nil {
		log.Fatalf("failed to initialize tree database: %v", err)
	}
	defer treeDB.Close()

	progressManager := app.NewProgressManager()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	addr := getenv("ZIP_FORGER_ADDR", ":8080")
	sourceType := strings.ToLower(getenv("ZIP_FORGER_SOURCE", "local"))
	repoRoot := getenv("ZIP_FORGER_REPO_ROOT", "./mock-repos")
	forgejoBaseURL := getenv("ZIP_FORGER_FORGEJO_BASE_URL", "")

	repoSource, err := buildSource(sourceType, repoRoot, forgejoBaseURL, treeDB, progressManager)
	if err != nil {
		logger.Fatalf("source setup failed: %v", err)
	}

	authMode := strings.ToLower(getenv("ZIP_FORGER_AUTH_MODE", defaultAuthMode(sourceType)))
	authRequired := parseBool(getenv("ZIP_FORGER_AUTH_REQUIRED", ""))
	if authRequired == nil {
		authRequired = boolPtr(sourceType == "forgejo")
	}
	authManager, err := buildAuthManager(authMode, *authRequired, forgejoBaseURL, logger)
	if err != nil {
		logger.Fatalf("auth setup failed: %v", err)
	}

	svc := app.NewServer(app.Dependencies{
		Source:        repoSource,
		ManifestCache: cache.NewManifestCache(5*time.Minute, 1024),
		Auth:          authManager,
		Progress:      progressManager,
		Logger:        logger,
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		// WriteTimeout is intentionally unset for long-running streamed downloads.
	}

	go func() {
		logger.Printf("zip-forger listening on %s (source=%s)", addr, sourceType)
		if sourceType == "local" {
			logger.Printf("local repository root: %s", repoRoot)
		}
		if sourceType == "forgejo" {
			logger.Printf("forgejo base URL: %s", forgejoBaseURL)
		}
		if authManager != nil && authManager.Enabled() {
			logger.Printf("auth mode: %s (required=%t)", authMode, authManager.Required())
		} else {
			logger.Printf("auth mode: none (required=%t)", *authRequired)
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server failed: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown error: %v", err)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func buildSource(sourceType, repoRoot, forgejoBaseURL string, treeDB *cache.TreeDB, progress *app.ProgressManager) (source.RepositorySource, error) {
	switch sourceType {
	case "local":
		return source.NewLocalFS(repoRoot), nil
	case "forgejo":
		return source.NewForgejo(source.ForgejoConfig{
			BaseURL: forgejoBaseURL,
			HTTPClient: &http.Client{
				Timeout: 60 * time.Second,
			},
			TreeDB:     treeDB,
			OnProgress: progress.Notify,
		})
	default:
		return nil, fmt.Errorf("unknown ZIP_FORGER_SOURCE value %q (expected local or forgejo)", sourceType)
	}
}

func buildAuthManager(mode string, required bool, forgejoBaseURL string, logger *log.Logger) (*auth.Manager, error) {
	switch mode {
	case "", "none":
		if required {
			return auth.NewManager(auth.Config{
				Enabled:  false,
				Required: true,
			}, logger)
		}
		return nil, nil

	case "forgejo-oauth":
		sessionSecret := getenv("ZIP_FORGER_SESSION_SECRET", "")
		if strings.TrimSpace(sessionSecret) == "" {
			return nil, errors.New("ZIP_FORGER_SESSION_SECRET is required for forgejo-oauth mode")
		}
		redirectURL := getenv("ZIP_FORGER_OAUTH_REDIRECT_URL", "")
		if strings.TrimSpace(redirectURL) == "" {
			return nil, errors.New("ZIP_FORGER_OAUTH_REDIRECT_URL is required for forgejo-oauth mode")
		}

		scopes := parseCSV(getenv("ZIP_FORGER_OAUTH_SCOPES", "read:repository"))
		return auth.NewManager(auth.Config{
			Enabled:        true,
			Required:       required,
			ForgejoBaseURL: forgejoBaseURL,
			ClientID:       getenv("ZIP_FORGER_OAUTH_CLIENT_ID", ""),
			ClientSecret:   getenv("ZIP_FORGER_OAUTH_CLIENT_SECRET", ""),
			RedirectURL:    redirectURL,
			Scopes:         scopes,
			CookieName:     getenv("ZIP_FORGER_SESSION_COOKIE_NAME", "zip_forger_session"),
			CookieSecure:   getenv("ZIP_FORGER_SESSION_COOKIE_SECURE", "true") != "false",
			SessionSecret:  sessionSecret,
		}, logger)
	default:
		return nil, fmt.Errorf("unknown ZIP_FORGER_AUTH_MODE value %q (expected none or forgejo-oauth)", mode)
	}
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func defaultAuthMode(sourceType string) string {
	if sourceType == "forgejo" {
		return "forgejo-oauth"
	}
	return "none"
}

func parseBool(value string) *bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return nil
	}
	if value == "1" || value == "true" || value == "yes" || value == "on" {
		return boolPtr(true)
	}
	if value == "0" || value == "false" || value == "no" || value == "off" {
		return boolPtr(false)
	}
	return nil
}

func boolPtr(v bool) *bool {
	return &v
}
