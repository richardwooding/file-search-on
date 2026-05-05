// Package testdata embeds the bank of public-domain content-type
// fixtures used by the test suite. The bank is exposed as Fixtures, an
// fs.FS rooted at the fixtures/ directory — so callers see paths like
// "sample.md", not "fixtures/sample.md".
//
// Importing this package costs ~120 KB of binary (the fixtures' total
// uncompressed size). It is NOT imported by the production binary —
// only by *_test.go files.
//
// All fixtures are CC0 / public domain. See fixtures/README.md for
// per-file generator commands.
package testdata

import (
	"embed"
	"io/fs"
)

//go:embed fixtures
var rootFS embed.FS

// Fixtures is an fs.FS rooted at the fixtures/ directory. Paths inside
// the returned FS are bare filenames ("sample.md", "sample.jpg", ...).
var Fixtures fs.FS

func init() {
	sub, err := fs.Sub(rootFS, "fixtures")
	if err != nil {
		panic("testdata: fs.Sub on embedded fixtures failed: " + err.Error())
	}
	Fixtures = sub
}
