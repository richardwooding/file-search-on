package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/richardwooding/file-search-on/internal/hashset"
)

// HashSetCmd is the `hash-set` subcommand surface — a small group
// of utilities around the bbolt-backed hashset format used by
// --hash-allowlist / --hash-denylist (PR #146).
type HashSetCmd struct {
	Build HashSetBuildCmd `cmd:"" help:"Compile a text or NSRL-CSV hash list into a bbolt hashset file. Pre-building saves the load cost on every search invocation and makes NSRL-scale allowlists (~50M hashes) practical."`
	Info  HashSetInfoCmd  `cmd:"" help:"Print per-algorithm entry counts for a hashset file (text or bbolt). Sanity-check that a list loaded correctly before pointing search at it."`
}

// HashSetBuildCmd compiles a text or NSRL-CSV hash list into the
// bbolt-format hashset file used by --hash-allowlist / --hash-denylist.
type HashSetBuildCmd struct {
	From   string `arg:"" name:"input" help:"Path to the input file (text or NSRL CSV). Use '-' to read stdin."`
	Out    string `name:"out" short:"o" required:"" help:"Output bbolt hashset file. Existing files are overwritten."`
	Format string `name:"format" enum:"auto,text,nsrl" default:"auto" help:"Input format: 'auto' (default — sniffs for the NSRL header), 'text' (newline-separated hex; mixed algorithms auto-detected), or 'nsrl' (NSRLFile.txt CSV with quoted columns)."`
	Quiet  bool   `name:"quiet" short:"q" help:"Suppress progress reporting (per-50k-row counters to stderr)."`
}

func (c *HashSetBuildCmd) Run(_ context.Context) error {
	var r io.Reader
	if c.From == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(c.From)
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}

	start := time.Now()
	var progress func(int64)
	if !c.Quiet {
		var lastReport time.Time
		progress = func(total int64) {
			if time.Since(lastReport) < time.Second {
				return
			}
			fmt.Fprintf(os.Stderr, "\rbuilt %d entries (%s)", total, time.Since(start).Truncate(time.Second))
			lastReport = time.Now()
		}
	}

	if err := hashset.Build(r, c.Out, hashset.BuildOpts{
		Format:   c.Format,
		Progress: progress,
	}); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// Reopen to report final counts.
	set, err := hashset.OpenBolt(c.Out)
	if err != nil {
		return fmt.Errorf("verify built hashset: %w", err)
	}
	defer func() { _ = set.Close() }()
	if !c.Quiet {
		fmt.Fprintf(os.Stderr, "\n")
	}
	counts := set.Counts()
	fmt.Printf("built %s in %s\n", c.Out, time.Since(start).Truncate(time.Millisecond))
	fmt.Printf("  md5:    %d\n", counts["md5"])
	fmt.Printf("  sha1:   %d\n", counts["sha1"])
	fmt.Printf("  sha256: %d\n", counts["sha256"])
	return nil
}

// HashSetInfoCmd prints per-algorithm counts for a hashset (text or bbolt).
type HashSetInfoCmd struct {
	Path string `arg:"" name:"path" help:"Path to a hashset file (text or bbolt)."`
}

func (c *HashSetInfoCmd) Run(_ context.Context) error {
	set, err := hashset.Open(c.Path)
	if err != nil {
		return fmt.Errorf("open hashset: %w", err)
	}
	defer func() { _ = set.Close() }()
	counts := set.Counts()
	fmt.Printf("%s\n", c.Path)
	fmt.Printf("  md5:    %d\n", counts["md5"])
	fmt.Printf("  sha1:   %d\n", counts["sha1"])
	fmt.Printf("  sha256: %d\n", counts["sha256"])
	total := counts["md5"] + counts["sha1"] + counts["sha256"]
	fmt.Printf("  total:  %d\n", total)
	return nil
}
