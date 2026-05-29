package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// PresetCmd runs a curated search recipe by name. Without an arg
// it lists every preset (canonical name + one-line description). The
// preset's PresetOptions are translated into a search.Options; CLI
// flags below allow per-call overrides without forking the preset
// catalogue. Issue #168 sub-feature B.
type PresetCmd struct {
	Name string `arg:"" optional:"" help:"Preset to run. Omit to list all available presets and exit."`

	Dir            []string `short:"d" help:"Directory to walk. Repeatable for multi-root walks. Defaults to '.'." default:"."`
	Workers        int      `short:"w" help:"Parallel workers. 0 = runtime.NumCPU()." default:"0"`
	Limit          int      `name:"limit" help:"Override the preset's default limit. 0 means use the preset's value (which itself may be 0 = unlimited)."`
	Output         string   `short:"o" name:"output" enum:"default,bare,verbose,json" default:"default" help:"Output format: default (path + content_type + size), bare (paths only), verbose (multi-line), json (NDJSON)."`
	Excludes       []string `name:"exclude" help:"Glob patterns pruned from the walk (e.g. node_modules, .git). Repeatable."`
	RespectGit     bool     `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks bool     `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
	IndexPath      string   `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/. Created on first use; speeds up repeat runs on unchanged trees."`
	NoIndex        bool     `name:"no-index" help:"Disable the on-disk index entirely; use only in-memory caching for the process lifetime."`
}

func (p *PresetCmd) Run(ctx context.Context) error {
	if p.Name == "" {
		return p.listPresets(os.Stdout)
	}

	preset := search.PresetByName(p.Name)
	if preset == nil {
		fmt.Fprintf(os.Stderr, "unknown preset: %q\n\nAvailable presets:\n", p.Name)
		_ = p.listPresets(os.Stderr)
		return &exitCodeError{code: 2, msg: "unknown preset"}
	}

	opts := preset.Build()

	// Translate PresetOptions + CLI overrides into a search.Options.
	walkOpts := search.Options{
		Roots:             p.Dir,
		Expr:              opts.Expr,
		Workers:           p.Workers,
		Sort:              opts.Sort,
		Order:             opts.Order,
		RankExpr:          opts.RankExpr,
		Limit:             opts.Limit,
		IncludeAttributes: true,
		IncludeBody:       opts.IncludeBody,
		ComputeHashes:     opts.ComputeHashes,
		CheckDisguised:    opts.CheckDisguised,
		Excludes:          p.Excludes,
		RespectGitignore:  p.RespectGit,
		FollowSymlinks:    p.FollowSymlinks,
	}

	if p.Limit != 0 {
		walkOpts.Limit = p.Limit
	}

	idx, _, err := openIndex(p.IndexPath, p.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()
	walkOpts.Index = idx

	results, err := search.Walk(ctx, walkOpts, contentpkg.DefaultRegistry())
	if err != nil && !isCancellation(err) {
		return fmt.Errorf("preset %s: %w", p.Name, err)
	}

	switch p.Output {
	case "json":
		if jerr := printJSON(os.Stdout, results); jerr != nil {
			return jerr
		}
	case "bare":
		printBare(os.Stdout, results)
	case "verbose":
		printVerbose(os.Stdout, results)
	default:
		printDefault(os.Stdout, results)
	}

	if isCancellation(err) {
		if errors.Is(err, context.Canceled) {
			return &exitCodeError{code: 130, msg: "interrupted"}
		}
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

// listPresets prints the canonical name + description table.
func (p *PresetCmd) listPresets(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	for _, pp := range search.Presets() {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", pp.Name, pp.Description)
	}
	return tw.Flush()
}
