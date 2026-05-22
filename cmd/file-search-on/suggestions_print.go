package main

import (
	"fmt"
	"io"
)

// printSuggestions emits a "Suggestions:" header followed by a
// bulleted list to w when len(suggestions) > 0. No-op on empty
// input. Issue #168 sub-feature C — used by every cancellation-aware
// subcommand (search / stats / find-matches / duplicates / etc.).
//
// Stderr is the conventional sink; the help text is advisory, not
// data, so it shouldn't clutter stdout-piped output.
func printSuggestions(w io.Writer, suggestions []string) {
	if len(suggestions) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "Suggestions:")
	for _, s := range suggestions {
		_, _ = fmt.Fprintf(w, "  • %s\n", s)
	}
}
