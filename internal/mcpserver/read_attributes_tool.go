package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReadAttributesInput is the JSON-schema input for the `read_attributes`
// tool. Path can be absolute or relative to the server's working
// directory; agents should prefer absolute paths.
type ReadAttributesInput struct {
	Path          string   `json:"path" jsonschema:"Filesystem path of a single file to extract attributes from. Absolute paths are preferred; relative paths resolve against the server's working directory."`
	Fields        []string `json:"fields,omitempty" jsonschema:"Project the response to only the listed attribute names — saves tokens when only a few attributes matter. 'path', 'content_type', and 'size' are always included regardless. Empty / omitted returns every populated attribute. Same field-name vocabulary as the search tool's 'fields' input; unknown names error at request validation time."`
	ComputeHashes     bool     `json:"compute_hashes,omitempty" jsonschema:"When true, populate md5 / sha1 / sha256 on the response. All three compute in one io.MultiWriter pass and cache alongside (size, mtime). Off by default — reads the file in full."`
	CheckDisguised    bool     `json:"check_disguised,omitempty" jsonschema:"When true, populate magic_content_type / extension_content_type / is_disguised on the response. is_disguised fires when bytes disagree with the extension. One extra 512-byte file read."`
	WithXattrs        bool     `json:"with_xattrs,omitempty" jsonschema:"When true, populate the xattr family — xattr_keys, xattr_count, is_xattr_rich, is_quarantined, quarantine_agent / source_url / referrer_url / download_date / user_approved, finder_tags, finder_color, has_finder_comment. Darwin-only; non-Darwin builds silently leave these empty. Two extra syscalls per request."`
	HashAllowlistPath string   `json:"hash_allowlist_path,omitempty" jsonschema:"Path to a hash allowlist (newline-separated md5/sha1/sha256 hex; # comments allowed) OR a pre-built bbolt hashset file. Populates is_known_good. Forces compute_hashes on."`
	HashDenylistPath  string   `json:"hash_denylist_path,omitempty" jsonschema:"Path to a hash denylist (same format). Populates is_known_bad. Forces compute_hashes on."`
}

// ReadAttributesOutput wraps a search.Match so it can carry the
// embedded CommonOutput.ServerVersion alongside the existing Match
// fields. Match fields are promoted to the top level by Go's
// struct-embedding JSON serialisation, so the wire shape stays
// backward-compatible — existing clients see `path`, `content_type`,
// etc. at the top level just as before, with the new `server_version`
// alongside.
type ReadAttributesOutput struct {
	CommonOutput
	search.Match
}

func (h *handlers) readAttributesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadAttributesInput) (*mcp.CallToolResult, ReadAttributesOutput, error) {
	if in.Path == "" {
		return nil, ReadAttributesOutput{}, fmt.Errorf("path is required")
	}
	if err := search.ValidateFields(in.Fields); err != nil {
		return nil, ReadAttributesOutput{}, fmt.Errorf("fields: %w", err)
	}
	path, err := expandHomeDir(in.Path)
	if err != nil {
		return nil, ReadAttributesOutput{}, fmt.Errorf("expand path: %w", err)
	}
	if path, err = h.validatePath(path); err != nil {
		return nil, ReadAttributesOutput{}, err
	}
	if in.HashAllowlistPath != "" {
		if p, err := h.validatePath(in.HashAllowlistPath); err != nil {
			return nil, ReadAttributesOutput{}, err
		} else {
			in.HashAllowlistPath = p
		}
	}
	if in.HashDenylistPath != "" {
		if p, err := h.validatePath(in.HashDenylistPath); err != nil {
			return nil, ReadAttributesOutput{}, err
		} else {
			in.HashDenylistPath = p
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, ReadAttributesOutput{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Single-file extraction is bounded but not free (markdown reads
	// the whole file; PDFs / EXIF are header-only). Apply the server
	// default timeout so a pathological file can't wedge the server.
	// No per-call override on this tool — pass nil.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, nil)
	defer cancel()

	var allowlist, denylist hashset.Set
	if in.HashAllowlistPath != "" {
		al, alErr := hashset.Open(in.HashAllowlistPath)
		if alErr != nil {
			return nil, ReadAttributesOutput{}, fmt.Errorf("load hash_allowlist_path: %w", alErr)
		}
		allowlist = al
		defer func() { _ = al.Close() }()
		in.ComputeHashes = true
	}
	if in.HashDenylistPath != "" {
		dl, dlErr := hashset.Open(in.HashDenylistPath)
		if dlErr != nil {
			return nil, ReadAttributesOutput{}, fmt.Errorf("load hash_denylist_path: %w", dlErr)
		}
		denylist = dl
		defer func() { _ = dl.Close() }()
		in.ComputeHashes = true
	}

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:          h.idx,
		ComputeHashes:          in.ComputeHashes,
		CheckDisguised:         in.CheckDisguised,
		ReadExtendedAttributes: in.WithXattrs,
		Allowlist:              allowlist,
		Denylist:               denylist,
	})
	if err != nil {
		return nil, ReadAttributesOutput{}, fmt.Errorf("read attributes: %w", err)
	}
	m := search.MatchFrom(search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	})
	return nil, ReadAttributesOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Match:        search.ProjectMatch(m, in.Fields),
	}, nil
}
