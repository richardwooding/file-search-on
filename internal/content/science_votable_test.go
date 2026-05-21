package content

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// minimalVOTable is the simplest VOTable that should detect and parse
// — one RESOURCE, one TABLE, two FIELDs, no DATA payload.
const minimalVOTable = `<?xml version="1.0" encoding="UTF-8"?>
<VOTABLE version="1.4" xmlns="http://www.ivoa.net/xml/VOTable/v1.3">
  <DESCRIPTION>Test catalog</DESCRIPTION>
  <INFO name="creator" value="Test Suite"/>
  <RESOURCE>
    <TABLE nrows="42">
      <FIELD name="ra" datatype="double" unit="deg" ucd="pos.eq.ra"/>
      <FIELD name="dec" datatype="double" unit="deg" ucd="pos.eq.dec"/>
      <FIELD name="mag" datatype="float" unit="mag" ucd="phot.mag"/>
      <DATA>
        <TABLEDATA/>
      </DATA>
    </TABLE>
  </RESOURCE>
</VOTABLE>`

func TestVOTable_MinimalDetectAndAttrs(t *testing.T) {
	fsys := fstest.MapFS{"catalog.vot": {Data: []byte(minimalVOTable)}}
	ct := DefaultRegistry().Detect(fsys, "catalog.vot")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/votable" {
		t.Fatalf("got %s, want science/votable", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "catalog.vot")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"science_format":      "votable",
		"votable_version":     "1.4",
		"table_count":         int64(1),
		"total_rows":          int64(42),
		"votable_data_format": "tabledata",
		"title":               "Test catalog",
		"author":              "Test Suite",
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	fieldNames, ok := attrs["field_names"].([]string)
	if !ok || len(fieldNames) != 3 {
		t.Fatalf("field_names = %v, want 3-element []string", attrs["field_names"])
	}
	if fieldNames[0] != "ra" || fieldNames[2] != "mag" {
		t.Errorf("field_names = %v", fieldNames)
	}
	fieldUCDs, _ := attrs["field_ucds"].([]string)
	if len(fieldUCDs) != 3 || fieldUCDs[0] != "pos.eq.ra" {
		t.Errorf("field_ucds = %v, want [pos.eq.ra, pos.eq.dec, phot.mag]", fieldUCDs)
	}
}

func TestVOTable_MultiTable(t *testing.T) {
	body := `<?xml version="1.0"?>
<VOTABLE version="1.3">
  <RESOURCE>
    <TABLE nrows="10">
      <FIELD name="a"/>
    </TABLE>
    <TABLE nrows="20">
      <FIELD name="b"/>
      <FIELD name="c"/>
    </TABLE>
  </RESOURCE>
</VOTABLE>`
	fsys := fstest.MapFS{"x.vot": {Data: []byte(body)}}
	attrs, err := DefaultRegistry().Detect(fsys, "x.vot").Attributes(context.Background(), fsys, "x.vot")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["table_count"]; got != int64(2) {
		t.Errorf("table_count = %v, want 2", got)
	}
	if got := attrs["total_rows"]; got != int64(30) {
		t.Errorf("total_rows = %v, want 30 (10+20)", got)
	}
	names, _ := attrs["field_names"].([]string)
	if len(names) != 3 {
		t.Errorf("field_names across both tables = %v, want 3", names)
	}
}

func TestVOTable_NonVOTableXMLReturnsEmpty(t *testing.T) {
	// An XML file with a non-VOTable root produces empty attrs — the
	// file detects (extension matches .vot) but the parser bails on
	// the namespace check.
	body := `<?xml version="1.0"?>
<NOT-VOTABLE>
  <something/>
</NOT-VOTABLE>`
	fsys := fstest.MapFS{"weird.vot": {Data: []byte(body)}}
	attrs, err := DefaultRegistry().Detect(fsys, "weird.vot").Attributes(context.Background(), fsys, "weird.vot")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 0 {
		t.Errorf("non-VOTable returned attrs: %v", attrs)
	}
}

func TestVOTable_WrongNamespaceReturnsEmpty(t *testing.T) {
	body := `<?xml version="1.0"?>
<VOTABLE xmlns="http://example.com/not-ivoa" version="1.4"/>`
	fsys := fstest.MapFS{"x.vot": {Data: []byte(body)}}
	attrs, err := DefaultRegistry().Detect(fsys, "x.vot").Attributes(context.Background(), fsys, "x.vot")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 0 {
		t.Errorf("non-IVOA namespace produced attrs: %v", attrs)
	}
}

func TestVOTable_EmptyNamespaceAccepted(t *testing.T) {
	// Hand-written VOTables sometimes omit xmlns. We accept those.
	body := `<?xml version="1.0"?>
<VOTABLE version="1.3">
  <RESOURCE>
    <TABLE nrows="5">
      <FIELD name="x"/>
    </TABLE>
  </RESOURCE>
</VOTABLE>`
	fsys := fstest.MapFS{"x.vot": {Data: []byte(body)}}
	attrs, err := DefaultRegistry().Detect(fsys, "x.vot").Attributes(context.Background(), fsys, "x.vot")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["votable_version"]; got != "1.3" {
		t.Errorf("expected version 1.3 from no-namespace VOTable, got %v", got)
	}
}

func TestVOTable_Truncated(t *testing.T) {
	// Truncated XML must not panic; best-effort attrs returned.
	truncated := minimalVOTable[:200]
	fsys := fstest.MapFS{"t.vot": {Data: []byte(truncated)}}
	if _, err := DefaultRegistry().Detect(fsys, "t.vot").Attributes(context.Background(), fsys, "t.vot"); err != nil {
		t.Errorf("truncated input errored: %v", err)
	}
}

func TestVOTable_DataFormatBinary(t *testing.T) {
	body := `<?xml version="1.0"?>
<VOTABLE version="1.4">
  <RESOURCE>
    <TABLE>
      <FIELD name="a"/>
      <DATA>
        <BINARY2><STREAM encoding="base64">AAAA</STREAM></BINARY2>
      </DATA>
    </TABLE>
  </RESOURCE>
</VOTABLE>`
	fsys := fstest.MapFS{"b.vot": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "b.vot").Attributes(context.Background(), fsys, "b.vot")
	if got := attrs["votable_data_format"]; got != "binary2" {
		t.Errorf("votable_data_format = %v, want binary2", got)
	}
}

func TestVOTable_FieldCap(t *testing.T) {
	// A VOTable claiming more FIELDs than the cap should clip the
	// list at votableMaxFields without panicking.
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><VOTABLE version="1.4"><RESOURCE><TABLE>`)
	for range votableMaxFields + 50 {
		sb.WriteString(`<FIELD name="x"/>`)
	}
	sb.WriteString(`</TABLE></RESOURCE></VOTABLE>`)
	fsys := fstest.MapFS{"big.vot": {Data: []byte(sb.String())}}
	attrs, err := DefaultRegistry().Detect(fsys, "big.vot").Attributes(context.Background(), fsys, "big.vot")
	if err != nil {
		t.Fatal(err)
	}
	names, _ := attrs["field_names"].([]string)
	if int64(len(names)) > votableMaxFields {
		t.Errorf("field_names exceeded cap: %d > %d", len(names), votableMaxFields)
	}
}
