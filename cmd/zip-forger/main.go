package main

import (
	"context"
	"errors"
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
	forgejoBaseURL := strings.TrimSpace(getenv("ZIP_FORGER_FORGEJO_BASE_URL", ""))
	sessionSecret := strings.TrimSpace(getenv("ZIP_FORGER_SESSION_SECRET", ""))
	if forgejoBaseURL == "" {
		logger.Fatal("source setup failed: ZIP_FORGER_FORGEJO_BASE_URL is required")
	}

	repoSource, err := buildSource(forgejoBaseURL, treeDB, progressManager)
	if err != nil {
		logger.Fatalf("source setup failed: %v", err)
	}

	authManager, err := buildAuthManager(forgejoBaseURL, logger)
	if err != nil {
		logger.Fatalf("auth setup failed: %v", err)
	}

	svc := app.NewServer(app.Dependencies{
		Source:        repoSource,
		ManifestCache: cache.NewManifestCache(5*time.Minute, 1024),
		Auth:          authManager,
		Progress:      progressManager,
		ArtifactStore: app.NewArtifactStore(filepath.Join(cacheDir, "downloads")),
		PrivateURL:    buildPrivateURLCodec(sessionSecret, logger),
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
		logger.Printf("zip-forger listening on %s (source=forgejo)", addr)
		logger.Printf("forgejo base URL: %s", forgejoBaseURL)
		if authManager != nil {
			logger.Printf("auth: forgejo oauth enabled (required=true)")
		} else {
			logger.Printf("auth: disabled")
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

func buildSource(forgejoBaseURL string, treeDB *cache.TreeDB, progress *app.ProgressManager) (source.RepositorySource, error) {
	return source.NewForgejo(source.ForgejoConfig{
		BaseURL: forgejoBaseURL,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		TreeDB:       treeDB,
		OnProgress:   progress.Notify,
		OnFinalizing: progress.Finalize,
	})
}

func buildAuthManager(forgejoBaseURL string, logger *log.Logger) (*auth.Manager, error) {
	sessionSecret := getenv("ZIP_FORGER_SESSION_SECRET", "")
	if strings.TrimSpace(sessionSecret) == "" {
		return nil, errors.New("ZIP_FORGER_SESSION_SECRET is required")
	}

	scopes := parseCSV(getenv("ZIP_FORGER_OAUTH_SCOPES", "write:repository"))
	return auth.NewManager(auth.Config{
		ForgejoBaseURL: forgejoBaseURL,
		ClientID:       getenv("ZIP_FORGER_OAUTH_CLIENT_ID", ""),
		ClientSecret:   getenv("ZIP_FORGER_OAUTH_CLIENT_SECRET", ""),
		RedirectURL:    getenv("ZIP_FORGER_OAUTH_REDIRECT_URL", ""),
		Scopes:         scopes,
		CookieName:     getenv("ZIP_FORGER_SESSION_COOKIE_NAME", "zip_forger_session"),
		CookieSecure:   getenv("ZIP_FORGER_SESSION_COOKIE_SECURE", "true") != "false",
		SessionSecret:  sessionSecret,
	}, logger)
}

func buildPrivateURLCodec(sessionSecret string, logger *log.Logger) *app.PrivateDownloadCodec {
	sessionSecret = strings.TrimSpace(sessionSecret)
	if sessionSecret == "" {
		logger.Printf("private download URLs disabled: ZIP_FORGER_SESSION_SECRET is not configured")
		return nil
	}

	ttl := 24 * time.Hour
	if rawTTL := strings.TrimSpace(getenv("ZIP_FORGER_DOWNLOAD_URL_TTL", "")); rawTTL != "" {
		parsedTTL, err := time.ParseDuration(rawTTL)
		if err != nil {
			logger.Fatalf("invalid ZIP_FORGER_DOWNLOAD_URL_TTL value %q: %v", rawTTL, err)
		}
		ttl = parsedTTL
	}

	codec, err := app.NewPrivateDownloadCodec(sessionSecret+"\x00private-download-url", ttl)
	if err != nil {
		logger.Printf("private download URLs disabled: %v", err)
		return nil
	}
	return codec
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
