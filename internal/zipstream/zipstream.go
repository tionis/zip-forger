package zipstream

import (
	"archive/zip"
	"context"
	"io"
	"time"

	"zip-forger/internal/source"
)

type OpenFunc func(ctx context.Context, filePath string) (io.ReadCloser, error)

var fixedModTime = time.Unix(0, 0).UTC()

func Stream(ctx context.Context, w io.Writer, entries []source.Entry, open OpenFunc) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	buffer := make([]byte, 128*1024)
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header := &zip.FileHeader{
			Name:   entry.Path,
			Method: zip.Store,
		}
		header.SetModTime(fixedModTime)

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		reader, err := open(ctx, entry.Path)
		if err != nil {
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

	return zw.Close()
}
