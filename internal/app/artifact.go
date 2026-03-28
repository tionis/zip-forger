package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

type ArtifactStore struct {
	root  string
	group singleflight.Group
}

type Artifact struct {
	Key     string
	Path    string
	Size    int64
	ModTime time.Time
}

func NewArtifactStore(root string) *ArtifactStore {
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join(os.TempDir(), "zip-forger-downloads")
	}
	return &ArtifactStore{root: root}
}

func (s *ArtifactStore) Ensure(ctx context.Context, key string, build func(context.Context, io.Writer) error) (Artifact, error) {
	if artifact, ok := s.lookup(key); ok {
		return artifact, nil
	}

	value, err, _ := s.group.Do(key, func() (any, error) {
		if artifact, ok := s.lookup(key); ok {
			return artifact, nil
		}
		if err := os.MkdirAll(s.root, 0o755); err != nil {
			return Artifact{}, err
		}

		tmp, err := os.CreateTemp(s.root, key+".*.tmp")
		if err != nil {
			return Artifact{}, err
		}

		tmpName := tmp.Name()
		removeTemp := func() {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}

		if err := build(ctx, tmp); err != nil {
			removeTemp()
			return Artifact{}, err
		}
		if err := tmp.Sync(); err != nil {
			removeTemp()
			return Artifact{}, err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			return Artifact{}, err
		}

		finalPath := s.finalPath(key)
		if err := os.Rename(tmpName, finalPath); err != nil {
			_ = os.Remove(tmpName)
			return Artifact{}, err
		}

		artifact, ok := s.lookup(key)
		if !ok {
			return Artifact{}, os.ErrNotExist
		}
		return artifact, nil
	})
	if err != nil {
		return Artifact{}, err
	}
	return value.(Artifact), nil
}

func (s *ArtifactStore) lookup(key string) (Artifact, bool) {
	info, err := os.Stat(s.finalPath(key))
	if err != nil || info.IsDir() || info.Size() == 0 {
		return Artifact{}, false
	}
	return Artifact{
		Key:     key,
		Path:    s.finalPath(key),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, true
}

func (s *ArtifactStore) finalPath(key string) string {
	return filepath.Join(s.root, key+".zip")
}
