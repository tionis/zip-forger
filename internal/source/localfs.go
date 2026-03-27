package source

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"zip-forger/internal/filter"
)

type LocalFS struct {
	root string
}

func NewLocalFS(root string) *LocalFS {
	return &LocalFS{root: root}
}

func (s *LocalFS) ResolveRef(_ context.Context, owner, repo, ref string) (string, error) {
	repoRoot := filepath.Join(s.root, owner, repo)
	info, err := os.Stat(repoRoot)
	if err != nil || !info.IsDir() {
		return "", ErrNotFound
	}

	if ref != "" {
		refRoot := filepath.Join(repoRoot, ref)
		info, err := os.Stat(refRoot)
		if err != nil || !info.IsDir() {
			return "", ErrNotFound
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
		return "", ErrNotFound
	}
	return refs[0], nil
}

func (s *LocalFS) ReadFile(_ context.Context, owner, repo, commit, filePath string) ([]byte, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	fullPath, err := safeJoin(base, filePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *LocalFS) ListFiles(_ context.Context, owner, repo, commit string, _ filter.Criteria) ([]Entry, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return nil, ErrNotFound
	}

	entries := make([]Entry, 0, 1024)
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
		entries = append(entries, Entry{
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

func (s *LocalFS) OpenFile(_ context.Context, owner, repo, commit, filePath string) (io.ReadCloser, error) {
	base := filepath.Join(s.root, owner, repo, commit)
	fullPath, err := safeJoin(base, filePath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *LocalFS) SearchRepos(_ context.Context, query string) ([]string, error) {
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

func (s *LocalFS) ListBranches(_ context.Context, owner, repo string) ([]string, error) {
	root := filepath.Join(s.root, owner, repo)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
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

func (s *LocalFS) UpsertFile(_ context.Context, owner, repo, branch, filePath string, data []byte, _ string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	base := filepath.Join(s.root, owner, repo, branch)
	targetPath, err := safeJoin(base, filePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func safeJoin(base, relativePath string) (string, error) {
	base = filepath.Clean(base)
	cleanPath := strings.ReplaceAll(strings.TrimSpace(relativePath), "\\", "/")
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = path.Clean("/" + cleanPath)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if cleanPath == "." {
		cleanPath = ""
	}

	target := filepath.Clean(filepath.Join(base, filepath.FromSlash(cleanPath)))
	if target == base {
		return target, nil
	}
	if !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", errors.New("source: invalid path")
	}
	return target, nil
}
