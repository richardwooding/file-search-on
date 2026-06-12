package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

type CoverageGapsCmd struct {
	Profile   string  `arg:"" help:"Path to a Go coverage profile (go test -coverprofile=cover.out ./...)."`
	Dir       string  `short:"d" name:"dir" default:"." help:"Module root holding go.mod; resolves the profile's import-path filenames to disk."`
	Threshold float64 `name:"threshold" default:"0" help:"Coverage fraction 0..1; report functions strictly below it. 0 (default) = 1.0 — every function not fully covered. 0.8 = under 80%."`
	Output    string  `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *CoverageGapsCmd) Run(ctx context.Context) error {
	res, err := search.CoverageGaps(ctx, c.Profile, c.Dir, c.Threshold, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("coverage-gaps failed: %w", err)
	}
	if c.Output == "json" {
		return writeJSON(os.Stdout, res)
	}
	if len(res.Gaps) == 0 {
		fmt.Printf("No coverage gaps below threshold %.2f (analysed %d file(s), mode %q).\n", res.Threshold, res.FilesAnalysed, res.ProfileMode)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "COVERAGE\tSTMTS\tFUNCTION\tLOCATION")
	for _, g := range res.Gaps {
		_, _ = fmt.Fprintf(tw, "%.0f%%\t%d/%d\t%s\t%s:%d-%d\n",
			g.CoveredPct*100, g.CoveredStatements, g.TotalStatements, g.Function, g.Path, g.StartLine, g.EndLine)
	}
	_ = tw.Flush()
	fmt.Printf("%d function(s) below threshold %.2f across %d analysed file(s) (mode %q).\n", res.Count, res.Threshold, res.FilesAnalysed, res.ProfileMode)
	return nil
}
