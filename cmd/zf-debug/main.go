package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"zip-forger/internal/cache"
	"zip-forger/internal/filter"
	"zip-forger/internal/source"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: zf-debug <base_url> <owner/repo> <ref> [prefix] [extension]")
		fmt.Println("Example: zf-debug https://forge.tionis.dev GSB/systems main Amber")
		os.Exit(1)
	}

	baseURL := os.Args[1]
	fullRepo := os.Args[2]
	ref := os.Args[3]
	
	var prefix, ext string
	if len(os.Args) > 4 {
		prefix = os.Args[4]
	}
	if len(os.Args) > 5 {
		ext = os.Args[5]
	}

	parts := strings.Split(fullRepo, "/")
	if len(parts) != 2 {
		log.Fatal("Invalid repo format, expected owner/repo")
	}
	owner, repo := parts[0], parts[1]

	logger := log.New(os.Stdout, "[DEBUG] ", log.LstdFlags)
	
	dbPath := "./debug-trees.db"
	_ = os.Remove(dbPath) // Start fresh
	db, err := cache.NewTreeDB(dbPath, logger)
	if err != nil {
		log.Fatalf("Failed to init DB: %v", err)
	}
	defer db.Close()

	token := os.Getenv("FORGEJO_TOKEN")
	httpClient := &http.Client{}
	
	src, err := source.NewForgejo(source.ForgejoConfig{
		BaseURL:    baseURL,
		HTTPClient: httpClient,
		TreeDB:     db,
	})
	if err != nil {
		log.Fatalf("Failed to init source: %v", err)
	}

	ctx := context.Background()
	if token != "" {
		ctx = source.WithAccessToken(ctx, token)
		logger.Println("Using provided FORGEJO_TOKEN")
	}

	logger.Printf("Resolving ref %s...", ref)
	sha, err := src.ResolveRef(ctx, owner, repo, ref)
	if err != nil {
		log.Fatalf("Failed to resolve ref: %v", err)
	}
	logger.Printf("Resolved to commit SHA: %s", sha)

	criteria := filter.Criteria{}
	if prefix != "" {
		criteria.PathPrefixes = []string{prefix}
	}
	if ext != "" {
		criteria.Extensions = []string{ext}
	}

	logger.Printf("Listing files with criteria: %+v", criteria)
	entries, err := src.ListFiles(ctx, owner, repo, sha, criteria)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[FATAL ERROR] ListFiles failed: %v\n", err)
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "Context Error: %v\n", ctx.Err())
		}
		
		// Still print stats even on failure
		printDBStats(db.RawDB(), sha)
		os.Exit(1)
	}

	fmt.Printf("\n--- Found %d matches ---\n", len(entries))
	limit := 20
	for i, e := range entries {
		if i >= limit {
			fmt.Printf("... and %d more\n", len(entries)-limit)
			break
		}
		fmt.Printf("[%d] %s (%d bytes)\n", i+1, e.Path, e.Size)
	}

	printDBStats(db.RawDB(), sha)
}

func printDBStats(rawDB *sql.DB, sha string) {
	if rawDB == nil {
		return
	}
	var count int
	_ = rawDB.QueryRow("SELECT COUNT(*) FROM tree_entries").Scan(&count)
	fmt.Printf("\n--- DB Stats ---\n")
	fmt.Printf("Total entries in tree_entries table: %d\n", count)
	
	fmt.Printf("\n--- Root Level Samples (Parent SHA: %s) ---\n", sha)
	rows, err := rawDB.Query("SELECT path, type, sha FROM tree_entries WHERE parent_tree_sha = ? LIMIT 10", sha)
	if err != nil {
		fmt.Printf("Error querying root samples: %v\n", err)
		return
	}
	if rows != nil {
		defer rows.Close()
		found := false
		for rows.Next() {
			found = true
			var p, t, s string
			_ = rows.Scan(&p, &t, &s)
			fmt.Printf("  -> %s [%s] (sha: %s)\n", p, t, s)
		}
		if !found {
			fmt.Println("  (No entries found for this parent SHA)")
		}
	}
}
