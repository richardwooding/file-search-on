package content

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveEntry is one file (or directory) inside an archive. Open()
// returns a fresh io.ReadCloser positioned at the entry's start; the
// caller MUST close it before the next visit callback runs (TAR
// iteration is sequential — leaving a previous entry's reader open
// breaks the stream).
//
// CompressedSize is 0 for TAR / TAR.GZ / GZIP (which don't expose
// per-entry compressed sizes); Size is always the uncompressed size.
// ModTime is the entry's recorded timestamp (zero if absent).
type ArchiveEntry struct {
	Name           string
	Size           int64
	CompressedSize int64
	ModTime        time.Time
	IsDir          bool
	Mode           fs.FileMode
	Open           func() (io.ReadCloser, error)
}

// ErrStopIteration is the sentinel a visitor returns to halt
// IterateArchive cleanly without surfacing an error to the caller.
// Useful for "stop at first match" loops.
var ErrStopIteration = errors.New("archive iteration stopped")

// IterateArchive opens the archive at fsys+path and calls visit for
// each entry in order. The archive's content type — one of
// "archive/zip", "archive/tar", "archive/tar+gzip", "archive/gzip" —
// selects the underlying parser; passing a different value returns
// an error.
//
// For sequential formats (TAR / TAR.GZ) the visitor MUST close the
// entry reader (if it called Open) before returning; leaving a
// previous entry's reader open breaks the next Next() call. For
// random-access ZIP this isn't strictly required (each Open returns
// an independent reader) but the same hygiene rule keeps the
// contract uniform.
//
// Returning ErrStopIteration from visit halts cleanly (no error to
// caller); returning any other non-nil error halts and surfaces the
// error.
func IterateArchive(ctx context.Context, fsys fs.FS, path, contentType string, visit func(ArchiveEntry) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch contentType {
	case "archive/zip":
		return iterateZIP(ctx, fsys, path, visit)
	case "archive/tar":
		return iterateTAR(ctx, fsys, path, false, visit)
	case "archive/tar+gzip":
		return iterateTAR(ctx, fsys, path, true, visit)
	case "archive/gzip":
		return iterateGZIP(ctx, fsys, path, visit)
	default:
		return errors.New("IterateArchive: unsupported content type " + contentType)
	}
}

func iterateZIP(ctx context.Context, fsys fs.FS, path string, visit func(ArchiveEntry) error) error {
	ra, size, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return err
	}
	defer func() { _ = closer() }()
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := ArchiveEntry{
			Name:           f.Name,
			Size:           int64(f.UncompressedSize64),
			CompressedSize: int64(f.CompressedSize64),
			ModTime:        f.Modified,
			IsDir:          f.FileInfo().IsDir(),
			Mode:           f.Mode(),
			Open: func() (io.ReadCloser, error) {
				return f.Open()
			},
		}
		if err := visit(entry); err != nil {
			if errors.Is(err, ErrStopIteration) {
				return nil
			}
			return err
		}
	}
	return nil
}

func iterateTAR(ctx context.Context, fsys fs.FS, path string, gz bool, visit func(ArchiveEntry) error) error {
	f, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	if gz {
		zr, gerr := gzip.NewReader(f)
		if gerr != nil {
			return gerr
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}

	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, hErr := tr.Next()
		if hErr == io.EOF {
			return nil
		}
		if hErr != nil {
			// Mid-stream corruption: return what we've delivered so
			// far rather than failing. Mirrors readTARArchive.
			return nil
		}
		// Skip PAX / GNU global extension headers — they don't
		// represent user-visible entries.
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			continue
		}
		entry := ArchiveEntry{
			Name:    hdr.Name,
			Size:    hdr.Size,
			ModTime: hdr.ModTime,
			IsDir:   hdr.FileInfo().IsDir(),
			Mode:    hdr.FileInfo().Mode(),
			// TAR's reader is positioned at this entry's bytes
			// until Next() is called. Open returns a no-op closer
			// over the tar.Reader (which acts as an io.Reader for
			// the current entry); the visitor must NOT call
			// Open() after returning from visit, since by then the
			// stream has advanced.
			Open: func() (io.ReadCloser, error) {
				return io.NopCloser(tr), nil
			},
		}
		if err := visit(entry); err != nil {
			if errors.Is(err, ErrStopIteration) {
				return nil
			}
			return err
		}
	}
}

func iterateGZIP(ctx context.Context, fsys fs.FS, path string, visit func(ArchiveEntry) error) error {
	f, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	// Sniff the uncompressed name from the gzip header when present.
	// Falls back to "<basename-without-.gz>" so the entry is at least
	// addressable.
	name := zr.Name
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, ".gz")
	}
	entry := ArchiveEntry{
		Name:    name,
		Size:    0, // gzip doesn't expose uncompressed size in the header
		ModTime: zr.ModTime,
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(zr), nil
		},
	}
	if err := visit(entry); err != nil && !errors.Is(err, ErrStopIteration) {
		return err
	}
	_ = ctx // single-entry — no per-entry cancellation point beyond entry
	return nil
}
