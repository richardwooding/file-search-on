package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/sarif"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReviewCmd is the diff-scoped review gate (issue #484): resolve the files
// changed in the git diff, run the per-file analyses scoped to them, and emit
// findings + a pass/warn/fail verdict whose exit code gates CI / pre-commit.
type ReviewCmd struct {
	Dir  string `short:"d" default:"." help:"Repository directory to review. The git working dir and the walk root."`
	Base string `name:"base" help:"Git ref to diff against. Empty (default) reviews uncommitted changes vs HEAD (pre-commit). A ref (e.g. origin/main) reviews <base>...HEAD — the changes introduced on HEAD since its merge-base with <base> (PR gate)."`
	Expr string `name:"expr" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`

	MaxComplexity int  `name:"max-complexity" default:"15" help:"Cyclomatic-complexity ceiling for a function in a changed file; functions above it are a fail-level finding."`
	NoDeadCode    bool `name:"no-dead-code" help:"Skip the dead-code check (it adds a second graph pass)."`
	Strict        bool `name:"strict" help:"Treat warn-level findings as failures for exit-code purposes (warn verdict also exits non-zero)."`

	Workers             int           `short:"w" help:"Parallel workers. 0 = runtime.NumCPU()." default:"0"`
	IndexPath           string        `name:"index-path" help:"Persistent attribute index file (bbolt)."`
	NoIndex             bool          `name:"no-index" help:"Disable the on-disk index; in-memory cache only."`
	Timeout             time.Duration `name:"timeout" help:"Maximum duration. On expiry the partial verdict is printed and the process exits 124."`
	Exclude             []string      `name:"exclude" help:"Glob matched against basenames; matches are pruned. Repeatable."`
	RespectGitignore    bool          `name:"respect-gitignore" help:"Parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks      bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
	PruneBuildArtefacts bool          `name:"prune-build-artefacts" help:"Prune canonical build-artefact dirs (vendor / node_modules / target / …)."`

	Output string `short:"o" name:"output" enum:"table,json,sarif" default:"table" help:"Output format: table | json | sarif (SARIF 2.1.0 for GitHub Code Scanning)."`
}

func (c *ReviewCmd) Run(ctx context.Context) error {
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

	res, err := search.Review(effectiveCtx, search.Options{
		Root:                c.Dir,
		Expr:                c.Expr,
		Workers:             c.Workers,
		Index:               idx,
		Excludes:            c.Exclude,
		RespectGitignore:    c.RespectGitignore,
		FollowSymlinks:      c.FollowSymlinks,
		PruneBuildArtefacts: c.PruneBuildArtefacts,
	}, contentpkg.DefaultRegistry(), search.ReviewConfig{
		Base:          c.Base,
		MaxComplexity: c.MaxComplexity,
		CheckDeadCode: !c.NoDeadCode,
	})
	if err != nil {
		return fmt.Errorf("review failed: %w", err)
	}

	switch c.Output {
	case "json":
		if jerr := writeJSON(os.Stdout, res); jerr != nil {
			return jerr
		}
	case "sarif":
		results := make([]sarif.Result, 0, len(res.Findings))
		for _, f := range res.Findings {
			level := "warning"
			if f.Level == "fail" {
				level = "error"
			}
			results = append(results, sarif.Result{
				RuleID:    "review-" + f.Rule,
				Level:     level,
				Message:   f.Message,
				URI:       f.Path,
				StartLine: f.StartLine,
				EndLine:   f.EndLine,
			})
		}
		if werr := writeSARIF(sarif.Rule{ID: "review", Name: "Review", Description: "Diff-scoped review findings"}, results); werr != nil {
			return werr
		}
	default:
		printReviewTable(os.Stdout, res)
	}

	if res.Cancelled {
		fmt.Fprintln(os.Stderr, "review interrupted; verdict above may be incomplete")
		if res.CancellationReason == "timeout" {
			return &exitCodeError{code: 124, msg: "timeout"}
		}
		return &exitCodeError{code: 130, msg: "interrupted"}
	}

	// Gate: fail always exits non-zero; warn exits non-zero only under --strict.
	if res.Verdict == "fail" || (c.Strict && res.Verdict == "warn") {
		return &exitCodeError{code: 1, msg: res.Verdict}
	}
	return nil
}

func printReviewTable(w *os.File, res *search.ReviewResult) {
	if len(res.ChangedFiles) == 0 {
		fmt.Fprintln(w, "PASS — no changed files in the diff.")
		return
	}
	if len(res.Findings) > 0 {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "LEVEL\tRULE\tLOCATION\tFINDING")
		for _, f := range res.Findings {
			loc := f.Path
			if f.StartLine > 0 {
				loc = fmt.Sprintf("%s:%d", f.Path, f.StartLine)
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Level, f.Rule, loc, f.Message)
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s — %d finding(s) (%d fail, %d warn) across %d changed file(s).\n",
		verdictBanner(res.Verdict), len(res.Findings), res.FailCount, res.WarnCount, len(res.ChangedFiles))
}

func verdictBanner(verdict string) string {
	switch verdict {
	case "fail":
		return "FAIL"
	case "warn":
		return "WARN"
	default:
		return "PASS"
	}
}
