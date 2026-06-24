package main

import (
	"os"

	"github.com/richardwooding/file-search-on/internal/sarif"
)

// writeSARIF emits a SARIF 2.1.0 document for one analysis rule + its results
// to stdout, stamped with the binary version. The analysis commands build
// their []sarif.Result and call this for the `--output sarif` format (#483).
func writeSARIF(rule sarif.Rule, results []sarif.Result) error {
	return sarif.Write(os.Stdout, version, []sarif.Rule{rule}, results)
}

// truncateForMessage caps a one-line SARIF message so a long matched line
// doesn't bloat the result. Rune-safe.
func truncateForMessage(s string) string {
	const max = 200
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
