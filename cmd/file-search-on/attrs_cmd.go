package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

type AttrsCmd struct {
	Path   string `arg:"" help:"File to inspect."`
	Output string `short:"o" name:"output" enum:"default,verbose,json" default:"verbose" help:"Output format: default | verbose | json."`
	Format string `name:"format" help:"Custom Go text/template applied to the record (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`

	IndexPath      string `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/<basename>-<sha1[:6]>.db. Repeated attrs calls on the same file are returned from the cache after the first parse."`
	NoIndex        bool   `name:"no-index" help:"Disable the on-disk index entirely; re-parse the file from scratch every call. Useful for hermetic CI or when another file-search-on instance holds the writer lock."`
	WithHashes     bool   `name:"with-hashes" help:"Compute md5 / sha1 / sha256 of the file in a single io.MultiWriter pass and surface them as attributes. Hashes cache in the index alongside (size, mtime), so subsequent runs are free on unchanged files. Off by default — hashing reads the file in full."`
	CheckDisguised bool   `name:"check-disguised" help:"Run both the name-based and magic-byte detection passes, populating magic_content_type / extension_content_type / is_disguised. is_disguised fires when the bytes disagree with the extension. One extra 512-byte file read (cached)."`
	WithXattrs     bool   `name:"with-xattrs" help:"Read macOS extended attributes and surface them as CEL variables: xattr_keys, xattr_count, is_xattr_rich, is_quarantined, quarantine_agent / event_id / source_url / referrer_url / download_date / user_approved, finder_tags, finder_color, has_finder_comment. Darwin-only; non-Darwin builds silently leave these empty."`
	HashAllowlist  string `name:"hash-allowlist" help:"Path to a hash allowlist (newline-separated md5/sha1/sha256 hex, mixed algorithms auto-detected by length, # comments allowed) OR a pre-built bbolt hashset file. Populates is_known_good. Forces --with-hashes on. NSRL / corp-allowlist / threat-intel-allowlist interop."`
	HashDenylist   string `name:"hash-denylist" help:"Path to a hash denylist (same format as --hash-allowlist). Populates is_known_bad. Threat-intel-feed / IOC-list interop."`
}

func (a *AttrsCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(a.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; use the search subcommand to walk a tree", abs)
	}

	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	idx, _, err := openIndex(a.IndexPath, a.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	// Hash allow/denylists mirror the read_attributes MCP tool — both
	// force --with-hashes on so the resulting is_known_good /
	// is_known_bad predicates have something to test against.
	computeHashes := a.WithHashes
	var allowlist, denylist hashset.Set
	if a.HashAllowlist != "" {
		al, alErr := hashset.Open(a.HashAllowlist)
		if alErr != nil {
			return fmt.Errorf("load --hash-allowlist: %w", alErr)
		}
		allowlist = al
		defer func() { _ = al.Close() }()
		computeHashes = true
	}
	if a.HashDenylist != "" {
		dl, dlErr := hashset.Open(a.HashDenylist)
		if dlErr != nil {
			return fmt.Errorf("load --hash-denylist: %w", dlErr)
		}
		denylist = dl
		defer func() { _ = dl.Close() }()
		computeHashes = true
	}

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, abs, contentpkg.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		ComputeHashes:          computeHashes,
		CheckDisguised:         a.CheckDisguised,
		ReadExtendedAttributes: a.WithXattrs,
		Allowlist:              allowlist,
		Denylist:               denylist,
	})
	if err != nil {
		return fmt.Errorf("read attributes: %w", err)
	}

	result := search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	}
	results := []search.Result{result}

	if a.Format != "" {
		tmpl, err := parseFormatTemplate(a.Format)
		if err != nil {
			return fmt.Errorf("parse --format template: %w", err)
		}
		return printTemplate(os.Stdout, results, tmpl)
	}
	switch a.Output {
	case "json":
		return printJSON(os.Stdout, results)
	case "default":
		printDefault(os.Stdout, results)
	default: // "" or "verbose"
		printVerbose(os.Stdout, results)
	}
	return nil
}
