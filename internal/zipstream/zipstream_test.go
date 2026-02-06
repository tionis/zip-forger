package zipstream

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"zip-forger/internal/source"
)

func TestStreamCreatesStoreEntriesWithFixedModTime(t *testing.T) {
	entries := []source.Entry{
		{Path: "a.txt"},
		{Path: "b.txt"},
	}
	content := map[string]string{
		"a.txt": "alpha",
		"b.txt": "beta",
	}

	var out bytes.Buffer
	err := Stream(context.Background(), &out, entries, func(_ context.Context, filePath string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(content[filePath])), nil
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}
	if len(reader.File) != 2 {
		t.Fatalf("expected 2 files, got %d", len(reader.File))
	}
	for _, file := range reader.File {
		if file.Method != zip.Store {
			t.Fatalf("expected zip.Store method for %s", file.Name)
		}
		if !file.Modified.Equal(fixedModTime) {
			t.Fatalf("unexpected modified timestamp for %s: %v", file.Name, file.Modified)
		}
	}
}
