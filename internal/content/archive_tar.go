package content

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
)

// readTARArchive walks a tar archive (optionally wrapped in gzip),
// counting entries and summing per-entry sizes from the headers. tar
// streams sequentially — no random access needed — so this scales
// cleanly to multi-GB archives. Each header read is O(1); the body
// content is never decompressed beyond what tar.Reader needs to skip.
//
// gz=true wraps the file in compress/gzip first. A standalone .gz
// (single file, no tar inside) is handled by readGZIPArchive instead.
// ctx is checked between every header so a multi-GB archive surrenders
// to a cancelled context within one entry's worth of work.
func readTARArchive(ctx context.Context, fsys fs.FS, path string, gz bool) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var r io.Reader = f
	if gz {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return Attributes{}, nil // malformed gzip → empty attrs
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}

	tr := tar.NewReader(r)
	var entryCount, uncompressed int64
	tops := map[string]struct{}{}
	for {
		if err := ctx.Err(); err != nil {
			return archiveAttrs(entryCount, uncompressed, tops), err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Mid-stream corruption — surface what we have so far rather
			// than dropping the whole file from results.
			break
		}
		entryCount++
		// PAX / GNU extension headers don't represent user-visible
		// entries; skip them in the count and roots tally.
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			entryCount-- // un-count
			continue
		}
		uncompressed += hdr.Size
		if t := topLevelOf(hdr.Name); t != "" {
			tops[t] = struct{}{}
		}
	}
	return archiveAttrs(entryCount, uncompressed, tops), nil
}
