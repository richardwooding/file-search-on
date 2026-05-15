package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReadFileInArchiveInput is the JSON-schema input for the
// `read_file_in_archive` tool.
type ReadFileInArchiveInput struct {
	ArchivePath string   `json:"archive_path" jsonschema:"Path to the archive file (.zip / .tar / .tar.gz / .gz). Required."`
	EntryPath   string   `json:"entry_path" jsonschema:"Exact entry path inside the archive (e.g. 'src/main.go'). Must match an entry name exactly — not a glob."`
	MaxBytes    int64    `json:"max_bytes,omitempty" jsonschema:"Cap on returned content bytes. Zero uses the 1 MiB default. Files larger than the cap have content truncated to max_bytes and truncated=true."`
	Timeout     *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout."`
}

// ReadFileInArchiveOutput is the structured output. Content is the
// raw bytes for binary-safe transport; ContentText is the
// UTF-8-decoded view (only populated when valid UTF-8). Both make
// the tool ergonomic for agents that prefer text vs raw bytes.
type ReadFileInArchiveOutput struct {
	ArchivePath    string         `json:"archive_path"`
	Name           string         `json:"name"`
	Size           int64          `json:"size"`
	Content        string         `json:"content,omitempty"`         // UTF-8 text when valid
	ContentBase64  string         `json:"content_base64,omitempty"`  // raw bytes when not valid UTF-8
	Truncated      bool           `json:"truncated,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
	ModTime        string         `json:"mod_time,omitempty"`
	Mode           uint32         `json:"mode,omitempty"`
	ElapsedSeconds float64        `json:"elapsed_seconds,omitempty"`
}

func (h *handlers) readFileInArchiveHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadFileInArchiveInput) (*mcp.CallToolResult, ReadFileInArchiveOutput, error) {
	archivePath, err := expandHomeDir(in.ArchivePath)
	if err != nil {
		return nil, ReadFileInArchiveOutput{}, fmt.Errorf("expand archive_path: %w", err)
	}
	if archivePath == "" {
		return nil, ReadFileInArchiveOutput{}, errors.New("archive_path is required")
	}
	if in.EntryPath == "" {
		return nil, ReadFileInArchiveOutput{}, errors.New("entry_path is required")
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.Timeout)
	defer cancel()

	start := time.Now()
	r, err := search.ReadFileInArchive(ctx, archivePath, in.EntryPath, in.MaxBytes, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil {
		if errors.Is(err, search.ErrArchiveEntryNotFound) {
			return nil, ReadFileInArchiveOutput{}, fmt.Errorf("entry %q not found in archive", in.EntryPath)
		}
		return nil, ReadFileInArchiveOutput{}, fmt.Errorf("read_file_in_archive: %w", err)
	}

	out := ReadFileInArchiveOutput{
		ArchivePath:    r.ArchivePath,
		Name:           r.Name,
		Size:           r.Size,
		Truncated:      r.Truncated,
		ContentType:    r.ContentType,
		Attributes:     r.Attributes,
		Mode:           r.Mode,
		ElapsedSeconds: elapsed,
	}
	if !r.ModTime.IsZero() {
		out.ModTime = r.ModTime.Format(time.RFC3339)
	}
	if utf8.Valid(r.Content) {
		out.Content = string(r.Content)
	} else {
		out.ContentBase64 = base64.StdEncoding.EncodeToString(r.Content)
	}
	return nil, out, nil
}
