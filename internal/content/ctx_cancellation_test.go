package content_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/richardwooding/file-search-on/internal/content"
)

// TestAttributes_HonoursCancelledContext is a regression test for the
// ctx-cancellation audit. It feeds each in-scope ContentType a
// pathologically-large input (~32k lines / tokens) that would take
// measurable wall-clock to fully parse, then calls Attributes with an
// already-cancelled ctx, and asserts the call returns an error
// without hanging.
//
// "Without hanging" is enforced by the global test timeout in CI
// (3 minutes — far longer than any honest parse needed below). A
// regression that drops the ctx.Err() check inside a Scanner loop
// would manifest as this test hitting the timeout instead of
// returning fast.
func TestAttributes_HonoursCancelledContext(t *testing.T) {
	// Build a 32k-line pathological body. For most line-scanner types
	// this is enough wall-clock to make an unguarded loop visibly slow
	// even under -race; for the tighter parsers (Dublin Core / Chromium)
	// the cancelled-at-entry guards win immediately.
	long := strings.Repeat("a,b,c,d,e,f,g\n", 32*1024)
	goSrc := strings.Repeat("// comment\npackage main\n", 16*1024)
	cssvSrc := strings.Repeat("a,b,c\n", 32*1024)

	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "csv", path: "a.csv", body: cssvSrc},
		{name: "text", path: "a.txt", body: long},
		{name: "source/go", path: "a.go", body: goSrc},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // pre-cancel — every iteration should observe Err()

			fsys := fstest.MapFS{c.path: {Data: []byte(c.body)}}
			ct := content.DefaultRegistry().Detect(fsys, c.path)
			if ct == nil {
				t.Fatalf("Detect returned nil for %q", c.path)
			}
			_, err := ct.Attributes(ctx, fsys, c.path)
			if err == nil {
				t.Errorf("Attributes did not surface ctx cancellation (expected ctx.Err)")
			}
		})
	}
}
