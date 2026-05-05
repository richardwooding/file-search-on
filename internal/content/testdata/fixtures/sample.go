// Package samplefixture is a test fixture for the source content-type
// family. The CLOC totals — line_count, loc, comment_loc, blank_loc —
// are exercised by TestFixturesAttributeSpotChecks.
package samplefixture

/*
Block comment spanning
three lines — counts as
3 comment_loc.
*/

import "fmt"

// Greet prints a greeting.
func Greet(name string) {
	fmt.Println("hello,", name)
}
