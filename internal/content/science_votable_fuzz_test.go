package content

import (
	"strings"
	"testing"
)

// FuzzParseVOTable targets the XML token walker + namespace + attr
// extraction. VOTable is a streaming parser exercising the
// encoding/xml decoder over potentially-adversarial XML — unclosed
// elements, huge attribute values, deeply-nested groups, and bogus
// numeric strings in `nrows`. The fuzz body asserts no panic and no
// negative numeric attributes.
func FuzzParseVOTable(f *testing.F) {
	// Seed 1: the minimal valid VOTable from the test suite.
	f.Add([]byte(minimalVOTable))

	// Seed 2: VOTABLE wrapper with no closing tag — exercises the
	// "decoder errored mid-parse" path. Should return best-effort
	// attrs, not crash.
	f.Add([]byte(`<?xml version="1.0"?><VOTABLE version="1.4"><RESOURCE><TABLE><FIELD name="x"/>`))

	// Seed 3: VOTable with adversarial nrows (negative).
	f.Add([]byte(`<?xml version="1.0"?><VOTABLE version="1.4"><RESOURCE><TABLE nrows="-99"><FIELD name="a"/></TABLE></RESOURCE></VOTABLE>`))

	// Seed 4: VOTable with huge nrows that would overflow int64 if
	// not bounds-checked.
	f.Add([]byte(`<?xml version="1.0"?><VOTABLE version="1.4"><RESOURCE><TABLE nrows="9999999999999999999999"><FIELD name="a"/></TABLE></RESOURCE></VOTABLE>`))

	// Seed 5: All 0xFF (binary noise) — must produce empty attrs,
	// not crash.
	bad := make([]byte, 256)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	// Seed 6: Empty input.
	f.Add([]byte{})

	// Seed 7: Pathological FIELD list (many tags). Stress-tests the
	// cap and the field_names append path.
	var pf strings.Builder
	pf.WriteString(`<?xml version="1.0"?><VOTABLE version="1.4"><RESOURCE><TABLE>`)
	for range 100 {
		pf.WriteString(`<FIELD name="x" unit="m" ucd="phys"/>`)
	}
	pf.WriteString(`</TABLE></RESOURCE></VOTABLE>`)
	f.Add([]byte(pf.String()))

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseVOTableHeader(data)
		if tc, ok := attrs["table_count"].(int64); ok && tc < 0 {
			t.Fatalf("table_count went negative: %d", tc)
		}
		if tr, ok := attrs["total_rows"].(int64); ok && tr < 0 {
			t.Fatalf("total_rows went negative: %d", tr)
		}
		if names, ok := attrs["field_names"].([]string); ok && int64(len(names)) > votableMaxFields {
			t.Fatalf("field_names exceeded cap: %d > %d", len(names), votableMaxFields)
		}
	})
}
