package content

import (
	"context"
	"fmt"
	"testing"
	"testing/fstest"
	"time"
)

// buildCard emits a single 80-byte FITS card. Numeric / boolean
// values right-pad with spaces; string values get single-quoted and
// blank-padded inside the quotes to at least 8 chars (FITS Standard
// §4.2.1.1). A `nil` value emits a value-less card (END / blank).
func buildCard(key string, value any) []byte {
	card := make([]byte, fitsCardSize)
	for i := range card {
		card[i] = ' '
	}
	// Keyword left-justified in cols 1-8.
	copy(card[0:8], key)
	if value == nil {
		// END card or blank — no `=` indicator.
		return card
	}
	card[8] = '='
	card[9] = ' '
	var s string
	switch v := value.(type) {
	case string:
		// FITS string values are single-quoted and blank-padded
		// inside the quotes to a minimum of 8 chars.
		padded := v
		for len(padded) < 8 {
			padded += " "
		}
		s = "'" + padded + "'"
	case bool:
		if v {
			s = "T"
		} else {
			s = "F"
		}
	case int:
		s = fmt.Sprintf("%d", v)
	case int64:
		s = fmt.Sprintf("%d", v)
	case float64:
		s = fmt.Sprintf("%g", v)
	default:
		s = fmt.Sprintf("%v", v)
	}
	// Right-justify numeric / boolean to col 30; string starts at
	// col 11 (immediately after `= `).
	if _, isStr := value.(string); isStr {
		copy(card[10:], s)
	} else {
		// Pad to col 30 (byte 29) so the value ends there.
		start := max(30-len(s), 10)
		copy(card[start:], s)
	}
	return card
}

// buildFITSHeader assembles a series of cards into a complete header,
// appending an END card and padding with blank cards to a 2880-byte
// block boundary.
func buildFITSHeader(cards [][]byte) []byte {
	var buf []byte
	for _, c := range cards {
		buf = append(buf, c...)
	}
	buf = append(buf, buildCard("END", nil)...)
	// Pad with blank cards to block boundary.
	for len(buf)%fitsBlockSize != 0 {
		buf = append(buf, buildCard("", nil)...)
	}
	return buf
}

func TestFITS_MinimalPrimary(t *testing.T) {
	body := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 8),
		buildCard("NAXIS", 0),
	})
	fsys := fstest.MapFS{"obs.fits": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "obs.fits")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/fits" {
		t.Fatalf("want science/fits, got %s", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "obs.fits")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["science_format"]; got != "fits" {
		t.Errorf("science_format = %v, want fits", got)
	}
	if got := attrs["bitpix"]; got != int64(8) {
		t.Errorf("bitpix = %v, want 8", got)
	}
	// NAXIS=0 emits the zero-default via the activation layer; the
	// parser doesn't set the key, so absent is correct here.
	if _, ok := attrs["naxis"]; ok {
		t.Errorf("naxis should be absent (zero value), got present")
	}
	if got := attrs["fits_kind"]; got != "primary" {
		t.Errorf("fits_kind = %v, want primary", got)
	}
	if got := attrs["hdu_count"]; got != int64(1) {
		t.Errorf("hdu_count = %v, want 1", got)
	}
}

func TestFITS_ImageHeaderPromotions(t *testing.T) {
	body := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", -32),
		buildCard("NAXIS", 2),
		buildCard("NAXIS1", 512),
		buildCard("NAXIS2", 512),
		buildCard("OBJECT", "M31"),
		buildCard("TELESCOP", "HST"),
		buildCard("INSTRUME", "WFC3"),
		buildCard("DATE-OBS", "2025-01-15T03:14:00"),
		buildCard("EXPTIME", 600.0),
		buildCard("FILTER", "F814W"),
	})
	// Pad past the data unit so the HDU walker can probe but find
	// nothing — the read still terminates cleanly.
	dataBytes := 4 * 512 * 512 // |BITPIX|/8 * NAXIS1 * NAXIS2
	pad := dataBytes + (fitsBlockSize - dataBytes%fitsBlockSize)
	body = append(body, make([]byte, pad)...)

	fsys := fstest.MapFS{"obs.fits": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "obs.fits")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "obs.fits")
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]any{
		"telescope":  "HST",
		"instrument": "WFC3",
		"object":     "M31",
		"title":      "M31", // promoted
		"date_obs":   "2025-01-15T03:14:00",
		"exptime":    float64(600),
		"filter":     "F814W",
		"bitpix":     int64(-32),
		"naxis":      int64(2),
		"naxis1":     int64(512),
		"naxis2":     int64(512),
		"fits_kind":  "image",
	}
	for k, want := range tests {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	// taken_at parses from DATE-OBS.
	taken, ok := attrs["taken_at"].(time.Time)
	if !ok {
		t.Fatalf("taken_at missing or wrong type: %T", attrs["taken_at"])
	}
	want := time.Date(2025, 1, 15, 3, 14, 0, 0, time.UTC)
	if !taken.Equal(want) {
		t.Errorf("taken_at = %v, want %v", taken, want)
	}
	// OBSERVER absent → author not surfaced.
	if got := attrs["author"]; got != nil {
		t.Errorf("author should be absent, got %v", got)
	}
}

func TestFITS_MultiHDU(t *testing.T) {
	// Primary HDU with no data unit (NAXIS=0).
	primary := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 8),
		buildCard("NAXIS", 0),
	})
	// Two image extensions, each with a small 4×4 byte data array
	// (16 bytes → padded to one 2880-byte block).
	makeExtension := func() []byte {
		header := buildFITSHeader([][]byte{
			buildCard("XTENSION", "IMAGE"),
			buildCard("BITPIX", 8),
			buildCard("NAXIS", 2),
			buildCard("NAXIS1", 4),
			buildCard("NAXIS2", 4),
		})
		header = append(header, make([]byte, fitsBlockSize)...)
		return header
	}
	body := append(primary, makeExtension()...)
	body = append(body, makeExtension()...)

	fsys := fstest.MapFS{"mef.fits": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "mef.fits")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "mef.fits")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["hdu_count"]; got != int64(3) {
		t.Errorf("hdu_count = %v, want 3", got)
	}
}

func TestFITS_Truncated(t *testing.T) {
	// 1000 bytes — less than one full block. Must not panic.
	body := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 16),
		buildCard("NAXIS", 1),
		buildCard("NAXIS1", 100),
	})
	body = body[:1000]
	fsys := fstest.MapFS{"trunc.fits": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "trunc.fits")
	if ct == nil {
		t.Fatal("Detect returned nil (truncated file should still detect by magic)")
	}
	if _, err := ct.Attributes(context.Background(), fsys, "trunc.fits"); err != nil {
		t.Fatalf("Attributes errored on truncated input: %v", err)
	}
}

func TestFITS_DetectByMagic(t *testing.T) {
	// File with no recognised extension — should still detect via
	// the `SIMPLE  =` magic prefix.
	body := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 8),
		buildCard("NAXIS", 0),
	})
	fsys := fstest.MapFS{"obs.dat": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "obs.dat")
	if ct == nil {
		t.Fatal("magic-byte detection failed for FITS without recognised extension")
	}
	if ct.Name() != "science/fits" {
		t.Errorf("got %s, want science/fits", ct.Name())
	}
}

func TestFITS_QuotedStringWithSlash(t *testing.T) {
	// Slashes inside quoted strings must not be treated as comment
	// delimiters. `stripFITSComment` walks the quote state for this.
	body := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 8),
		buildCard("NAXIS", 0),
		buildCard("OBJECT", "Path/To/Thing"),
	})
	fsys := fstest.MapFS{"obs.fits": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "obs.fits")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "obs.fits")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["object"]; got != "Path/To/Thing" {
		t.Errorf("object = %q, want %q", got, "Path/To/Thing")
	}
}
