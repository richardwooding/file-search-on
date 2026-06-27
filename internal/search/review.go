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
	"strconv"
	"strings"
	"time"

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
	// MaxCognitive is the cognitive-complexity ceiling (SonarSource,
	// nesting-weighted) for a function in a changed file; functions above it
	// become a "fail" finding. <= 0 uses the default (15). Only applies where
	// cognitive complexity is available (Go + most tree-sitter languages); a
	// function whose language doesn't compute it is never flagged on this gate.
	MaxCognitive int
	// CheckDeadCode includes dead-code candidates in changed files as "warn"
	// findings. Heuristic, so it never escalates to "fail" on its own.
	CheckDeadCode bool
	// BaselineOnly restricts the complexity / cognitive-complexity gate to
	// findings that are NEW or WORSENED relative to the base ref — a function
	// whose metric is unchanged (or lower) than its baseline value is not
	// flagged, even if it's over the ceiling. This stops a PR that merely
	// touches a file with pre-existing debt from being blocked on code it
	// didn't change (issue #538). The baseline is the same content the diff is
	// taken against: HEAD when Base is empty, else the merge-base of Base and
	// HEAD. Off by default (every over-ceiling function in a changed file is
	// flagged). Dead-code (warn-level) is unaffected.
	BaselineOnly bool
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

const (
	defaultReviewMaxComplexity = 15
	defaultReviewMaxCognitive  = 15
)

// Review resolves the files changed in the git diff (see ReviewConfig.Base),
// runs the per-file analyses scoped to those files, and returns the findings
// plus an overall pass/warn/fail verdict. The walk covers opts.Root /
// opts.Roots (so cross-file analyses like dead-code see the whole graph) and
// findings are then filtered to the changed set.
func Review(ctx context.Context, opts Options, registry *content.Registry, cfg ReviewConfig) (*ReviewResult, error) {
	if cfg.MaxComplexity <= 0 {
		cfg.MaxComplexity = defaultReviewMaxComplexity
	}
	if cfg.MaxCognitive <= 0 {
		cfg.MaxCognitive = defaultReviewMaxCognitive
	}
	root := reviewRoot(opts)

	changedAbs, changedRel, toplevel, err := changedFiles(ctx, root, cfg.Base)
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
	canon := func(p string) string {
		c, ok := resolved[p]
		if !ok {
			c = absClean(p)
			resolved[p] = c
		}
		return c
	}
	inChanged := func(p string) bool { return changedAbs[canon(p)] }
	suppressed := baselineSuppressor(ctx, root, toplevel, cfg, changedRel, registry, canon)

	// Complexity: every function, then filter to changed files over the gate.
	rep, cErr := Complexity(ctx, opts, registry, 1<<30)
	if cErr != nil && !isReviewCancel(cErr) {
		return nil, fmt.Errorf("complexity analysis: %w", cErr)
	}
	appendComplexityFindings(out, rep, cfg, inChanged, suppressed)

	if err := runDeadCode(ctx, opts, registry, cfg, out, inChanged); err != nil {
		return nil, err
	}

	surfaceCancellation(out, ctx, rep)
	finalizeReview(out)
	return out, nil
}

// runDeadCode appends warn-level dead-code findings for changed files, unless
// cancellation is already in flight (a partial graph yields false positives).
// A genuine (non-cancellation) graph error is returned.
func runDeadCode(ctx context.Context, opts Options, registry *content.Registry, cfg ReviewConfig, out *ReviewResult, inChanged func(string) bool) error {
	if !cfg.CheckDeadCode || cancelReason(ctx) != "" {
		return nil
	}
	g, gErr := BuildCodeGraph(ctx, opts, registry)
	if gErr != nil && !isReviewCancel(gErr) {
		return fmt.Errorf("dead-code analysis: %w", gErr)
	}
	appendDeadCodeFindings(out, g, inChanged)
	return nil
}

// surfaceCancellation marks the result as partial when the context was
// cancelled, or when the complexity pass reported its own cancellation.
func surfaceCancellation(out *ReviewResult, ctx context.Context, rep *ComplexityReport) {
	if reason := cancelReason(ctx); reason != "" {
		out.Cancelled = true
		out.CancellationReason = reason
	} else if rep != nil && rep.Cancelled {
		out.Cancelled = true
		out.CancellationReason = rep.CancellationReason
	}
}

// baselineSuppressor returns the predicate appendComplexityFindings uses to
// drop pre-existing-and-not-worsened complexity findings in baseline mode
// (#538). When cfg.BaselineOnly is off it returns a predicate that suppresses
// nothing. The baseline is built once here (keyed by absClean path via canon);
// an unresolvable base ref degrades safely to flagging everything.
func baselineSuppressor(ctx context.Context, root, toplevel string, cfg ReviewConfig, changedRel []string, registry *content.Registry, canon func(string) string) func(path, symbol string, value int, cognitive bool) bool {
	if !cfg.BaselineOnly {
		return func(string, string, int, bool) bool { return false }
	}
	var baseline map[string]map[string]metricPair
	if baseRef, err := reviewBaseRef(ctx, root, cfg.Base); err == nil {
		baseline = baselineComplexity(ctx, root, toplevel, baseRef, changedRel, registry)
	}
	return func(path, symbol string, value int, cognitive bool) bool {
		m := baseline[canon(path)]
		if m == nil {
			return false // file absent at base (newly added) → finding is new
		}
		prev, ok := m[symbol]
		if !ok {
			return false // symbol new at base → finding is new
		}
		base := prev.cyclomatic
		if cognitive {
			base = prev.cognitive
		}
		if base < 0 {
			return false // baseline metric unavailable → don't suppress
		}
		return value <= base // unchanged or improved → suppress
	}
}

// appendComplexityFindings adds fail-level cyclomatic + cognitive findings for
// every changed-file function over the configured ceilings, skipping any the
// baseline suppressor marks as pre-existing-and-not-worsened (#538). Cognitive
// is only checked where the language computes it (a distinct signal from
// cyclomatic, so a function may trip both).
func appendComplexityFindings(out *ReviewResult, rep *ComplexityReport, cfg ReviewConfig, inChanged func(string) bool, suppressed func(path, symbol string, value int, cognitive bool) bool) {
	if rep == nil {
		return
	}
	for _, fn := range rep.Functions {
		if !inChanged(fn.Path) {
			continue
		}
		if fn.Complexity > cfg.MaxComplexity && !suppressed(fn.Path, fn.Function, fn.Complexity, false) {
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
		if fn.CognitiveComplexity != nil && *fn.CognitiveComplexity > cfg.MaxCognitive &&
			!suppressed(fn.Path, fn.Function, *fn.CognitiveComplexity, true) {
			out.Findings = append(out.Findings, ReviewFinding{
				Rule:      "cognitive-complexity",
				Level:     "fail",
				Message:   fmt.Sprintf("%s has cognitive complexity %d (> %d)", fn.Function, *fn.CognitiveComplexity, cfg.MaxCognitive),
				Path:      fn.Path,
				Symbol:    fn.Function,
				StartLine: fn.StartLine,
				EndLine:   fn.EndLine,
			})
		}
	}
}

// appendDeadCodeFindings adds warn-level dead-code candidates in changed files.
func appendDeadCodeFindings(out *ReviewResult, g *CodeGraph, inChanged func(string) bool) {
	if g == nil {
		return
	}
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
func changedFiles(ctx context.Context, dir, base string) (map[string]bool, []string, string, error) {
	top, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil, "", fmt.Errorf("not a git repository (or git unavailable) at %q: %w", dir, err)
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
		return nil, nil, "", fmt.Errorf("git diff failed: %w", err)
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
	return abs, rel, toplevel, nil
}

// metricPair is a function's cyclomatic + cognitive complexity at the baseline.
// cognitive is -1 when the language doesn't compute it.
type metricPair struct {
	cyclomatic int
	cognitive  int
}

// reviewBaseRef resolves the git ref whose content is the baseline for the
// diff: HEAD when base is empty (uncommitted vs HEAD), else the merge-base of
// base and HEAD (matching the `base...HEAD` three-dot diff the gate uses).
func reviewBaseRef(ctx context.Context, dir, base string) (string, error) {
	if base == "" {
		return "HEAD", nil
	}
	mb, err := gitOutput(ctx, dir, "merge-base", base, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(mb), nil
}

// baselineComplexity returns, for each changed file, the per-symbol complexity
// at baseRef, keyed by absClean(toplevel/rel) to match the changed-set keys. It
// reuses the production extractor on the base content (via a single-file fs.FS),
// so baseline metrics are computed identically to HEAD. Files absent at baseRef
// (newly added) get no entry — their findings then count as new.
func baselineComplexity(ctx context.Context, dir, toplevel, baseRef string, changedRel []string, registry *content.Registry) map[string]map[string]metricPair {
	out := make(map[string]map[string]metricPair, len(changedRel))
	for _, rel := range changedRel {
		if m := baselineFileComplexity(ctx, dir, baseRef, rel, registry); m != nil {
			out[absClean(filepath.Join(toplevel, filepath.FromSlash(rel)))] = m
		}
	}
	return out
}

// baselineFileComplexity returns the per-symbol complexity of one file at
// baseRef, or nil when the file is absent at baseRef or carries no source
// symbols. It runs the production extractor on the base content via a
// single-file fs.FS so metrics match HEAD's exactly.
func baselineFileComplexity(ctx context.Context, dir, baseRef, rel string, registry *content.Registry) map[string]metricPair {
	data, err := gitShowFile(ctx, dir, baseRef, rel)
	if err != nil {
		return nil // not present at baseRef → no baseline (treated as new)
	}
	name := filepath.Base(rel)
	fsys := content.NewSingleFileFS(name, data, time.Time{}, 0)
	ct := registry.Detect(fsys, name)
	if ct == nil {
		return nil
	}
	attrs, aerr := ct.Attributes(ctx, fsys, name)
	if aerr != nil || attrs == nil {
		return nil
	}
	rows, _ := attrs["complexity_rows"].([]string)
	if len(rows) == 0 {
		return nil
	}
	m := make(map[string]metricPair, len(rows))
	for _, r := range rows {
		if sym, mp, ok := parseComplexityRow(r); ok {
			mergeMaxMetric(m, sym, mp)
		}
	}
	return m
}

// mergeMaxMetric records mp for sym, keeping the per-metric maximum when sym is
// already present. Same-named functions in one file thus collapse to the most
// lenient baseline, so a finding is only "worsened" if it exceeds every prior
// namesake — avoiding false positives on overloads / same-named methods.
func mergeMaxMetric(m map[string]metricPair, sym string, mp metricPair) {
	if prev, exists := m[sym]; exists {
		if prev.cyclomatic > mp.cyclomatic {
			mp.cyclomatic = prev.cyclomatic
		}
		if prev.cognitive > mp.cognitive {
			mp.cognitive = prev.cognitive
		}
	}
	m[sym] = mp
}

// parseComplexityRow parses a builder-internal complexity row
// "name\x00cyclomatic\x00startLine\x00endLine[\x00cognitive]" into its symbol
// and metrics. cognitive is -1 when the row has no cognitive field.
func parseComplexityRow(r string) (string, metricPair, bool) {
	p := strings.Split(r, "\x00")
	if len(p) < 2 {
		return "", metricPair{}, false
	}
	cyc, err := strconv.Atoi(p[1])
	if err != nil {
		return "", metricPair{}, false
	}
	mp := metricPair{cyclomatic: cyc, cognitive: -1}
	if len(p) >= 5 {
		if cog, e := strconv.Atoi(p[4]); e == nil {
			mp.cognitive = cog
		}
	}
	return p[0], mp, true
}

// gitShowFile returns the content of relpath (repo-root-relative) at ref via
// `git show <ref>:<relpath>`. An error (e.g. the path didn't exist at ref)
// means no baseline for that file.
func gitShowFile(ctx context.Context, dir, ref, relpath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+filepath.ToSlash(relpath))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
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
