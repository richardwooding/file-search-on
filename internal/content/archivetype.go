package content

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
)

func init() {
	Register(&archiveType{name: "archive/zip", exts: []string{".zip", ".jar", ".war", ".ear"}, magic: [][]byte{{0x50, 0x4B, 0x03, 0x04}, {0x50, 0x4B, 0x05, 0x06}, {0x50, 0x4B, 0x07, 0x08}}})
	Register(&archiveType{name: "archive/tar", exts: []string{".tar"}, magic: nil})
	Register(&archiveType{name: "archive/tar+gzip", exts: []string{".tar.gz", ".tgz"}, magic: nil})
	Register(&archiveType{name: "archive/gzip", exts: []string{".gz"}, magic: [][]byte{{0x1F, 0x8B}}})
}

type archiveType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (a *archiveType) Name() string         { return a.name }
func (a *archiveType) Extensions() []string { return a.exts }
func (a *archiveType) MagicBytes() [][]byte { return a.magic }

// Attributes dispatches to a per-format archive parser. All parsers
// produce the same surface — entry_count, uncompressed_size,
// top_level_entries, has_root_dir — so callers can filter without
// knowing the format. ctx is honoured at entry AND inside the TAR
// walker's per-entry loop (huge multi-GB tarballs are the worst case
// for unbounded mid-parse work).
func (a *archiveType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch a.name {
	case "archive/zip":
		return readZIPArchive(ctx, fsys, path)
	case "archive/tar":
		return readTARArchive(ctx, fsys, path, false)
	case "archive/tar+gzip":
		return readTARArchive(ctx, fsys, path, true)
	case "archive/gzip":
		return readGZIPArchive(fsys, path)
	}
	return nil, errors.New("unsupported archive type")
}

// archiveAttrs packs the common surface into a content.Attributes map.
// Keys match the four CEL attributes registered for the family.
// topLevelEntries is sorted+deduped before being stashed.
func archiveAttrs(entryCount int64, uncompressedSize int64, topLevelEntries map[string]struct{}) Attributes {
	out := make([]string, 0, len(topLevelEntries))
	for k := range topLevelEntries {
		out = append(out, k)
	}
	sort.Strings(out)
	hasRootDir := len(out) == 1
	return Attributes{
		"entry_count":       entryCount,
		"uncompressed_size": uncompressedSize,
		"top_level_entries": out,
		"has_root_dir":      hasRootDir,
	}
}

// topLevelOf returns the first path segment of `name`, or "" for the
// degenerate empty case. Trailing slashes (directory entries in zip /
// tar) are normalised away so "foo/" and "foo/bar" both contribute the
// same root "foo".
func topLevelOf(name string) string {
	name = strings.TrimLeft(name, "/")
	if name == "" {
		return ""
	}
	if before, _, ok := strings.Cut(name, "/"); ok {
		return before
	}
	// No slash — the entry IS a top-level file.
	return name
}
