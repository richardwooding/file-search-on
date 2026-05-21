package content

import (
	"context"
	"testing"
	"testing/fstest"
	"time"
)

// minimalPDS3 is the smallest PDS3 PVL label that should detect and
// parse — version + a handful of metadata keywords + END.
const minimalPDS3 = `PDS_VERSION_ID                = PDS3
RECORD_TYPE                   = FIXED_LENGTH
/* This is a comment that should be stripped */
MISSION_NAME                  = "MARS RECONNAISSANCE ORBITER"
SPACECRAFT_NAME               = "MARS RECONNAISSANCE ORBITER"
INSTRUMENT_NAME               = "HIGH RESOLUTION IMAGING SCIENCE EXPERIMENT"
TARGET_NAME                   = MARS
PRODUCT_ID                    = "PSP_007146_1755_RED"
START_TIME                    = 2024-01-15T08:30:00.000

OBJECT                        = IMAGE
  LINES                       = 9472
  LINE_SAMPLES                = 1024
END_OBJECT                    = IMAGE

END
`

func TestPDS3_MinimalDetectAndAttrs(t *testing.T) {
	fsys := fstest.MapFS{"data.lbl": {Data: []byte(minimalPDS3)}}
	ct := DefaultRegistry().Detect(fsys, "data.lbl")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/pds3" {
		t.Fatalf("got %s, want science/pds3", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "data.lbl")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"science_format":  "pds3",
		"pds_version":     "PDS3",
		"mission_name":    "MARS RECONNAISSANCE ORBITER",
		"spacecraft_name": "MARS RECONNAISSANCE ORBITER",
		"instrument_name": "HIGH RESOLUTION IMAGING SCIENCE EXPERIMENT",
		"target_name":     "MARS",
		"product_id":      "PSP_007146_1755_RED",
		"start_time":      "2024-01-15T08:30:00.000",
		"title":           "HIGH RESOLUTION IMAGING SCIENCE EXPERIMENT MARS",
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	taken, ok := attrs["taken_at"].(time.Time)
	if !ok {
		t.Fatalf("taken_at missing: %T", attrs["taken_at"])
	}
	want := time.Date(2024, 1, 15, 8, 30, 0, 0, time.UTC)
	if !taken.Equal(want) {
		t.Errorf("taken_at = %v, want %v", taken, want)
	}
}

func TestPDS3_DetectByMagic(t *testing.T) {
	// File named with no recognised extension — still detects via
	// the PDS_VERSION_ID magic prefix.
	fsys := fstest.MapFS{"unnamed": {Data: []byte(minimalPDS3)}}
	ct := DefaultRegistry().Detect(fsys, "unnamed")
	if ct == nil {
		t.Fatal("magic-based detection failed")
	}
	if ct.Name() != "science/pds3" {
		t.Errorf("got %s, want science/pds3", ct.Name())
	}
}

func TestPDS3_EmptyParseReturnsEmpty(t *testing.T) {
	// File with magic prefix but no parseable content — empty attrs.
	body := "PDS_VERSION_ID\n\n\n"
	fsys := fstest.MapFS{"x.lbl": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.lbl").Attributes(context.Background(), fsys, "x.lbl")
	if len(attrs) != 0 {
		t.Errorf("unparseable PDS3 returned attrs: %v", attrs)
	}
}

const minimalPDS4 = `<?xml version="1.0" encoding="UTF-8"?>
<Product_Observational xmlns="http://pds.nasa.gov/pds4/pds/v1">
  <Identification_Area>
    <logical_identifier>urn:nasa:pds:perseverance.mast_cam:data_raw:abc123</logical_identifier>
    <version_id>1.0</version_id>
    <title>Mastcam-Z observation of Jezero crater</title>
  </Identification_Area>
  <Observation_Area>
    <Time_Coordinates>
      <start_date_time>2025-04-15T08:30:00Z</start_date_time>
      <stop_date_time>2025-04-15T08:35:00Z</stop_date_time>
    </Time_Coordinates>
    <Investigation_Area>
      <name>Mars 2020 Perseverance Rover</name>
      <type>Mission</type>
    </Investigation_Area>
    <Observing_System>
      <Observing_System_Component>
        <name>Perseverance Rover</name>
        <type>Host</type>
      </Observing_System_Component>
      <Observing_System_Component>
        <name>Mastcam-Z</name>
        <type>Instrument</type>
      </Observing_System_Component>
    </Observing_System>
    <Target_Identification>
      <name>Mars</name>
      <type>Planet</type>
    </Target_Identification>
  </Observation_Area>
</Product_Observational>`

func TestPDS4_MinimalDetectAndAttrs(t *testing.T) {
	fsys := fstest.MapFS{"obs.lblx": {Data: []byte(minimalPDS4)}}
	ct := DefaultRegistry().Detect(fsys, "obs.lblx")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/pds4" {
		t.Fatalf("got %s, want science/pds4", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "obs.lblx")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"science_format":  "pds4",
		"pds_version":     "PDS4",
		"mission_name":    "Mars 2020 Perseverance Rover",
		"spacecraft_name": "Perseverance Rover",
		"instrument_name": "Mastcam-Z",
		"target_name":     "Mars",
		"product_id":      "urn:nasa:pds:perseverance.mast_cam:data_raw:abc123",
		"start_time":      "2025-04-15T08:30:00Z",
		"title":           "Mastcam-Z observation of Jezero crater",
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	taken, ok := attrs["taken_at"].(time.Time)
	if !ok {
		t.Fatalf("taken_at missing: %T", attrs["taken_at"])
	}
	want := time.Date(2025, 4, 15, 8, 30, 0, 0, time.UTC)
	if !taken.Equal(want) {
		t.Errorf("taken_at = %v, want %v", taken, want)
	}
}

func TestPDS4_WrongNamespaceReturnsEmpty(t *testing.T) {
	body := `<?xml version="1.0"?>
<Product_Observational xmlns="http://example.com/not-pds">
  <Identification_Area>
    <title>not real PDS</title>
  </Identification_Area>
</Product_Observational>`
	fsys := fstest.MapFS{"bad.lblx": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "bad.lblx").Attributes(context.Background(), fsys, "bad.lblx")
	if len(attrs) != 0 {
		t.Errorf("non-PDS namespace produced attrs: %v", attrs)
	}
}

func TestPDS4_NonProductObservationalReturnsEmpty(t *testing.T) {
	// Product_Bundle / Product_Collection / Product_Document have
	// different observation-area shapes; v1 only supports
	// Product_Observational.
	body := `<?xml version="1.0"?>
<Product_Bundle xmlns="http://pds.nasa.gov/pds4/pds/v1">
  <Identification_Area>
    <title>A bundle</title>
  </Identification_Area>
</Product_Bundle>`
	fsys := fstest.MapFS{"bundle.lblx": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "bundle.lblx").Attributes(context.Background(), fsys, "bundle.lblx")
	if len(attrs) != 0 {
		t.Errorf("non-Product_Observational produced attrs: %v", attrs)
	}
}

func TestPDS4_MultipleInstrumentsTakesFirst(t *testing.T) {
	body := `<?xml version="1.0"?>
<Product_Observational xmlns="http://pds.nasa.gov/pds4/pds/v1">
  <Identification_Area><title>multi</title></Identification_Area>
  <Observation_Area>
    <Observing_System>
      <Observing_System_Component>
        <name>InstrumentA</name>
        <type>Instrument</type>
      </Observing_System_Component>
      <Observing_System_Component>
        <name>InstrumentB</name>
        <type>Instrument</type>
      </Observing_System_Component>
    </Observing_System>
  </Observation_Area>
</Product_Observational>`
	fsys := fstest.MapFS{"x.lblx": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.lblx").Attributes(context.Background(), fsys, "x.lblx")
	if got := attrs["instrument_name"]; got != "InstrumentA" {
		t.Errorf("instrument_name = %v, want InstrumentA (first instrument)", got)
	}
}

func TestPDS3_Truncated(t *testing.T) {
	body := minimalPDS3[:80]
	fsys := fstest.MapFS{"t.lbl": {Data: []byte(body)}}
	if _, err := DefaultRegistry().Detect(fsys, "t.lbl").Attributes(context.Background(), fsys, "t.lbl"); err != nil {
		t.Errorf("truncated PDS3 errored: %v", err)
	}
}

func TestPDS4_TruncatedNoCrash(t *testing.T) {
	body := minimalPDS4[:200]
	fsys := fstest.MapFS{"t.lblx": {Data: []byte(body)}}
	if _, err := DefaultRegistry().Detect(fsys, "t.lblx").Attributes(context.Background(), fsys, "t.lblx"); err != nil {
		t.Errorf("truncated PDS4 errored: %v", err)
	}
}

func TestPDS3_CommentsStripped(t *testing.T) {
	// /* ... */ inside a value should NOT be stripped if quoted; only
	// outside quotes. (Current parser strips all /* */; this test
	// documents that behaviour and pins it.)
	body := `PDS_VERSION_ID = PDS3
/* A comment with = sign inside */
TARGET_NAME = MARS
END
`
	fsys := fstest.MapFS{"c.lbl": {Data: []byte(body)}}
	attrs, _ := DefaultRegistry().Detect(fsys, "c.lbl").Attributes(context.Background(), fsys, "c.lbl")
	if got := attrs["target_name"]; got != "MARS" {
		t.Errorf("target_name = %v, want MARS (comment with = should be stripped)", got)
	}
}
