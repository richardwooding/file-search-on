package search

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
)

// defaultReadLinesMax bounds the worst-case response payload when
// the caller doesn't pass a max. 1000 lines is comfortably enough
// for "show me lines around a match" without flooding an MCP
// response. Override by passing a positive max to ReadLines.
const defaultReadLinesMax = 1000

// readLinesLineCap matches the snippet reader's per-line cap. Files
// with absurdly long single lines (minified JSON, rolled-up logs)
// won't blow up the scanner; the offending line is truncated to the
// cap and the scan continues. Independent of Options.MaxLineBytes.
const readLinesLineCap = 64 * 1024

// LinesResult is the payload ReadLines returns. Lines is the
// requested range without trailing newlines (one element per
// physical line). TotalLines is the total line count of the file
// (always computed — useful for agents that want context).
// Truncated is true when the requested range exceeded maxLines and
// only the first maxLines of the range are present.
type LinesResult struct {
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	TotalLines int      `json:"total_lines"`
	Lines      []string `json:"lines"`
	Truncated  bool     `json:"truncated,omitempty"`
}

// ErrInvalidLineRange is returned when start_line > end_line (and
// both are positive). Out-of-range values (start past EOF, end <
// 1) are tolerated — they yield an empty Lines slice rather than an
// error, matching the cross-codebase "broken input degrades" pattern.
var ErrInvalidLineRange = errors.New("invalid line range: start_line > end_line")

// ReadLines opens path on fsys and returns lines [start, end]
// (1-indexed, inclusive). When end == 0 it means "to end of file".
// When start == 0 it defaults to 1. maxLines caps the returned
// slice (0 → defaultReadLinesMax); the LinesResult.Truncated flag
// signals when the cap was hit.
//
// ctx is checked at entry and at every line read so cancellation
// propagates promptly. The full file is scanned to compute
// TotalLines even when the requested range is at the start —
// agents typically want the total alongside.
func ReadLines(ctx context.Context, fsys fs.FS, path string, start, end, maxLines int) (*LinesResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if start <= 0 {
		start = 1
	}
	if maxLines <= 0 {
		maxLines = defaultReadLinesMax
	}
	if end > 0 && start > end {
		return nil, ErrInvalidLineRange
	}

	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), readLinesLineCap)

	res := &LinesResult{StartLine: start}
	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNo++
		if lineNo < start {
			continue
		}
		if end > 0 && lineNo > end {
			// Keep scanning to compute TotalLines.
			continue
		}
		if int64(len(res.Lines)) >= int64(maxLines) {
			res.Truncated = true
			// Still keep scanning — TotalLines is useful even
			// past the truncation point.
			continue
		}
		// scanner.Bytes() is reused on the next Scan(); copy.
		line := string(scanner.Bytes())
		res.Lines = append(res.Lines, line)
	}
	res.TotalLines = lineNo

	// Clamp the reported end_line so callers see what they
	// actually got. If end was 0 (EOF), report the last line read
	// up to the cap. If end was past EOF, clamp to TotalLines.
	switch {
	case len(res.Lines) == 0:
		res.EndLine = start - 1
	case res.Truncated:
		res.EndLine = res.StartLine + len(res.Lines) - 1
	case end == 0 || end > res.TotalLines:
		res.EndLine = res.TotalLines
	default:
		res.EndLine = end
	}
	// scanner.Err() is intentionally not surfaced — files that
	// blow the per-line cap or contain weird bytes still yield
	// whatever was scanned. Matches snippet.go's degradation.
	return res, nil
}
