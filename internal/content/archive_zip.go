package content

import (
	"archive/zip"
	"context"
	"io/fs"
)

// readZIPArchive parses a ZIP archive's central directory, counting
// entries and summing uncompressed sizes. archive/zip reads the
// directory eagerly via NewReader, so this is fast even for large
// archives — we don't decompress payloads. ctx is checked at entry
// and inside the central-directory iteration so pathological-archive
// inputs (millions of entries) surrender to a cancelled context.
func readZIPArchive(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ra, size, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr // graceful degradation: malformed archive returns empty attrs
	}

	var entryCount, uncompressed int64
	tops := map[string]struct{}{}
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return archiveAttrs(entryCount, uncompressed, tops), err
		}
		entryCount++
		uncompressed += int64(f.UncompressedSize64)
		if t := topLevelOf(f.Name); t != "" {
			tops[t] = struct{}{}
		}
	}
	return archiveAttrs(entryCount, uncompressed, tops), nil
}
