package search

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// ArchiveFileResult is the payload of ReadFileInArchive — one entry's
// content plus the metadata + content_type + attributes the regular
// per-entry walker also produces. Truncated fires when the entry's
// uncompressed size exceeds MaxBytes; Content carries the first
// MaxBytes regardless.
type ArchiveFileResult struct {
	ArchivePath string         `json:"archive_path"`
	Name        string         `json:"name"`
	Size        int64          `json:"size"`
	Content     []byte         `json:"content"`
	Truncated   bool           `json:"truncated"`
	ContentType string         `json:"content_type,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	ModTime     time.Time      `json:"mod_time"`
	Mode        uint32         `json:"mode,omitempty"`
}

// ErrArchiveEntryNotFound is returned when entryPath doesn't match
// any entry in the archive. Distinguishable from generic read errors
// so callers can give a useful "not found" message.
var ErrArchiveEntryNotFound = errors.New("entry not found in archive")

// defaultArchiveReadCap is the per-entry byte cap for read tools.
// Matches the per-entry walker's cap; agents that need more can
// override via MaxBytes.
const defaultArchiveReadCap = 1 << 20

// ReadFileInArchive returns the bytes (capped at maxBytes) of a single
// named entry inside archivePath plus the entry's metadata and
// detected attributes. Useful for "pull pyproject.toml out of
// source.tar.gz to check the Python version" agent workflows.
//
// entryPath must exactly match an entry name in the archive (e.g.
// "src/main.go", not just "main.go"). Returns ErrArchiveEntryNotFound
// when no such entry exists.
func ReadFileInArchive(ctx context.Context, archivePath, entryPath string, maxBytes int64, registry *content.Registry) (*ArchiveFileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultArchiveReadCap
	}

	dir, base := filepath.Split(archivePath)
	if dir == "" {
		dir = "."
	}
	fsys := os.DirFS(dir)
	ct := registry.Detect(fsys, base)
	if ct == nil {
		return nil, errors.New("ReadFileInArchive: " + archivePath + " is not a recognised archive")
	}
	ctName := ct.Name()
	switch ctName {
	case "archive/zip", "archive/tar", "archive/tar+gzip", "archive/gzip":
	default:
		return nil, errors.New("ReadFileInArchive: content type " + ctName + " is not a supported archive format")
	}

	var result *ArchiveFileResult
	visitor := func(e content.ArchiveEntry) error {
		if e.Name != entryPath {
			return nil
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		// Read maxBytes + 1 so we can tell whether the entry was
		// larger than the cap (Truncated=true) vs exactly fitting.
		buf, _ := io.ReadAll(io.LimitReader(rc, maxBytes+1))
		_ = rc.Close()
		truncated := int64(len(buf)) > maxBytes
		if truncated {
			buf = buf[:maxBytes]
		}

		// Build attributes for this entry via the same single-file FS
		// the walker uses, so content_type detection and per-format
		// Attributes fire consistently.
		entryFS := content.NewSingleFileFS(e.Name, buf, e.ModTime, e.Mode)
		displayPath := archivePath + ArchiveSeparator + e.Name
		var attrMap map[string]any
		var contentType string
		if attrs, aerr := celexpr.BuildAttributesWith(ctx, entryFS, e.Name, displayPath, registry, celexpr.BuildOptions{}); aerr == nil && attrs != nil {
			contentType = attrs.ContentType
			if attrs.Extra != nil {
				attrMap = sanitiseExtraForWire(attrs.Extra)
			}
		}

		result = &ArchiveFileResult{
			ArchivePath: archivePath,
			Name:        e.Name,
			Size:        e.Size,
			Content:     buf,
			Truncated:   truncated,
			ContentType: contentType,
			Attributes:  attrMap,
			ModTime:     e.ModTime,
			Mode:        uint32(e.Mode),
		}
		return content.ErrStopIteration
	}

	if err := content.IterateArchive(ctx, fsys, base, ctName, visitor); err != nil &&
		!errors.Is(err, content.ErrStopIteration) {
		return nil, err
	}
	if result == nil {
		return nil, ErrArchiveEntryNotFound
	}
	return result, nil
}
