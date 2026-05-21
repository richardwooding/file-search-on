package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/richardwooding/file-search-on/internal/search"
)

type LinesCmd struct {
	Path     string `arg:"" help:"File to read lines from."`
	Start    int    `short:"s" name:"start" help:"First line to print (1-indexed, inclusive)." default:"1"`
	End      int    `short:"e" name:"end" help:"Last line to print (1-indexed, inclusive). 0 = end of file." default:"0"`
	MaxLines int    `name:"max-lines" help:"Cap on lines returned. 0 uses the 1000-line default." default:"0"`
	Output   string `short:"o" name:"output" enum:"text,json" default:"text" help:"Output format: text (default; raw lines) | json (machine-readable with start/end/total/truncated)."`
}

func (l *LinesCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(l.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", abs)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	res, err := search.ReadLines(ctx, os.DirFS(dir), base, l.Start, l.End, l.MaxLines)
	if err != nil {
		return fmt.Errorf("read lines: %w", err)
	}

	if l.Output == "json" {
		return printLinesJSON(os.Stdout, abs, res)
	}
	for _, line := range res.Lines {
		_, _ = fmt.Fprintln(os.Stdout, line)
	}
	if res.Truncated {
		fmt.Fprintf(os.Stderr, "(truncated at %d lines; total lines in file: %d)\n", len(res.Lines), res.TotalLines)
	}
	return nil
}
