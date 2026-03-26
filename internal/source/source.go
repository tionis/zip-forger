package source

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("source: not found")
var ErrUnauthorized = errors.New("source: unauthorized")
var ErrUnsupportedSearchMode = errors.New("source: unsupported search mode")

type Entry struct {
	Path string
	Size int64
}

type RepositorySource interface {
	ResolveRef(ctx context.Context, owner, repo, ref string) (string, error)
	ReadFile(ctx context.Context, owner, repo, commit, filePath string) ([]byte, error)
	ListFiles(ctx context.Context, owner, repo, commit string) ([]Entry, error)
	OpenFile(ctx context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error)
	SearchRepos(ctx context.Context, query string) ([]string, error)
	ListBranches(ctx context.Context, owner, repo string) ([]string, error)
	UpsertFile(ctx context.Context, owner, repo, branch, filePath string, data []byte, message string) error
}
