package content

import "sync/atomic"

// DefaultMaxLineBytes is the per-line scanner buffer cap when nothing else has
// been configured. Lines longer than this are silently truncated, which can
// under-count line_count / word_count / column_count for unusually wide inputs
// (single-line JSON logs, minified output). Use SetMaxLineBytes to raise it.
const DefaultMaxLineBytes = 1 << 20 // 1 MiB

var maxLineBytes atomic.Int64

func init() {
	maxLineBytes.Store(int64(DefaultMaxLineBytes))
}

// MaxLineBytes is the current per-line buffer cap honoured by content types
// that read files line-by-line (text, csv, html). Read on every Attributes()
// call so the knob takes effect mid-walk if reset.
func MaxLineBytes() int {
	return int(maxLineBytes.Load())
}

// SetMaxLineBytes overrides the per-line buffer cap. Values <= 0 are ignored
// and the previous value is retained, so callers can pass an unset CLI flag
// without special-casing zero. Process-global; concurrent Walk calls that need
// different caps will race.
func SetMaxLineBytes(n int) {
	if n <= 0 {
		return
	}
	maxLineBytes.Store(int64(n))
}
