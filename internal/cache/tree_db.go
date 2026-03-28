package cache

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"zip-forger/internal/filter"
	"zip-forger/internal/source"
)

type TreeDB struct {
	db     *sql.DB
	logger *log.Logger
	mu     sync.Mutex
}

func NewTreeDB(path string, logger *log.Logger) (*TreeDB, error) {
	if logger == nil {
		logger = log.Default()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("cache: failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("cache: failed to open sqlite db: %w", err)
	}

	// Optimize for performance
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA synchronous=NORMAL")

	schema := `
	CREATE TABLE IF NOT EXISTS indexed_trees (
		sha TEXT PRIMARY KEY,
		indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tree_entries (
		parent_tree_sha TEXT,
		path TEXT,
		name TEXT,
		type TEXT,
		size INTEGER,
		sha TEXT,
		PRIMARY KEY (parent_tree_sha, path)
	);

	CREATE INDEX IF NOT EXISTS idx_entries_parent ON tree_entries(parent_tree_sha);
	CREATE INDEX IF NOT EXISTS idx_entries_path ON tree_entries(path);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("cache: failed to initialize schema: %w", err)
	}

	return &TreeDB{
		db:     db,
		logger: logger,
	}, nil
}

func (c *TreeDB) IsIndexed(ctx context.Context, sha string) (bool, error) {
	var exists bool
	err := c.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM indexed_trees WHERE sha = ?)", sha).Scan(&exists)
	return exists, err
}

func (c *TreeDB) MarkIndexed(ctx context.Context, sha string) error {
	_, err := c.db.ExecContext(ctx, "INSERT OR IGNORE INTO indexed_trees (sha) VALUES (?)", sha)
	return err
}

func (c *TreeDB) SaveEntries(ctx context.Context, parentSHA string, entries []struct {
	Path string
	Type string
	Size int64
	SHA  string
}) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO tree_entries (parent_tree_sha, path, name, type, size, sha) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		name := filepath.Base(entry.Path)
		if _, err := stmt.ExecContext(ctx, parentSHA, entry.Path, name, entry.Type, entry.Size, entry.SHA); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (c *TreeDB) GetFullTree(ctx context.Context, rootSHA string) ([]source.Entry, error) {
	query := `
	WITH RECURSIVE walk(full_path, type, size, sha) AS (
		SELECT path, type, size, sha FROM tree_entries WHERE parent_tree_sha = ?
		UNION ALL
		SELECT walk.full_path || '/' || tree_entries.path, tree_entries.type, tree_entries.size, tree_entries.sha
		FROM walk
		JOIN tree_entries ON tree_entries.parent_tree_sha = walk.sha
		WHERE walk.type = 'tree'
	)
	SELECT full_path, size FROM walk WHERE type = 'blob'
	`
	rows, err := c.db.QueryContext(ctx, query, rootSHA)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []source.Entry
	for rows.Next() {
		var e source.Entry
		if err := rows.Scan(&e.Path, &e.Size); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Search returns all blobs under a root SHA that match prefix and/or extension criteria.
func (c *TreeDB) Search(ctx context.Context, rootSHA string, criteria filter.Criteria) ([]source.Entry, error) {
	cte := `
	WITH RECURSIVE walk(full_path, type, size, sha) AS (
		SELECT path, type, size, sha FROM tree_entries WHERE parent_tree_sha = ?
		UNION ALL
		SELECT walk.full_path || '/' || tree_entries.path, tree_entries.type, tree_entries.size, tree_entries.sha
		FROM walk
		JOIN tree_entries ON tree_entries.parent_tree_sha = walk.sha
		WHERE walk.type = 'tree'
	)
	SELECT full_path, size FROM walk WHERE type = 'blob'`

	whereParts := []string{}
	args := []any{rootSHA}

	if len(criteria.PathPrefixes) > 0 {
		var prefixParts []string
		for _, p := range criteria.PathPrefixes {
			p = strings.TrimRight(p, "/")
			if p == "" {
				continue
			}
			// Strict directory prefix matching: either the path is the prefix, or it starts with "prefix/"
			prefixParts = append(prefixParts, "(full_path = ? OR full_path LIKE ?)")
			args = append(args, p, p+"/%")
		}
		if len(prefixParts) > 0 {
			whereParts = append(whereParts, "("+strings.Join(prefixParts, " OR ")+")")
		}
	}

	if len(criteria.Extensions) > 0 {
		var extParts []string
		for _, e := range criteria.Extensions {
			prefixParts := ""
			if !strings.HasPrefix(e, ".") {
				prefixParts = "."
			}
			extParts = append(extParts, "full_path LIKE ?")
			args = append(args, "%"+prefixParts+e)
		}
		whereParts = append(whereParts, "("+strings.Join(extParts, " OR ")+")")
	}

	query := cte
	if len(whereParts) > 0 {
		query += " AND " + strings.Join(whereParts, " AND ")
	}

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []source.Entry
	for rows.Next() {
		var e source.Entry
		if err := rows.Scan(&e.Path, &e.Size); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (c *TreeDB) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
