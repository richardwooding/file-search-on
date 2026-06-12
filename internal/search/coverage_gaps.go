package search

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// CoverageGap is one function whose statement coverage is below the
// requested threshold (issue #397).
type CoverageGap struct {
	Path              string  `json:"path"`
	Function          string  `json:"function"`
	StartLine         int     `json:"start_line"`
	EndLine           int     `json:"end_line"`
	CoveredStatements int     `json:"covered_statements"`
	TotalStatements   int     `json:"total_statements"`
	CoveredPct        float64 `json:"covered_pct"`
	FullyUncovered    bool    `json:"fully_uncovered"`
}

// CoverageGapsResult is the aggregate result of CoverageGaps.
type CoverageGapsResult struct {
	ProfileMode   string        `json:"profile_mode"`
	FilesAnalysed int           `json:"files_analysed"`
	Threshold     float64       `json:"threshold"`
	Gaps          []CoverageGap `json:"gaps"`
	Count         int           `json:"count"`
}

// coverBlock is one parsed line of a Go coverage profile.
type coverBlock struct {
	startLine int
	numStmt   int
	covered   bool
}

// CoverageGaps reads a Go coverage profile (go test -coverprofile) and reports
// functions whose statement coverage is below threshold (issue #397) — the
// precise complement to test_gaps, which needs no profile but only sees
// direct test references. Each profiled .go file is resolved to disk (its
// import path stripped of the module prefix from root/go.mod), split into
// functions via content.FunctionSpans, and each function's blocks summed.
//
// threshold is a coverage fraction in [0,1]: a function is a gap when its
// covered fraction is strictly below it. threshold 1.0 (the default when <= 0)
// reports every function not fully covered; 0.8 reports functions under 80%.
// Functions with zero statements (no executable lines) are never gaps.
//
// Go coverage profiles only; the format is `importpath/file.go:sL.sC,eL.eC
// numStmt count` after a `mode:` header.
func CoverageGaps(ctx context.Context, profilePath, root string, threshold float64, registry *content.Registry) (*CoverageGapsResult, error) {
	if threshold <= 0 || threshold > 1 {
		threshold = 1.0
	}
	mode, byFile, err := parseCoverProfile(profilePath)
	if err != nil {
		return nil, err
	}
	module := moduledPath(root)

	out := &CoverageGapsResult{ProfileMode: mode, Threshold: threshold}
	for importFile, blocks := range byFile {
		if ctx.Err() != nil {
			break
		}
		disk, ok := resolveProfilePath(importFile, module, root)
		if !ok {
			continue // external dependency or unresolvable — skip
		}
		src, rerr := os.ReadFile(disk)
		if rerr != nil {
			continue
		}
		spans := content.FunctionSpans("source/go", src)
		if len(spans) == 0 {
			continue
		}
		out.FilesAnalysed++
		rel := relForReport(importFile, module)
		for _, sp := range spans {
			total, covered := 0, 0
			for _, b := range blocks {
				if b.startLine >= sp.StartLine && b.startLine <= sp.EndLine {
					total += b.numStmt
					if b.covered {
						covered += b.numStmt
					}
				}
			}
			if total == 0 {
				continue // no executable statements — nothing to cover
			}
			pct := float64(covered) / float64(total)
			if pct >= threshold {
				continue
			}
			out.Gaps = append(out.Gaps, CoverageGap{
				Path:              rel,
				Function:          sp.Name,
				StartLine:         sp.StartLine,
				EndLine:           sp.EndLine,
				CoveredStatements: covered,
				TotalStatements:   total,
				CoveredPct:        pct,
				FullyUncovered:    covered == 0,
			})
		}
	}

	// Worst coverage first, then biggest gap (uncovered statements) desc,
	// then path/line for determinism.
	sort.Slice(out.Gaps, func(i, j int) bool {
		a, b := out.Gaps[i], out.Gaps[j]
		if a.CoveredPct != b.CoveredPct {
			return a.CoveredPct < b.CoveredPct
		}
		ua, ub := a.TotalStatements-a.CoveredStatements, b.TotalStatements-b.CoveredStatements
		if ua != ub {
			return ua > ub
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})
	out.Count = len(out.Gaps)
	return out, nil
}

// parseCoverProfile reads a Go coverage profile into its mode and a map of
// import-path file → blocks.
func parseCoverProfile(path string) (mode string, byFile map[string][]coverBlock, error error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = f.Close() }()

	byFile = map[string][]coverBlock{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if after, ok := strings.CutPrefix(line, "mode:"); ok {
			mode = strings.TrimSpace(after)
			continue
		}
		file, blk, ok := parseCoverLine(line)
		if !ok {
			continue
		}
		byFile[file] = append(byFile[file], blk)
	}
	return mode, byFile, sc.Err()
}

// parseCoverLine parses one block line: `file:sL.sC,eL.eC numStmt count`.
func parseCoverLine(line string) (file string, blk coverBlock, ok bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", coverBlock{}, false
	}
	numStmt, err1 := strconv.Atoi(fields[1])
	count, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil {
		return "", coverBlock{}, false
	}
	// fields[0] = "import/path/file.go:sL.sC,eL.eC"; the colon before the
	// range separates the (colon-free) file path from the position span.
	colon := strings.LastIndex(fields[0], ":")
	if colon < 0 {
		return "", coverBlock{}, false
	}
	file = fields[0][:colon]
	startCol, _, found := strings.Cut(fields[0][colon+1:], ",")
	if !found {
		return "", coverBlock{}, false
	}
	startLineStr, _, _ := strings.Cut(startCol, ".")
	startLine, err := strconv.Atoi(startLineStr)
	if err != nil {
		return "", coverBlock{}, false
	}
	return file, coverBlock{startLine: startLine, numStmt: numStmt, covered: count > 0}, true
}

// moduledPath returns the module path declared in root/go.mod, or "".
func moduledPath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// resolveProfilePath maps a coverage-profile import-path filename to a disk
// path under root by stripping the module prefix. Files outside the module
// (no prefix match) return ok=false.
func resolveProfilePath(importFile, module, root string) (string, bool) {
	rel := relForReport(importFile, module)
	if rel == "" || rel == importFile && module != "" {
		return "", false // had a module but this file isn't under it
	}
	return filepath.Join(root, filepath.FromSlash(rel)), true
}

// relForReport returns the module-relative path used in reporting (the import
// path minus the module prefix). When module is empty, returns importFile
// unchanged (best-effort).
func relForReport(importFile, module string) string {
	if module == "" {
		return importFile
	}
	if importFile == module {
		return ""
	}
	if rel, ok := strings.CutPrefix(importFile, module+"/"); ok {
		return rel
	}
	return importFile // not under the module — caller decides
}
