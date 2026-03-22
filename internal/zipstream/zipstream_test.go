package zipstream

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
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
	}, nil)
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

func TestStreamOnFileErrorSkipsMissingFiles(t *testing.T) {
	entries := []source.Entry{
		{Path: "exists.txt"},
		{Path: "missing.txt"},
		{Path: "also-exists.txt"},
	}

	errNotFound := errors.New("not found")

	var out bytes.Buffer
	err := Stream(context.Background(), &out, entries, func(_ context.Context, filePath string) (io.ReadCloser, error) {
		if filePath == "missing.txt" {
			return nil, errNotFound
		}
		return io.NopCloser(strings.NewReader("content-of-" + filePath)), nil
	}, &Options{
		OnFileError: func(_ string, _ error) error {
			return nil // skip
		},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}

	// Should have 2 content files + 1 warnings file = 3
	if len(reader.File) != 3 {
		names := make([]string, len(reader.File))
		for i, f := range reader.File {
			names[i] = f.Name
		}
		t.Fatalf("expected 3 files (2 content + warnings), got %d: %v", len(reader.File), names)
	}

	// Verify content files are correct
	if reader.File[0].Name != "exists.txt" {
		t.Fatalf("expected first file 'exists.txt', got %q", reader.File[0].Name)
	}
	if reader.File[1].Name != "also-exists.txt" {
		t.Fatalf("expected second file 'also-exists.txt', got %q", reader.File[1].Name)
	}

	// Verify warnings file
	warningsFile := reader.File[2]
	if warningsFile.Name != "__zip-forger-warnings.txt" {
		t.Fatalf("expected warnings file, got %q", warningsFile.Name)
	}
	rc, err := warningsFile.Open()
	if err != nil {
		t.Fatalf("failed to open warnings file: %v", err)
	}
	defer rc.Close()
	warningsBody, _ := io.ReadAll(rc)
	if !strings.Contains(string(warningsBody), "missing.txt") {
		t.Fatalf("warnings file should mention missing.txt, got: %q", string(warningsBody))
	}
}

func TestStreamOnFileErrorAborts(t *testing.T) {
	entries := []source.Entry{
		{Path: "a.txt"},
		{Path: "b.txt"},
	}

	fatalErr := errors.New("fatal source error")

	var out bytes.Buffer
	err := Stream(context.Background(), &out, entries, func(_ context.Context, filePath string) (io.ReadCloser, error) {
		if filePath == "b.txt" {
			return nil, fatalErr
		}
		return io.NopCloser(strings.NewReader("ok")), nil
	}, &Options{
		OnFileError: func(_ string, err error) error {
			return err // propagate
		},
	})
	if err == nil {
		t.Fatal("expected error from Stream")
	}
	if !errors.Is(err, fatalErr) {
		t.Fatalf("expected fatalErr, got: %v", err)
	}
}

func TestStreamWithoutOnFileErrorAbortsByDefault(t *testing.T) {
	entries := []source.Entry{
		{Path: "a.txt"},
	}

	openErr := errors.New("open failed")

	var out bytes.Buffer
	err := Stream(context.Background(), &out, entries, func(_ context.Context, _ string) (io.ReadCloser, error) {
		return nil, openErr
	}, nil)
	if !errors.Is(err, openErr) {
		t.Fatalf("expected openErr, got: %v", err)
	}
}

func TestStreamProducesValidZipWithNoDoubleClose(t *testing.T) {
	// Regression test: ensure no duplicate central directory from double-close
	entries := []source.Entry{
		{Path: "file.txt"},
	}

	var out bytes.Buffer
	err := Stream(context.Background(), &out, entries, func(_ context.Context, _ string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("hello world")), nil
	}, nil)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader failed (possible double-close corruption): %v", err)
	}
	if len(reader.File) != 1 {
		t.Fatalf("expected 1 file, got %d", len(reader.File))
	}

	rc, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("failed to open zip entry: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "hello world" {
		t.Fatalf("unexpected content: %q", string(body))
	}
}
