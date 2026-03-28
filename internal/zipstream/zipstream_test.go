package zipstream

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
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

func TestVirtualArchiveStreamsValidZip(t *testing.T) {
	entries := []source.Entry{
		{Path: "a.txt", Size: 5},
		{Path: "nested/b.txt", Size: 4},
	}
	content := map[string]string{
		"a.txt":        "alpha",
		"nested/b.txt": "beta",
	}

	archive, err := NewVirtualArchive(entries)
	if err != nil {
		t.Fatalf("NewVirtualArchive failed: %v", err)
	}

	var out bytes.Buffer
	err = archive.StreamRange(
		context.Background(),
		&out,
		0,
		archive.Size(),
		func(_ context.Context, filePath string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(content[filePath])), nil
		},
		nil,
		func(_ context.Context, entry source.Entry) (uint32, error) {
			return crc32.ChecksumIEEE([]byte(content[entry.Path])), nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("StreamRange failed: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader failed: %v", err)
	}
	if len(reader.File) != 2 {
		t.Fatalf("expected 2 files, got %d", len(reader.File))
	}

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("ReadAll(%s) failed: %v", file.Name, err)
		}
		if got := string(body); got != content[file.Name] {
			t.Fatalf("unexpected content for %s: %q", file.Name, got)
		}
	}
}

func TestVirtualArchiveRangeMatchesFullArchiveSlice(t *testing.T) {
	entries := []source.Entry{
		{Path: "docs/guide.txt", Size: 20},
	}
	content := "0123456789ABCDEFGHIJ"

	archive, err := NewVirtualArchive(entries)
	if err != nil {
		t.Fatalf("NewVirtualArchive failed: %v", err)
	}

	var full bytes.Buffer
	err = archive.StreamRange(
		context.Background(),
		&full,
		0,
		archive.Size(),
		func(_ context.Context, filePath string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(content)), nil
		},
		nil,
		func(_ context.Context, entry source.Entry) (uint32, error) {
			return crc32.ChecksumIEEE([]byte(content)), nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("full StreamRange failed: %v", err)
	}

	dataStart := int64(30 + len(entries[0].Path))
	rangeStart := dataStart + 7
	rangeEnd := rangeStart + 5

	var rangeCalls int
	var partial bytes.Buffer
	err = archive.StreamRange(
		context.Background(),
		&partial,
		rangeStart,
		rangeEnd,
		func(_ context.Context, filePath string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(content)), nil
		},
		func(_ context.Context, filePath string, start, end int64) (io.ReadCloser, error) {
			rangeCalls++
			if start != 7 || end != 12 {
				return nil, fmt.Errorf("unexpected source range %d-%d", start, end)
			}
			return io.NopCloser(strings.NewReader(content[start:end])), nil
		},
		func(_ context.Context, entry source.Entry) (uint32, error) {
			return crc32.ChecksumIEEE([]byte(content)), nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("partial StreamRange failed: %v", err)
	}

	if rangeCalls != 1 {
		t.Fatalf("expected one ranged open, got %d", rangeCalls)
	}
	if !bytes.Equal(partial.Bytes(), full.Bytes()[rangeStart:rangeEnd]) {
		t.Fatalf("range bytes mismatch")
	}
}

func TestVirtualArchiveUsesZip64ForLargeAggregateOffsets(t *testing.T) {
	archive, err := NewVirtualArchive([]source.Entry{
		{Path: "part-1.bin", Size: 3 << 30},
		{Path: "part-2.bin", Size: 2 << 30},
	})
	if err != nil {
		t.Fatalf("NewVirtualArchive failed: %v", err)
	}
	if !archive.zip64 {
		t.Fatal("expected zip64 layout for archive larger than 4 GiB")
	}
	if archive.Size() <= 4<<30 {
		t.Fatalf("expected archive size above 4 GiB, got %d", archive.Size())
	}
}
