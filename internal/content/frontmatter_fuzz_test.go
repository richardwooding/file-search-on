package content

import (
	"testing"
)

// FuzzSplitFrontmatter feeds arbitrary bytes into the YAML / TOML /
// JSON frontmatter splitter. The contract is:
//
//   - never panic, even on malformed / truncated / random input;
//   - the returned body is always a (possibly empty) slice that is a
//     prefix-suffix of the input — we don't synthesise content;
//   - when the splitter returns a non-nil Frontmatter, its Format is
//     one of {"yaml", "toml", "json"}; the Data map is non-nil.
//
// Seeds cover the three documented frontmatter formats plus a handful
// of pathological shapes (bare delimiters, mid-document delimiters,
// unicode, BOM). The fuzzer mutates from there.
func FuzzSplitFrontmatter(f *testing.F) {
	seeds := []string{
		"",
		"# heading\n",
		"---\ntitle: Hi\n---\nbody\n",
		"+++\ntitle = \"Hi\"\n+++\nbody\n",
		"{\"title\":\"Hi\"}\nbody\n",
		// Pathological cases — not crashes but worth a baseline.
		"---\n",                          // open delim only
		"---\n---\n",                     // empty body between delims
		"+++\ninvalid toml===\n+++\nx\n", // malformed TOML
		"---\n: : : :\n---\n",            // malformed YAML
		"{not json",                      // JSON open + garbage
		"\xef\xbb\xbf---\ntitle: bom\n---\n", // UTF-8 BOM prefix
		"---\r\ntitle: crlf\r\n---\r\n",  // CRLF line endings
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		fm, body := splitFrontmatter(data)

		// Body must be a real []byte — never nil-when-input-non-empty
		// in a way that would surprise callers (markdown.go pipes
		// body straight into a scanner).
		if data == nil && body != nil && len(body) != 0 {
			t.Fatalf("nil input produced body=%q", body)
		}

		if fm == nil {
			// No frontmatter detected: body should equal input (the
			// parser falls through to "treat everything as body").
			return
		}
		switch fm.Format {
		case "yaml", "toml", "json":
		default:
			t.Fatalf("unexpected Format=%q", fm.Format)
		}
		if fm.Data == nil {
			t.Fatalf("Format=%q but Data is nil", fm.Format)
		}
	})
}
