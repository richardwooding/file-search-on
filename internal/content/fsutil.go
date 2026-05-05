package content

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
)

// openReadSeeker opens path on fsys and returns it as an io.ReadSeeker plus
// the file size. If the underlying fs.File already supports io.Seeker — as
// *os.File does when fsys is os.DirFS — it's returned directly so large
// media files don't have to be slurped into memory. If the fs.File is
// sequential-only (embed.FS, fstest.MapFS, gzip-fs wrappers, …) the file
// is read into memory once and wrapped in a *bytes.Reader, which satisfies
// both io.ReadSeeker and io.ReaderAt — exactly what zip.NewReader and
// pdf.NewReader need.
//
// The returned closer must always be deferred. It's a no-op when the file
// was slurped (the underlying fs.File was already closed inside this
// function); for the streaming fast path it closes the underlying file.
func openReadSeeker(fsys fs.FS, path string) (io.ReadSeeker, int64, func() error, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		// Production fast path: streaming via *os.File.
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, 0, nil, err
		}
		return rs, info.Size(), f.Close, nil
	}
	// Sequential-only fs.File (embed.FS, fstest.MapFS, …): slurp.
	buf, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, 0, nil, err
	}
	return bytes.NewReader(buf), int64(len(buf)), func() error { return nil }, nil
}

// openReaderAt is the io.ReaderAt-flavoured cousin of openReadSeeker, for
// callers like zip.NewReader and pdf.NewReader that need ReaderAt rather
// than ReadSeeker. The returned reader satisfies both. Closer rules are
// the same.
func openReaderAt(fsys fs.FS, path string) (io.ReaderAt, int64, func() error, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, 0, nil, err
	}
	if ra, ok := f.(io.ReaderAt); ok {
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, 0, nil, err
		}
		return ra, info.Size(), f.Close, nil
	}
	buf, readErr := io.ReadAll(f)
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, 0, nil, err
	}
	return bytes.NewReader(buf), int64(len(buf)), func() error { return nil }, nil
}

// readAll reads the entire contents of path on fsys. Used by the
// markdown / json / xml / csv / html / text parsers that don't need
// random access.
func readAll(fsys fs.FS, path string) ([]byte, error) {
	return fs.ReadFile(fsys, path)
}
