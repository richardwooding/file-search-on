package content

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"time"
)

// NewSingleFileFS returns an fs.FS containing exactly one regular
// file. Used by callers that need to feed in-memory bytes through
// the existing content-type Detect + Attributes pipeline (which
// takes an fs.FS) without depending on testing/fstest in production
// code. The returned file's reader satisfies io.ReaderAt, so
// content types that route through openReaderAt (office, EPUB, ZIP-
// envelope walkers, PDF) get random access without a second copy.
//
// name is the in-fs path the single file appears at — typically the
// archive-entry's recorded name, since the registry's longest-suffix
// extension matcher and the per-format parsers all key off the path.
//
// The returned FS is read-only and zero-allocation per Open beyond
// the wrapping struct. Any path other than name returns
// fs.ErrNotExist. Calling Open more than once returns independent
// readers over the same backing []byte (the bytes are NOT copied).
func NewSingleFileFS(name string, data []byte, modTime time.Time, mode fs.FileMode) fs.FS {
	return &singleFileFS{
		name:    name,
		data:    data,
		modTime: modTime,
		mode:    mode,
	}
}

type singleFileFS struct {
	name    string
	data    []byte
	modTime time.Time
	mode    fs.FileMode
}

func (s *singleFileFS) Open(name string) (fs.File, error) {
	if name != s.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &singleFile{
		Reader: bytes.NewReader(s.data),
		info: singleFileInfo{
			name:    path.Base(s.name),
			size:    int64(len(s.data)),
			modTime: s.modTime,
			mode:    s.mode,
		},
	}, nil
}

// singleFile wraps a bytes.Reader to satisfy fs.File. The embedded
// *bytes.Reader gives us io.Reader, io.ReaderAt, io.Seeker, and
// io.WriterTo for free — exactly the surface openReaderAt and the
// per-format parsers expect from a "regular file."
type singleFile struct {
	*bytes.Reader
	info   singleFileInfo
	closed bool
}

func (s *singleFile) Stat() (fs.FileInfo, error) { return s.info, nil }

func (s *singleFile) Close() error {
	if s.closed {
		return errors.New("singleFile: already closed")
	}
	s.closed = true
	return nil
}

// Read short-circuits to ErrClosed after Close. The embedded Reader's
// own Read would happily continue serving bytes; the explicit guard
// matches os.File semantics.
func (s *singleFile) Read(p []byte) (int, error) {
	if s.closed {
		return 0, fs.ErrClosed
	}
	return s.Reader.Read(p)
}

// ReadAt has the same closed-after-Close guard. Surface kept tight
// because openReaderAt's type assertion is on the FILE (not the
// FS); the file's embedded *bytes.Reader satisfies io.ReaderAt
// directly, and this override preserves Close semantics.
func (s *singleFile) ReadAt(p []byte, off int64) (int, error) {
	if s.closed {
		return 0, fs.ErrClosed
	}
	return s.Reader.ReadAt(p, off)
}

type singleFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	mode    fs.FileMode
}

func (i singleFileInfo) Name() string       { return i.name }
func (i singleFileInfo) Size() int64        { return i.size }
func (i singleFileInfo) Mode() fs.FileMode  { return i.mode }
func (i singleFileInfo) ModTime() time.Time { return i.modTime }
func (i singleFileInfo) IsDir() bool        { return false }
func (i singleFileInfo) Sys() any           { return nil }

// Compile-time assertions that singleFile / singleFileFS satisfy the
// interfaces callers expect. If any of these break, you'll see it at
// build time instead of via a runtime type-assertion miss.
var (
	_ fs.FS       = (*singleFileFS)(nil)
	_ fs.File     = (*singleFile)(nil)
	_ io.ReaderAt = (*singleFile)(nil)
)
