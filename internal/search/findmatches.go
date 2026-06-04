package search

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

// LineMatch is one regex hit inside a file. Path is the user-facing
// path (matching search.Match.Path); Line is 1-indexed; Text is the
// matched line, raw, with no trailing newline. Before / After are
// up to Options.ContextBefore / ContextAfter surrounding lines
// (empty when the file boundary is reached before filling them or
// when context is disabled).
type LineMatch struct {
	Path        string   `json:"path"`
	ContentType string   `json:"content_type,omitempty"`
	Line        int      `json:"line"`
	Text        string   `json:"text"`
	Before      []string `json:"before,omitempty"`
	After       []string `json:"after,omitempty"`
}

// FindMatchesResult is the aggregate response. Matches is sorted by
// (Path, Line) ascending. FilesScanned counts files that were opened
// and line-scanned (after the CEL prune); FilesWithMatches counts
// files that produced at least one match. Cancelled / CancellationReason
// mirror the search / stats / duplicates partial-result contract.
//
// TruncatedFiles names every file whose scanner hit
// findMatchesLineCap on at least one line — the over-cap suffix
// wasn't searched, so a regex that would have matched past the cap
// silently misses. Issue #283.
type FindMatchesResult struct {
	Matches            []LineMatch `json:"matches"`
	Count              int         `json:"count"`
	FilesScanned       int         `json:"files_scanned"`
	FilesWithMatches   int         `json:"files_with_matches"`
	TruncatedFiles     []string    `json:"truncated_files,omitempty"`
	Cancelled          bool        `json:"cancelled,omitempty"`
	CancellationReason string      `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64     `json:"elapsed_seconds,omitempty"`
}

// ErrEmptyPattern is returned when FindMatches is called with no
// regex pattern. Distinct from a regex-compile error so callers can
// surface different messages.
var ErrEmptyPattern = errors.New("pattern is required")

// matchInMode is the internal enum behind the user-facing Options.MatchIn
// string. Validated up front in parseMatchIn so each per-file scan
// reads an enum, not the string. Issue #272.
type matchInMode uint8

const (
	matchInAny matchInMode = iota
	matchInComments
	matchInCode
)

// MatchInAny / MatchInComments / MatchInCode are the user-facing
// string constants for Options.MatchIn. Exposed so CLI / MCP layers
// can validate at parse time and the CEL prune layer (find_matches
// preset, etc) doesn't have to keep magic strings.
const (
	MatchInAny      = "any"
	MatchInComments = "comments"
	MatchInCode     = "code"
)

// parseMatchIn maps the user-facing string to the internal enum.
// Empty / "any" → matchInAny (no filtering). Unknown values error
// at the entry point — fail-fast is better than silent no-op when
// the user typo'd the mode.
func parseMatchIn(s string) (matchInMode, error) {
	switch s {
	case "", MatchInAny:
		return matchInAny, nil
	case MatchInComments:
		return matchInComments, nil
	case MatchInCode:
		return matchInCode, nil
	}
	return matchInAny, fmt.Errorf("match_in: unknown value %q (want one of any|comments|code)", s)
}

// findMatchesLineCap bounds the per-line scanner buffer. Matches the
// read_lines / snippet pattern: pathological single-line files (rolled
// logs, minified JSON) don't blow up the response — the offending line
// is truncated to the cap and the scan continues.
const findMatchesLineCap = 64 * 1024

// findMatchesBodyCap bounds the extracted text for structured documents
// (office / epub / pdf / …) when Options.BodyMaxBytes is unset. 8 MiB
// comfortably covers a full-length book's text while keeping per-file
// memory bounded across workers. Text past the cap isn't scanned.
const findMatchesBodyCap = 8 << 20

// FindMatches walks opts.Root / opts.Roots, applies the CEL filter,
// and for each text-typed match opens the file and reports every line
// that matches opts.Pattern (RE2). Context windows (opts.ContextBefore
// / opts.ContextAfter) attach surrounding lines to each hit;
// opts.MaxMatchesPerFile caps matches within a single file (0 =
// unlimited).
//
// Plain-text content types (markdown / text / html / csv / json / xml /
// source/*) are line-scanned directly. Structured documents whose text
// lives behind a container — office (DOCX/XLSX/PPTX/ODT), epub, pdf,
// email (.eml/.mbox), and the browser/chat/sqlite exports — are first
// run through content.ExtractBody and the extracted text is scanned, so
// `find-matches "Captain Ahab"` finds the phrase inside an .epub
// (issue #309). Truly binary families (image / audio / video / archive /
// compiled binary) are filtered out — there's no useful "line" concept.
//
// Like FindDuplicates this is two-pass: Walk to collect candidates,
// then parallel regex-scan. Walk-stage cancellation is honoured via the
// shared partial-result contract; scan-stage cancellation drops any
// in-flight file's matches and returns the partial set with
// cancelled=true.
func FindMatches(ctx context.Context, opts Options, registry *content.Registry) (*FindMatchesResult, error) {
	start := time.Now()

	if opts.Pattern == "" {
		return nil, ErrEmptyPattern
	}
	re, err := regexp.Compile(opts.Pattern)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	matchIn, err := parseMatchIn(opts.MatchIn)
	if err != nil {
		return nil, err
	}

	// Match the CLI / MCP search-handler convention: empty CEL expr
	// means "every file". FindMatches's own filter is the regex
	// pattern; the CEL expr is just the optional type/attribute
	// pre-prune.
	if opts.Expr == "" {
		opts.Expr = "true"
	}

	// Force on / off the bits FindMatches actually cares about.
	// Sort/Limit/Snippet/Body are file-level concepts; they're
	// re-applied (or ignored) at the line-level post-collection.
	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false
	opts.IncludeBody = false

	results, walkErr := Walk(ctx, opts, registry)

	out := &FindMatchesResult{}

	// Filter to scannable content: plain text (scanned raw) plus
	// structured documents whose body we can extract (office / epub /
	// pdf / email / …). Truly binary families are silently dropped —
	// see the doc comment for the rationale.
	var candidates []Result
	for _, r := range results {
		if isTextContentType(r.ContentType) || content.SupportsBodyExtraction(r.ContentType) {
			candidates = append(candidates, r)
		}
	}

	// Cap on the extracted document body (distinct from the per-line
	// scanner cap). Honour an explicit Options.BodyMaxBytes; otherwise
	// use a generous default that covers full-length books.
	bodyCap := opts.BodyMaxBytes
	if bodyCap <= 0 {
		bodyCap = findMatchesBodyCap
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Tests inject opts.FS to scan an in-memory filesystem; production
	// callers leave it nil and we fall through to os.Open via the
	// Result.Path (which Walk already absolutized).
	jobs := make(chan Result, workers*2)
	var (
		mu               sync.Mutex
		allMatches       []LineMatch
		filesScanned     int
		filesWithMatches int
		truncatedFiles   []string
		wg               sync.WaitGroup
	)

	for range workers {
		wg.Go(func() {
			for r := range jobs {
				if ctx.Err() != nil {
					return
				}
				hits, truncated := scanResultForMatches(ctx, opts.FS, r, re, opts.ContextBefore, opts.ContextAfter, opts.MaxMatchesPerFile, matchIn, bodyCap)
				mu.Lock()
				filesScanned++
				if len(hits) > 0 {
					filesWithMatches++
					allMatches = append(allMatches, hits...)
				}
				if truncated {
					truncatedFiles = append(truncatedFiles, r.Path)
				}
				mu.Unlock()
			}
		})
	}

	for _, c := range candidates {
		select {
		case <-ctx.Done():
			// Stop feeding; the workers see ctx.Done() and return.
		case jobs <- c:
			continue
		}
		break
	}
	close(jobs)
	wg.Wait()

	sort.Slice(allMatches, func(i, j int) bool {
		if allMatches[i].Path != allMatches[j].Path {
			return allMatches[i].Path < allMatches[j].Path
		}
		return allMatches[i].Line < allMatches[j].Line
	})

	sort.Strings(truncatedFiles)
	out.Matches = allMatches
	out.Count = len(allMatches)
	out.FilesScanned = filesScanned
	out.FilesWithMatches = filesWithMatches
	out.TruncatedFiles = truncatedFiles
	out.ElapsedSeconds = time.Since(start).Seconds()

	// Cancellation: same precedence as duplicates / stats. Walk-stage
	// error wins; otherwise check the live ctx.
	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			out.Cancelled = true
			out.CancellationReason = "client_cancel"
			return out, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			out.Cancelled = true
			out.CancellationReason = "timeout"
			return out, nil
		}
		return out, walkErr
	}
	if ctx.Err() != nil {
		out.Cancelled = true
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.CancellationReason = "timeout"
		} else {
			out.CancellationReason = "client_cancel"
		}
	}
	return out, nil
}

// scanResultForMatches opens r via fsys (if non-nil) or os.Open
// (otherwise), runs the regex line scan, and stamps each hit with
// r's user-facing Path and ContentType. Errors opening a single file
// drop that file silently — matches the broader "broken file doesn't
// abort the walk" pattern. fsys is non-nil only in test paths that
// pass Options.FS.
func scanResultForMatches(ctx context.Context, fsys fs.FS, r Result, re *regexp.Regexp, ctxBefore, ctxAfter, maxPerFile int, matchIn matchInMode, bodyCap int) ([]LineMatch, bool) {
	if err := ctx.Err(); err != nil {
		return nil, false
	}

	// Structured documents (office / epub / pdf / email / …) carry their
	// text behind a container, so a raw byte scan finds nothing. Extract
	// the body and scan THAT instead. Issue #309.
	if content.SupportsBodyExtraction(r.ContentType) {
		body := extractDocumentBody(ctx, fsys, r.Path, r.ContentType, bodyCap)
		if body == "" {
			return nil, false
		}
		hits, truncated := scanReaderForMatches(ctx, strings.NewReader(body), re, ctxBefore, ctxAfter, maxPerFile, matchIn, commentSyntax{}, false)
		for i := range hits {
			hits[i].Path = r.Path
			hits[i].ContentType = r.ContentType
		}
		return hits, truncated
	}

	var rd io.ReadCloser
	if fsys != nil {
		f, err := fsys.Open(r.Path)
		if err != nil {
			return nil, false
		}
		rd = f
	} else {
		f, err := os.Open(r.Path)
		if err != nil {
			return nil, false
		}
		rd = f
	}
	defer func() { _ = rd.Close() }()

	// Resolve per-language syntax once per file. matchIn=any short-
	// circuits the lookup entirely (zero syntax → classifier never
	// runs). Non-source content types (markdown / json / etc) have no
	// syntax registered — the scanner sees ok=false and skips role
	// filtering, so MatchIn="comments" against a markdown file is a
	// no-op. Issue #272.
	var (
		syntax commentSyntax
		hasSyntax bool
	)
	if matchIn != matchInAny {
		syntax, hasSyntax = commentSyntaxFor(languageFromContentType(r.ContentType))
	}

	hits, truncated := scanReaderForMatches(ctx, rd, re, ctxBefore, ctxAfter, maxPerFile, matchIn, syntax, hasSyntax)
	for i := range hits {
		hits[i].Path = r.Path
		hits[i].ContentType = r.ContentType
	}
	return hits, truncated
}

// scanReaderForMatches is the pure scanner — given any io.Reader,
// return every regex match with the requested before/after context.
// Exported via package-private name only; FindMatches and tests both
// go through here. Maintains a ring buffer for the "before" window
// and a pending-indices slice for the "after" window so a single
// linear pass fills both. Past the maxPerFile cap we keep scanning
// (without adding new matches) until every pending After window is
// filled — otherwise the last few matches would carry truncated
// After lines, which is surprising.
func scanReaderForMatches(ctx context.Context, rd io.Reader, re *regexp.Regexp, ctxBefore, ctxAfter, maxPerFile int, matchIn matchInMode, syntax commentSyntax, hasSyntax bool) ([]LineMatch, bool) {
	scanner := bufio.NewScanner(rd)
	scanner.Buffer(make([]byte, 0, 4096), findMatchesLineCap)

	var (
		matches []LineMatch
		before  []string // ring; len <= ctxBefore
		pending []int    // indices into matches waiting for their After window
		inBlock bool     // running block-comment state for the classifier
	)
	lineNo := 0
	// filterByRole is true only when the caller asked for comments/code
	// AND we recognised the file's language. Files without syntax
	// (markdown, json, raw text) or matchIn=any leave filtering off.
	filterByRole := matchIn != matchInAny && hasSyntax

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return matches, false
		}
		lineNo++
		// scanner.Bytes() is reused on next Scan(); copy.
		line := string(scanner.Bytes())

		// Classify role + advance block-comment state. We always
		// advance the state (so the classifier stays accurate across
		// the file) but only USE the role when filterByRole is set.
		var role lineRole
		if hasSyntax {
			role, inBlock = classifyLine(line, syntax, inBlock)
		}

		// Fill any pending After windows; drop those whose After is
		// full. Reuse pending's backing array (kept = pending[:0]).
		if len(pending) > 0 {
			kept := pending[:0]
			for _, idx := range pending {
				if len(matches[idx].After) < ctxAfter {
					matches[idx].After = append(matches[idx].After, line)
				}
				if len(matches[idx].After) < ctxAfter {
					kept = append(kept, idx)
				}
			}
			pending = kept
		}

		atCap := maxPerFile > 0 && len(matches) >= maxPerFile
		if !atCap && re.MatchString(line) && roleMatches(filterByRole, matchIn, role) {
			m := LineMatch{Line: lineNo, Text: line}
			if len(before) > 0 {
				m.Before = append([]string(nil), before...)
			}
			matches = append(matches, m)
			if ctxAfter > 0 {
				pending = append(pending, len(matches)-1)
			}
		}

		// Past the cap AND no pending After to fill → done.
		if atCap && len(pending) == 0 {
			break
		}

		// Slide the before-ring window.
		if ctxBefore > 0 {
			before = append(before, line)
			if len(before) > ctxBefore {
				before = before[1:]
			}
		}
	}
	// Detect truncation: bufio.Scanner halts and stamps
	// bufio.ErrTooLong on scanner.Err() when a single line exceeds
	// the buffer cap. The matches we already collected stay; the
	// over-cap line and everything after it didn't scan. Surface
	// this to the caller via the second return so FindMatches can
	// list the affected file path in TruncatedFiles and emit a hint
	// in Suggestions. Issue #283.
	truncated := errors.Is(scanner.Err(), bufio.ErrTooLong)
	return matches, truncated
}

// extractDocumentBody turns a structured document into plain text for
// line scanning. Test callers pass a non-nil fsys and r.Path is the
// in-fs path; production callers pass nil and r.Path is an absolute OS
// path, which we expose to ExtractBody via an os.DirFS rooted at the
// file's parent. database/sqlite (and any future OS-path-only
// extractor) can't go through fs.FS — fall back to ExtractBodyOSPath.
// Errors / unsupported types degrade to "" so the file is skipped, not
// fatal — matching the broader "broken file doesn't abort the walk".
func extractDocumentBody(ctx context.Context, fsys fs.FS, path, contentType string, maxBytes int) string {
	if fsys != nil {
		body, _ := content.ExtractBody(ctx, contentType, fsys, path, maxBytes)
		return body
	}
	if content.RequiresOSPath(contentType) {
		body, _ := content.ExtractBodyOSPath(ctx, contentType, path, maxBytes)
		return body
	}
	body, _ := content.ExtractBody(ctx, contentType, os.DirFS(filepath.Dir(path)), filepath.Base(path), maxBytes)
	return body
}

// roleMatches reports whether a line of role `r` survives the MatchIn
// filter. When filtering is off (filterByRole=false), every role
// passes — that covers matchIn=any AND the "language unrecognised /
// non-source" no-op path.
func roleMatches(filterByRole bool, mode matchInMode, r lineRole) bool {
	if !filterByRole {
		return true
	}
	switch mode {
	case matchInComments:
		return r == roleComment
	case matchInCode:
		return r == roleCode
	}
	return true
}
