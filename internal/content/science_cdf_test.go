package content

import (
	"context"
	"encoding/binary"
	"testing"
	"testing/fstest"
)

// buildCDFFile synthesises a minimal but spec-conformant CDF v3 file
// — 4-byte magic, 312-byte CDR, then a GDR at offset 316. The GDR
// fields surface as variable_count + attribute_count.
//
// For our parser we only need ~52 bytes of CDR fields populated; the
// rest is zero-padded.
func buildCDFFile(version, release, increment int32, encoding int32, flags int32, nrVars, nzVars, numAttr int32) []byte {
	// CDR is 312 bytes total (56 header + 256 copyright). GDR follows.
	const cdrSize = 312
	const gdrSize = 84
	// File layout: magic(4) + CDR(312) + GDR(84) = 400 bytes total.
	buf := make([]byte, 4+cdrSize+gdrSize)

	// Magic
	binary.BigEndian.PutUint32(buf[0:4], cdfMagicV3)

	// CDR starts at byte 4.
	cdr := buf[4:]
	binary.BigEndian.PutUint64(cdr[0:8], cdrSize)               // RecordSize
	binary.BigEndian.PutUint32(cdr[8:12], cdfRecordTypeCDR)     // RecordType = 1
	binary.BigEndian.PutUint64(cdr[12:20], uint64(4+cdrSize))   // GDROffset (right after CDR)
	binary.BigEndian.PutUint32(cdr[20:24], uint32(version))
	binary.BigEndian.PutUint32(cdr[24:28], uint32(release))
	binary.BigEndian.PutUint32(cdr[28:32], uint32(encoding))
	binary.BigEndian.PutUint32(cdr[32:36], uint32(flags))
	binary.BigEndian.PutUint32(cdr[44:48], uint32(increment))

	// GDR starts at byte 4+cdrSize = 316.
	gdr := buf[4+cdrSize:]
	binary.BigEndian.PutUint64(gdr[0:8], gdrSize)
	binary.BigEndian.PutUint32(gdr[8:12], cdfRecordTypeGDR)
	binary.BigEndian.PutUint32(gdr[44:48], uint32(nrVars))
	binary.BigEndian.PutUint32(gdr[48:52], uint32(numAttr))
	binary.BigEndian.PutUint32(gdr[60:64], uint32(nzVars))

	return buf
}

func TestCDF_FullDetectAndAttrs(t *testing.T) {
	body := buildCDFFile(3, 8, 0, 1, cdfFlagMajorityRow, 5, 10, 3)
	fsys := fstest.MapFS{"data.cdf": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "data.cdf")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "science/cdf" {
		t.Fatalf("got %s, want science/cdf", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "data.cdf")
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{
		"science_format":  "cdf",
		"cdf_version":     "3.8.0",
		"cdf_encoding":    "network",
		"cdf_majority":    "row",
		"variable_count":  int64(15), // 5 rVars + 10 zVars
		"attribute_count": int64(3),
	}
	for k, want := range wants {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestCDF_ColumnMajority(t *testing.T) {
	// Flags = 0 (no row-majority bit) → cdf_majority = "column"
	body := buildCDFFile(3, 0, 0, 6, 0, 0, 0, 0)
	fsys := fstest.MapFS{"x.cdf": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.cdf").Attributes(context.Background(), fsys, "x.cdf")
	if got := attrs["cdf_majority"]; got != "column" {
		t.Errorf("cdf_majority = %v, want column", got)
	}
	if got := attrs["cdf_encoding"]; got != "ibmpc" {
		t.Errorf("cdf_encoding = %v, want ibmpc", got)
	}
}

func TestCDF_DetectByMagicWithoutExtension(t *testing.T) {
	// File named .dat — magic-byte detection still fires.
	body := buildCDFFile(3, 8, 0, 1, cdfFlagMajorityRow, 1, 0, 1)
	fsys := fstest.MapFS{"obs.dat": {Data: body}}
	ct := DefaultRegistry().Detect(fsys, "obs.dat")
	if ct == nil {
		t.Fatal("magic-byte detection failed")
	}
	if ct.Name() != "science/cdf" {
		t.Errorf("got %s, want science/cdf", ct.Name())
	}
}

func TestCDF_UnknownEncodingOmitted(t *testing.T) {
	// Encoding = 99 isn't in our table — cdf_encoding should be
	// absent (zero default surfaces via the activation, but we
	// don't set it in attrs at all).
	body := buildCDFFile(3, 0, 0, 99, 0, 0, 0, 0)
	fsys := fstest.MapFS{"x.cdf": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.cdf").Attributes(context.Background(), fsys, "x.cdf")
	if _, present := attrs["cdf_encoding"]; present {
		t.Errorf("unknown encoding should not populate cdf_encoding, got %v", attrs["cdf_encoding"])
	}
}

func TestCDF_TruncatedAfterMagicSurfacesCoarseVersion(t *testing.T) {
	// Only the 4-byte magic — too short to read CDR fields. Detection
	// succeeds; cdf_version surfaces as "3.x" sentinel.
	body := []byte{0xCD, 0xF3, 0x00, 0x01}
	fsys := fstest.MapFS{"t.cdf": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "t.cdf").Attributes(context.Background(), fsys, "t.cdf")
	if got := attrs["cdf_version"]; got != "3.x" {
		t.Errorf("cdf_version = %v, want '3.x' sentinel", got)
	}
	if _, present := attrs["variable_count"]; present {
		t.Errorf("variable_count should be absent on truncated file")
	}
}

func TestCDF_BadMagicReturnsEmpty(t *testing.T) {
	// Magic bytes wrong — empty attrs (detection by extension still
	// fires, but Attributes returns empty).
	body := make([]byte, 64)
	copy(body, []byte("NOT-CDF!"))
	fsys := fstest.MapFS{"x.cdf": {Data: body}}
	attrs, err := DefaultRegistry().Detect(fsys, "x.cdf").Attributes(context.Background(), fsys, "x.cdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 0 {
		t.Errorf("bad-magic CDF produced attrs: %v", attrs)
	}
}

func TestCDF_GDROutOfReadWindow(t *testing.T) {
	// Synthesise a file whose GDROffset points BEYOND the read cap.
	// The parser surfaces CDR fields but not variable_count.
	const farOffset = cdfReadCap + 1024
	body := make([]byte, farOffset+84)
	binary.BigEndian.PutUint32(body[0:4], cdfMagicV3)
	cdr := body[4:]
	binary.BigEndian.PutUint64(cdr[0:8], 312)
	binary.BigEndian.PutUint32(cdr[8:12], cdfRecordTypeCDR)
	binary.BigEndian.PutUint64(cdr[12:20], farOffset)
	binary.BigEndian.PutUint32(cdr[20:24], 3) // version
	binary.BigEndian.PutUint32(cdr[24:28], 8) // release
	binary.BigEndian.PutUint32(cdr[28:32], 1) // encoding = network

	// Plant a valid GDR at farOffset so a Seek-based read could find
	// it (testing/fstest.MapFS does support Seek on its files).
	gdr := body[farOffset:]
	binary.BigEndian.PutUint64(gdr[0:8], 84)
	binary.BigEndian.PutUint32(gdr[8:12], cdfRecordTypeGDR)
	binary.BigEndian.PutUint32(gdr[44:48], 7)  // NrVars
	binary.BigEndian.PutUint32(gdr[48:52], 11) // NumAttr
	binary.BigEndian.PutUint32(gdr[60:64], 4)  // NzVars

	fsys := fstest.MapFS{"big.cdf": {Data: body}}
	attrs, err := DefaultRegistry().Detect(fsys, "big.cdf").Attributes(context.Background(), fsys, "big.cdf")
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs["cdf_version"]; got != "3.8.0" {
		t.Errorf("cdf_version = %v, want 3.8.0", got)
	}
	// MapFS files DO implement io.Seeker, so the fallback succeeds
	// and we should see variable_count populated.
	if got := attrs["variable_count"]; got != int64(11) {
		t.Errorf("variable_count = %v, want 11 (7+4) via seek fallback", got)
	}
}

func TestCDF_RecordTypeMismatchSurfacesCoarseVersion(t *testing.T) {
	body := buildCDFFile(3, 8, 0, 1, 0, 0, 0, 0)
	// Corrupt the CDR RecordType (offset 12).
	binary.BigEndian.PutUint32(body[12:16], 99)
	fsys := fstest.MapFS{"x.cdf": {Data: body}}
	attrs, _ := DefaultRegistry().Detect(fsys, "x.cdf").Attributes(context.Background(), fsys, "x.cdf")
	if got := attrs["cdf_version"]; got != "3.x" {
		t.Errorf("cdf_version = %v, want '3.x' sentinel on RecordType mismatch", got)
	}
}
