package search

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// ReviewFinding is one issue surfaced by Review on a changed file. Rule names
// the analysis it came from; Level is the gate severity ("warn" | "fail").
type ReviewFinding struct {
	Rule      string `json:"rule"`  // "complexity" | "dead-code"
	Level     string `json:"level"` // "warn" | "fail"
	Message   string `json:"message"`
	Path      string `json:"path"`
	Symbol    string `json:"symbol,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// ReviewConfig tunes the diff-scoped review gate.
type ReviewConfig struct {
	// Base is a git ref. When empty, Review diffs the uncommitted working
	// tree + index against HEAD (`git diff --name-only HEAD`) — the
	// pre-commit case. When set, it diffs `<Base>...HEAD` (three-dot: the
	// changes introduced on HEAD since its merge-base with Base) — the
	// PR-gate case.
	Base string
	// MaxComplexity is the cyclomatic-complexity ceiling for a function in a
	// changed file; functions above it become a "fail" finding. <= 0 uses
	// the default (15).
	MaxComplexity int
	// CheckDeadCode includes dead-code candidates in changed files as "warn"
	// findings. Heuristic, so it never escalates to "fail" on its own.
	CheckDeadCode bool
}

// ReviewResult is the verdict + findings for a diff-scoped review. Verdict is
// "fail" when any finding is fail-level, "warn" when any is warn-level (and
// none fail), else "pass".
type ReviewResult struct {
	Base               string          `json:"base"`
	ChangedFiles       []string        `json:"changed_files"`
	FilesAnalysed      int             `json:"files_analysed"`
	Findings           []ReviewFinding `json:"findings"`
	Verdict            string          `json:"verdict"`
	WarnCount          int             `json:"warn_count"`
	FailCount          int             `json:"fail_count"`
	Cancelled          bool            `json:"cancelled,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
}

const defaultReviewMaxComplexity = 15

// Review resolves the files changed in the git diff (see ReviewConfig.Base),
// runs the per-file analyses scoped to those files, and returns the findings
// plus an overall pass/warn/fail verdict. The walk covers opts.Root /
// opts.Roots (so cross-file analyses like dead-code see the whole graph) and
// findings are then filtered to the changed set.
func Review(ctx context.Context, opts Options, registry *content.Registry, cfg ReviewConfig) (*ReviewResult, error) {
	if cfg.MaxComplexity <= 0 {
		cfg.MaxComplexity = defaultReviewMaxComplexity
	}
	root := reviewRoot(opts)

	changedAbs, changedRel, err := changedFiles(ctx, root, cfg.Base)
	if err != nil {
		// Can't resolve the changed set: treat a cancellation/timeout as a
		// (clean) partial result so the caller's gate maps it to the usual
		// exit code, but surface a genuine failure (e.g. not a git repo).
		if reason := cancelReason(ctx); reason != "" {
			return &ReviewResult{Base: cfg.Base, Verdict: "pass", Cancelled: true, CancellationReason: reason}, nil
		}
		return nil, err
	}
	out := &ReviewResult{Base: cfg.Base, ChangedFiles: changedRel, FilesAnalysed: len(changedRel)}
	if len(changedAbs) == 0 {
		out.Verdict = "pass"
		return out, nil
	}

	if opts.Expr == "" {
		opts.Expr = "is_source"
	}
	// Memoise the symlink-resolving path match: complexity iterates every
	// function in the tree and dead-code every candidate, but each file's
	// path repeats across its symbols, so caching collapses EvalSymlinks
	// (physical disk I/O) to one call per distinct path.
	resolved := map[string]string{}
	inChanged := func(p string) bool {
		c, ok := resolved[p]
		if !ok {
			c = absClean(p)
			resolved[p] = c
		}
		return changedAbs[c]
	}

	// Complexity: every function, then filter to changed files over the gate.
	rep, cErr := Complexity(ctx, opts, registry, 1<<30)
	if cErr != nil && !isReviewCancel(cErr) {
		return nil, fmt.Errorf("complexity analysis: %w", cErr)
	}
	if rep != nil {
		for _, fn := range rep.Functions {
			if !inChanged(fn.Path) || fn.Complexity <= cfg.MaxComplexity {
				continue
			}
			out.Findings = append(out.Findings, ReviewFinding{
				Rule:      "complexity",
				Level:     "fail",
				Message:   fmt.Sprintf("%s has cyclomatic complexity %d (> %d)", fn.Function, fn.Complexity, cfg.MaxComplexity),
				Path:      fn.Path,
				Symbol:    fn.Function,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
			})
		}
	}

	// Dead code: candidates in changed files, as warn-level findings. Skipped
	// once cancellation is in flight — a partial graph yields false positives.
	if cfg.CheckDeadCode && cancelReason(ctx) == "" {
		g, gErr := BuildCodeGraph(ctx, opts, registry)
		if gErr != nil && !isReviewCancel(gErr) {
			return nil, fmt.Errorf("dead-code analysis: %w", gErr)
		}
		if g != nil {
			for _, d := range g.DeadCode() {
				if !inChanged(d.Path) {
					continue
				}
				name := d.Symbol
				if d.Owner != "" {
					name = d.Owner + "." + d.Symbol
				}
				out.Findings = append(out.Findings, ReviewFinding{
					Rule:    "dead-code",
					Level:   "warn",
					Message: fmt.Sprintf("%s %q is never referenced (candidate dead code)", d.Kind, name),
					Path:    d.Path,
					Symbol:  name,
				})
			}
		}
	}

	// Surface a cancellation that landed during either analysis phase, so the
	// caller knows the verdict is over a partial set.
	if reason := cancelReason(ctx); reason != "" {
		out.Cancelled = true
		out.CancellationReason = reason
	} else if rep != nil && rep.Cancelled {
		out.Cancelled = true
		out.CancellationReason = rep.CancellationReason
	}

	finalizeReview(out)
	return out, nil
}

// cancelReason maps a cancelled context to the reason vocabulary the CLI gate
// expects ("timeout" → exit 124, otherwise "interrupted" → 130), or "" when
// the context is still live.
func cancelReason(ctx context.Context) string {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return "timeout"
	case context.Canceled:
		return "interrupted"
	default:
		return ""
	}
}

// finalizeReview sorts findings deterministically and computes the verdict.
func finalizeReview(out *ReviewResult) {
	for _, f := range out.Findings {
		switch f.Level {
		case "fail":
			out.FailCount++
		case "warn":
			out.WarnCount++
		}
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		a, b := out.Findings[i], out.Findings[j]
		// fail before warn, then path, then line, then symbol.
		if a.Level != b.Level {
			return a.Level == "fail"
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.Symbol < b.Symbol
	})
	switch {
	case out.FailCount > 0:
		out.Verdict = "fail"
	case out.WarnCount > 0:
		out.Verdict = "warn"
	default:
		out.Verdict = "pass"
	}
}

// reviewRoot returns the directory Review treats as the git working dir and
// the walk root: the first of opts.Roots, else opts.Root, else ".".
func reviewRoot(opts Options) string {
	if len(opts.Roots) > 0 && opts.Roots[0] != "" {
		return opts.Roots[0]
	}
	if opts.Root != "" {
		return opts.Root
	}
	return "."
}

// absClean returns an absolute, symlink-resolved form of p for set-membership
// matching between git-diff output and walk results, which may carry either
// relative or absolute paths depending on the root the caller passed. Symlinks
// are resolved because `git rev-parse --show-toplevel` reports the real path
// (e.g. /private/var/... on macOS) while filepath.Abs does not — without this
// the two sides never match. EvalSymlinks needs the path to exist; both the
// changed files and the walked files do, so it succeeds, falling back to the
// unresolved absolute path otherwise.
func absClean(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return resolved
	}
	return abs
}

// changedFiles runs git in dir to list the files changed relative to base (see
// ReviewConfig.Base for the semantics). It returns a set of absolute cleaned
// paths (for matching) and the sorted repo-relative paths (for reporting).
// Deleted paths are dropped — they can't carry a current finding.
func changedFiles(ctx context.Context, dir, base string) (map[string]bool, []string, error) {
	top, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil, fmt.Errorf("not a git repository (or git unavailable) at %q: %w", dir, err)
	}
	toplevel := strings.TrimSpace(top)

	args := []string{"diff", "--name-only", "--diff-filter=d"}
	if base == "" {
		args = append(args, "HEAD")
	} else {
		args = append(args, base+"...HEAD")
	}
	// Pathspec "-- ." (with cmd.Dir == dir) scopes the diff to the reviewed
	// subtree; without it git reports changes across the whole repo even when
	// only a subdirectory is being reviewed, inflating the changed set.
	args = append(args, "--", ".")
	rawList, err := gitOutput(ctx, dir, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("git diff failed: %w", err)
	}

	abs := map[string]bool{}
	var rel []string
	sc := bufio.NewScanner(bytes.NewBufferString(rawList))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		rel = append(rel, line)
		abs[absClean(filepath.Join(toplevel, filepath.FromSlash(line)))] = true
	}
	sort.Strings(rel)
	return abs, rel, nil
}

// gitOutput runs `git <args...>` in dir and returns stdout. The context bounds
// the run so a hung git can't outlive a cancelled review.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// isReviewCancel reports whether err is a context cancellation/deadline, which
// Review treats as a partial result rather than a hard failure.
func isReviewCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
