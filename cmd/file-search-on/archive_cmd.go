package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

type ArchiveContentsCmd struct {
	Archive           string        `arg:"" help:"Path to the archive file (.zip / .tar / .tar.gz / .gz)."`
	Expr              string        `name:"expr" short:"e" help:"Optional CEL expression to filter entries (e.g. 'is_source && language == \"go\"'). Empty matches every entry."`
	Glob              string        `name:"glob" help:"Optional filepath.Match basename pattern applied BEFORE the CEL filter as a cheap pre-prune (e.g. '*.go')."`
	IncludeAttributes bool          `name:"include-attributes" help:"Include the full per-entry attribute map in the output. Off by default for terse listings."`
	Body              bool          `name:"body" help:"Read entry bodies so body.contains() / body.matches() CEL filters fire. Capped at --entry-read-cap. Bypasses the entry-list cache (bodies aren't cached)."`
	EntryReadCap      int64         `name:"entry-read-cap" default:"0" help:"Cap on per-entry bytes read into memory for detection and body evaluation (bytes). 0 uses the 8 MiB default — enough for typical PDF / DOCX / EPUB / email bodies inside archives. Raise for archives containing huge documents; lower if memory pressure matters on large collections."`
	MaxEntries        int           `name:"max" default:"0" help:"Cap on entries returned. 0 = unlimited."`
	IndexPath         string        `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/. The per-archive entry-list cache is consulted before each walk and populated on miss."`
	NoIndex           bool          `name:"no-index" help:"Disable the on-disk index entirely; use only in-memory caching for the process lifetime."`
	Timeout           time.Duration `name:"timeout" help:"Maximum walk duration. On expiry the partial set is still printed and the process exits 124."`
	Output            string        `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (c *ArchiveContentsCmd) Run(ctx context.Context) error {
	parentCtx := ctx
	effectiveCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	idx, _, err := openIndex(c.IndexPath, c.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	result, err := search.WalkArchiveEntries(effectiveCtx, c.Archive, search.ArchiveWalkOptions{
		Expr:              c.Expr,
		Glob:              c.Glob,
		IncludeAttributes: c.IncludeAttributes,
		IncludeBody:       c.Body,
		EntryReadCap:      c.EntryReadCap,
		MaxEntries:        c.MaxEntries,
		Index:             idx,
	}, contentpkg.DefaultRegistry())

	if result != nil {
		if c.Output == "json" {
			if err := printArchiveContentsJSON(os.Stdout, result); err != nil {
				return err
			}
		} else {
			printArchiveContentsTable(os.Stdout, result)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("archive-contents failed: %w", err)
	}
	if result != nil && result.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "archive-contents interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case c.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "archive-contents timed out after %s; results above may be incomplete\n", c.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

type ArchiveReadCmd struct {
	Archive  string        `arg:"" help:"Path to the archive file (.zip / .tar / .tar.gz / .gz)."`
	Entry    string        `arg:"" help:"Exact entry path inside the archive (e.g. 'src/main.go')."`
	MaxBytes int64         `name:"max-bytes" default:"0" help:"Cap on bytes returned. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix is still returned."`
	Output   string        `short:"o" name:"output" enum:"raw,json" default:"raw" help:"Output format: raw (entry content to stdout) | json (envelope with metadata + content)."`
	Timeout  time.Duration `name:"timeout" help:"Maximum duration. On expiry the process exits 124."`
}

func (c *ArchiveReadCmd) Run(ctx context.Context) error {
	effectiveCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	r, err := search.ReadFileInArchive(effectiveCtx, c.Archive, c.Entry, c.MaxBytes, contentpkg.DefaultRegistry())
	if err != nil {
		if errors.Is(err, search.ErrArchiveEntryNotFound) {
			return &exitCodeError{code: 1, msg: fmt.Sprintf("entry %q not found in archive %q", c.Entry, c.Archive)}
		}
		return fmt.Errorf("archive-read failed: %w", err)
	}

	if c.Output == "json" {
		return printArchiveReadJSON(os.Stdout, r)
	}
	_, werr := os.Stdout.Write(r.Content)
	if werr != nil {
		return werr
	}
	if r.Truncated {
		fmt.Fprintf(os.Stderr, "(truncated at %d bytes; entry is %d bytes total)\n", len(r.Content), r.Size)
	}
	return nil
}
