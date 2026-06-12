// Package arch holds repo-wide architectural guard tests — tripwires that
// fail the suite when a structural property the project cares about regresses.
//
// These are not unit tests of any one package; they parse the module's own
// source tree and assert invariants over it. Today that's the import-fan-out
// ceiling (issue #388): a watch on coupling growth in the core packages. The
// package has no production code — only the guards under *_test.go.
package arch
