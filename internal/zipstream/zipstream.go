package zipstream

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"zip-forger/internal/source"
)

// OpenFunc returns a reader for the given file path.
type OpenFunc func(ctx context.Context, filePath string) (io.ReadCloser, error)

// OnFileErrorFunc is called when opening a file fails. Return nil to skip
// the file and continue streaming; return a non-nil error to abort.
type OnFileErrorFunc func(path string, err error) error

var fixedModTime = time.Unix(0, 0).UTC()

// Options configures the streaming behavior.
type Options struct {
	OnFileError OnFileErrorFunc
}

func Stream(ctx context.Context, w io.Writer, entries []source.Entry, open OpenFunc, opts *Options) error {
	zw := zip.NewWriter(w)

	var onFileError OnFileErrorFunc
	if opts != nil && opts.OnFileError != nil {
		onFileError = opts.OnFileError
	}

	var skipped []string
	buffer := make([]byte, 128*1024)
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		reader, err := open(ctx, entry.Path)
		if err != nil {
			if onFileError != nil {
				if handlerErr := onFileError(entry.Path, err); handlerErr != nil {
					return handlerErr
				}
				skipped = append(skipped, entry.Path)
				continue
			}
			return err
		}

		header := &zip.FileHeader{
			Name:   entry.Path,
			Method: zip.Store,
		}
		header.SetModTime(fixedModTime)

		writer, err := zw.CreateHeader(header)
		if err != nil {
			reader.Close()
			return err
		}

		_, copyErr := io.CopyBuffer(writer, reader, buffer)
		closeErr := reader.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	if len(skipped) > 0 {
		if err := writeWarnings(zw, skipped); err != nil {
			return err
		}
	}

	return zw.Close()
}

func writeWarnings(zw *zip.Writer, skipped []string) error {
	header := &zip.FileHeader{
		Name:   "__zip-forger-warnings.txt",
		Method: zip.Store,
	}
	header.SetModTime(fixedModTime)

	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("The following files were unavailable during download and have been skipped:\n\n")
	for _, path := range skipped {
		fmt.Fprintf(&b, "  - %s\n", path)
	}
	_, err = io.WriteString(w, b.String())
	return err
}
