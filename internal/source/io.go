package source

import "io"

type readCloser struct {
	io.Reader
	io.Closer
}
