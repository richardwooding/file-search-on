package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf16"

	"github.com/andybalholm/brotli"
)

// buildSFNT writes a valid sfnt header + table directory wrapping the
// given table bodies. Table tags are 4-char ASCII; bodies are arbitrary
// bytes. The table directory's checksum and pad-to-4 invariants are
// honoured per the OpenType spec so the parser sees a realistic shape.
//
// All test fixtures are synthesised at runtime — no binary blobs are
// committed to the repo. magic should be sfntMagicTrueType (TTF) or
// sfntMagicOpenType (OTF).
func buildSFNT(magic []byte, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags) // deterministic order; sfnt spec doesn't strictly require sort but agents expect it

	numTables := len(tags)
	dirSize := 12 + numTables*16
	// Pad each table to a 4-byte boundary, accumulate offsets.
	offsets := make(map[string]uint32, numTables)
	lengths := make(map[string]uint32, numTables)
	bodyOffset := uint32(dirSize)
	for _, t := range tags {
		offsets[t] = bodyOffset
		lengths[t] = uint32(len(tables[t]))
		// Pad to 4.
		padded := (uint32(len(tables[t])) + 3) &^ 3
		bodyOffset += padded
	}

	out := make([]byte, bodyOffset)
	// sfnt header.
	copy(out[0:4], magic)
	binary.BigEndian.PutUint16(out[4:6], uint16(numTables))
	// searchRange / entrySelector / rangeShift can be left zero —
	// parser doesn't read them.

	// Table directory entries.
	for i, t := range tags {
		off := 12 + i*16
		copy(out[off:off+4], []byte(t))
		// checksum (4 bytes) — leave zero; parser doesn't verify
		binary.BigEndian.PutUint32(out[off+8:off+12], offsets[t])
		binary.BigEndian.PutUint32(out[off+12:off+16], lengths[t])
	}

	// Table bodies.
	for _, t := range tags {
		copy(out[offsets[t]:offsets[t]+lengths[t]], tables[t])
	}
	return out
}

// buildName synthesises a `name` table with the given records. Each
// record's nameID + platformID/encodingID/languageID + string is
// stored; strings are UTF-16BE for platform 3, raw bytes for platform 1.
func buildName(records []testNameRecord) []byte {
	// Header: format (u16) + count (u16) + stringOffset (u16).
	// Then count × 12-byte records, then the string heap.
	count := len(records)
	headerEnd := 6 + count*12
	heap := []byte{}
	type recOff struct{ offset, length uint16 }
	offsets := make([]recOff, count)
	for i, r := range records {
		var encoded []byte
		if r.platformID == 3 {
			// UTF-16BE.
			u16 := utf16.Encode([]rune(r.value))
			encoded = make([]byte, len(u16)*2)
			for j, u := range u16 {
				binary.BigEndian.PutUint16(encoded[j*2:j*2+2], u)
			}
		} else {
			encoded = []byte(r.value)
		}
		offsets[i] = recOff{offset: uint16(len(heap)), length: uint16(len(encoded))}
		heap = append(heap, encoded...)
	}

	out := make([]byte, headerEnd+len(heap))
	binary.BigEndian.PutUint16(out[0:2], 0) // format 0
	binary.BigEndian.PutUint16(out[2:4], uint16(count))
	binary.BigEndian.PutUint16(out[4:6], uint16(headerEnd))
	for i, r := range records {
		off := 6 + i*12
		binary.BigEndian.PutUint16(out[off+0:off+2], r.platformID)
		binary.BigEndian.PutUint16(out[off+2:off+4], r.encodingID)
		binary.BigEndian.PutUint16(out[off+4:off+6], r.languageID)
		binary.BigEndian.PutUint16(out[off+6:off+8], r.nameID)
		binary.BigEndian.PutUint16(out[off+8:off+10], offsets[i].length)
		binary.BigEndian.PutUint16(out[off+10:off+12], offsets[i].offset)
	}
	copy(out[headerEnd:], heap)
	return out
}

type testNameRecord struct {
	platformID, encodingID, languageID, nameID uint16
	value                                      string
}

// buildOS2 writes a minimal v0 OS/2 table — first 78 bytes — covering
// the fields parseOS2Table reads.
func buildOS2(weight, width uint16, fsType uint16, panose [10]byte, ranges [4]uint32) []byte {
	out := make([]byte, 78)
	// version (2) + xAvgCharWidth (2) at 0,2 — leave zero.
	binary.BigEndian.PutUint16(out[4:6], weight)
	binary.BigEndian.PutUint16(out[6:8], width)
	binary.BigEndian.PutUint16(out[8:10], fsType)
	// y* / sub / super / strikeout — leave zero (10..31)
	copy(out[32:42], panose[:])
	binary.BigEndian.PutUint32(out[42:46], ranges[0])
	binary.BigEndian.PutUint32(out[46:50], ranges[1])
	binary.BigEndian.PutUint32(out[50:54], ranges[2])
	binary.BigEndian.PutUint32(out[54:58], ranges[3])
	return out
}

// buildHead writes a minimal `head` table.
func buildHead(revision uint32, unitsPerEm uint16, macStyle uint16) []byte {
	out := make([]byte, 54)
	// version (4) leave zero.
	binary.BigEndian.PutUint32(out[4:8], revision)
	binary.BigEndian.PutUint16(out[18:20], unitsPerEm)
	binary.BigEndian.PutUint16(out[44:46], macStyle)
	return out
}

// buildPost writes a minimal `post` table.
func buildPost(italicAngle uint32, fixedPitch uint32) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint32(out[4:8], italicAngle)
	binary.BigEndian.PutUint32(out[12:16], fixedPitch)
	return out
}

// buildMaxp writes a minimal `maxp` table.
func buildMaxp(numGlyphs uint16) []byte {
	out := make([]byte, 6)
	// version (4) leave zero (v0.5).
	binary.BigEndian.PutUint16(out[4:6], numGlyphs)
	return out
}

// buildFvar writes a minimal `fvar` table with the given axis tags.
// Each axis gets default min/default/max values + null flags + nameID 0.
func buildFvar(axes []string) []byte {
	axisCount := len(axes)
	axisSize := 20 // VariationAxisRecord
	header := 16
	out := make([]byte, header+axisCount*axisSize)
	// version (4) = 1.0 fixed
	binary.BigEndian.PutUint16(out[0:2], 1)
	// axesArrayOffset (2)
	binary.BigEndian.PutUint16(out[4:6], uint16(header))
	// reserved (2) leave zero
	binary.BigEndian.PutUint16(out[8:10], uint16(axisCount))
	binary.BigEndian.PutUint16(out[10:12], uint16(axisSize))
	// instanceCount (2), instanceSize (2) leave zero — we don't read them
	for i, tag := range axes {
		off := header + i*axisSize
		copy(out[off:off+4], []byte(tag))
		// minValue/defaultValue/maxValue at off+4 / off+8 / off+12 — leave zero
	}
	return out
}

func TestParseSFNT_FullHappyPath(t *testing.T) {
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{
			{3, 1, 0x409, nameIDFamily, "Inter"},
			{3, 1, 0x409, nameIDSubfamily, "Bold"},
			{3, 1, 0x409, nameIDDesigner, "Rasmus Andersson"},
			{3, 1, 0x409, nameIDVersion, "Version 3.19"},
			{3, 1, 0x409, nameIDLicenseDescription, "SIL Open Font License 1.1"},
		}),
		"OS/2": buildOS2(700, 5, 0, [10]byte{}, [4]uint32{0x1, 0, 0, 0}), // weight=Bold, width=Medium, Basic Latin
		"head": buildHead(0x00030000, 1000, macStyleBold), // revision 3.0, 1000 upem, bold bit
		"post": buildPost(0, 0),
		"maxp": buildMaxp(2500),
		"glyf": []byte{0x00},
	})

	info := parseSFNT(data, 0)
	if !info.Present {
		t.Fatal("expected Present=true")
	}
	if info.FamilyName != "Inter" {
		t.Errorf("FamilyName = %q", info.FamilyName)
	}
	if info.SubfamilyName != "Bold" {
		t.Errorf("SubfamilyName = %q", info.SubfamilyName)
	}
	if info.Designer != "Rasmus Andersson" {
		t.Errorf("Designer = %q", info.Designer)
	}
	if info.Version != "Version 3.19" {
		t.Errorf("Version = %q", info.Version)
	}
	if info.License != "SIL Open Font License 1.1" {
		t.Errorf("License = %q", info.License)
	}
	if info.Weight != 700 {
		t.Errorf("Weight = %d, want 700", info.Weight)
	}
	if info.Width != 5 {
		t.Errorf("Width = %d, want 5", info.Width)
	}
	if info.Embedding != "installable" {
		t.Errorf("Embedding = %q", info.Embedding)
	}
	if info.UnitsPerEm != 1000 {
		t.Errorf("UnitsPerEm = %d", info.UnitsPerEm)
	}
	if info.Revision != 3.0 {
		t.Errorf("Revision = %v", info.Revision)
	}
	if info.GlyphCount != 2500 {
		t.Errorf("GlyphCount = %d", info.GlyphCount)
	}
	if info.OutlineKind != "truetype" {
		t.Errorf("OutlineKind = %q", info.OutlineKind)
	}
	if !info.IsBold {
		t.Error("IsBold should be true")
	}
	// macStyle bold bit set — should appear in MacStyle list.
	if len(info.MacStyle) == 0 || info.MacStyle[0] != "bold" {
		t.Errorf("MacStyle = %v, want [bold]", info.MacStyle)
	}
	// Basic Latin unicode range bit.
	if len(info.UnicodeRanges) == 0 || info.UnicodeRanges[0] != "Basic Latin" {
		t.Errorf("UnicodeRanges = %v, want [Basic Latin]", info.UnicodeRanges)
	}
}

func TestParseSFNT_OutlineKindCFF(t *testing.T) {
	data := buildSFNT(sfntMagicOpenType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Adobe Garamond"}}),
		"CFF ": []byte{0x00},
	})
	info := parseSFNT(data, 0)
	if info.OutlineKind != "cff" {
		t.Errorf("OutlineKind = %q, want cff", info.OutlineKind)
	}
}

func TestParseSFNT_OutlineKindCFF2(t *testing.T) {
	data := buildSFNT(sfntMagicOpenType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Source Sans Variable"}}),
		"CFF2": []byte{0x00},
		"fvar": buildFvar([]string{"wght", "ital"}),
	})
	info := parseSFNT(data, 0)
	if info.OutlineKind != "cff2" {
		t.Errorf("OutlineKind = %q", info.OutlineKind)
	}
	if info.AxisCount != 2 {
		t.Errorf("AxisCount = %d", info.AxisCount)
	}
	if len(info.Axes) != 2 || info.Axes[0] != "wght" || info.Axes[1] != "ital" {
		t.Errorf("Axes = %v", info.Axes)
	}
}

func TestParseSFNT_ColorFont(t *testing.T) {
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Apple Color Emoji"}}),
		"glyf": []byte{0x00},
		"sbix": []byte{0x00},
	})
	info := parseSFNT(data, 0)
	if !info.IsColorFont {
		t.Error("IsColorFont should be true (sbix table present)")
	}
}

func TestParseSFNT_MonospaceAndItalic(t *testing.T) {
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "JetBrains Mono Italic"}}),
		"post": buildPost(0xFFEC0000, 1), // -20.0 italic angle, isFixedPitch=1
		"glyf": []byte{0x00},
	})
	info := parseSFNT(data, 0)
	if !info.IsMonospace {
		t.Error("IsMonospace should be true")
	}
	if info.ItalicAngle != -20.0 {
		t.Errorf("ItalicAngle = %v", info.ItalicAngle)
	}
	if !info.IsItalic {
		t.Error("IsItalic should be true (non-zero italicAngle)")
	}
}

func TestParseSFNT_NameTableEncodingPriority(t *testing.T) {
	// Multiple records for nameID 1: Mac Roman + Windows Unicode EN.
	// Expect Windows Unicode EN to win.
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{
			{1, 0, 0, nameIDFamily, "Mac-Roman-Family"},
			{3, 1, 0x409, nameIDFamily, "Win-Unicode-EN-Family"},
		}),
	})
	info := parseSFNT(data, 0)
	if info.FamilyName != "Win-Unicode-EN-Family" {
		t.Errorf("FamilyName = %q, want Win-Unicode-EN-Family (Windows Unicode English wins priority)", info.FamilyName)
	}
}

func TestParseSFNT_NameTableMacRomanFallback(t *testing.T) {
	// Only a Mac Roman record present — should be picked up via the
	// platform-1 fallback.
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{
			{1, 0, 0, nameIDFamily, "Mac-Roman-Family"},
		}),
	})
	info := parseSFNT(data, 0)
	if info.FamilyName != "Mac-Roman-Family" {
		t.Errorf("FamilyName = %q", info.FamilyName)
	}
}

func TestParseSFNT_MagicMismatch(t *testing.T) {
	info := parseSFNT(bytes.Repeat([]byte{0xCC}, 256), 0)
	if info.Present {
		t.Error("Present should be false on magic mismatch")
	}
}

func TestParseSFNT_TruncatedHeader(t *testing.T) {
	info := parseSFNT([]byte{0x00, 0x01, 0x00, 0x00, 0x00}, 0)
	if info.Present {
		t.Error("Present should be false on truncated header")
	}
}

func TestParseSFNT_NumTablesCap(t *testing.T) {
	// Claim numTables = 9999 — should be rejected.
	out := make([]byte, 12)
	copy(out[0:4], sfntMagicTrueType)
	binary.BigEndian.PutUint16(out[4:6], 9999)
	info := parseSFNT(out, 0)
	if info.OutlineKind != "" || info.FamilyName != "" {
		t.Errorf("expected empty info on bogus numTables, got %+v", info)
	}
}

func TestSfntAttrs_DualSurface(t *testing.T) {
	info := sfntInfo{
		Present:    true,
		FamilyName: "Roboto",
		Designer:   "Christian Robertson",
	}
	attrs := sfntAttrs(info, "ttf")
	if attrs["font_family"] != "Roboto" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["title"] != "Roboto" {
		t.Errorf("title should be dual-surfaced from font_family, got %v", attrs["title"])
	}
	if attrs["font_designer"] != "Christian Robertson" {
		t.Errorf("font_designer = %v", attrs["font_designer"])
	}
	if attrs["author"] != "Christian Robertson" {
		t.Errorf("author should be dual-surfaced from font_designer, got %v", attrs["author"])
	}
}

func TestSfntAttrs_OmitsZeroValues(t *testing.T) {
	// Empty info should produce empty attrs (modulo Present).
	attrs := sfntAttrs(sfntInfo{Present: true}, "ttf")
	if got := attrs["font_format"]; got != "ttf" {
		t.Errorf("font_format = %v, want ttf", got)
	}
	for _, k := range []string{"font_family", "font_designer", "font_weight", "font_units_per_em"} {
		if _, ok := attrs[k]; ok {
			t.Errorf("%s should be omitted for zero-value info, got %v", k, attrs[k])
		}
	}
}

// ----------------------------------------------------------------------------
// TTC tests
// ----------------------------------------------------------------------------

// buildTTC synthesises a TTC wrapping the given member sfnts. Each
// member must be a complete sfnt (from buildSFNT).
//
// Per OpenType spec §10.4, TTC member table directory offsets MUST be
// file-absolute (relative to the start of the TTC file, not the
// start of the member sfnt). Bare sfnts from buildSFNT have member-
// relative offsets, so this helper rewrites each member's table
// directory in-place after embedding to shift the offsets by the
// member's file-absolute position.
func buildTTC(members [][]byte) []byte {
	headerLen := 12 + len(members)*4
	memberOffsets := make([]uint32, len(members))
	off := uint32(headerLen)
	for i, m := range members {
		memberOffsets[i] = off
		off += uint32(len(m))
	}
	out := make([]byte, off)
	copy(out[0:4], []byte("ttcf"))
	binary.BigEndian.PutUint32(out[4:8], 0x00010000) // v1
	binary.BigEndian.PutUint32(out[8:12], uint32(len(members)))
	for i, o := range memberOffsets {
		binary.BigEndian.PutUint32(out[12+i*4:16+i*4], o)
	}
	for i, m := range members {
		base := memberOffsets[i]
		copy(out[base:base+uint32(len(m))], m)
		// Rewrite table directory offsets to be file-absolute.
		// The member's directory starts at offset 12 within itself
		// (sfnt header is 12 bytes); each entry is 16 bytes; the
		// table-offset field is at bytes 8..12 of each entry.
		numTables := int(binary.BigEndian.Uint16(m[4:6]))
		for j := range numTables {
			entryOff := int(base) + 12 + j*16 + 8
			rel := binary.BigEndian.Uint32(out[entryOff : entryOff+4])
			binary.BigEndian.PutUint32(out[entryOff:entryOff+4], rel+base)
		}
	}
	return out
}

func TestParseTTC_TwoMembers(t *testing.T) {
	regular := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Helvetica"}}),
		"glyf": []byte{0x00},
	})
	bold := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{
			{3, 1, 0x409, nameIDFamily, "Helvetica"},
			{3, 1, 0x409, nameIDSubfamily, "Bold"},
		}),
		"glyf": []byte{0x00},
	})
	data := buildTTC([][]byte{regular, bold})

	attrs := parseTTC(data)
	if attrs["font_collection_count"] != int64(2) {
		t.Errorf("font_collection_count = %v, want 2", attrs["font_collection_count"])
	}
	if attrs["font_family"] != "Helvetica" {
		t.Errorf("font_family (primary) = %v", attrs["font_family"])
	}
	if attrs["font_format"] != "ttc" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
	families, _ := attrs["font_collection_families"].([]string)
	if len(families) != 1 || families[0] != "Helvetica" {
		t.Errorf("font_collection_families = %v, want [Helvetica]", families)
	}
}

func TestParseTTC_OTC_CFFFlavor(t *testing.T) {
	// CFF outlines → format = "otc"
	member := buildSFNT(sfntMagicOpenType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Source Sans"}}),
		"CFF ": []byte{0x00},
	})
	data := buildTTC([][]byte{member})
	attrs := parseTTC(data)
	if attrs["font_format"] != "otc" {
		t.Errorf("font_format = %v, want otc (CFF outlines)", attrs["font_format"])
	}
}

func TestParseTTC_RejectsBogusOffset(t *testing.T) {
	// Single-member TTC with member offset pointing back into the header.
	out := make([]byte, 16)
	copy(out[0:4], []byte("ttcf"))
	binary.BigEndian.PutUint32(out[4:8], 0x00010000)
	binary.BigEndian.PutUint32(out[8:12], 1)
	binary.BigEndian.PutUint32(out[12:16], 4) // offset 4 — inside the header
	attrs := parseTTC(out)
	if attrs["font_collection_count"] != int64(1) {
		t.Errorf("font_collection_count = %v, want 1 (header parsed)", attrs["font_collection_count"])
	}
	if attrs["font_family"] != nil {
		t.Errorf("font_family should be nil (member rejected for bogus offset), got %v", attrs["font_family"])
	}
}

// ----------------------------------------------------------------------------
// WOFF1 tests
// ----------------------------------------------------------------------------

// buildWOFF1 wraps a name-table body in a minimal WOFF1 container. The
// underlying flavor is TrueType (0x00010000); only the name table is
// present.
func buildWOFF1(nameTable []byte) []byte {
	numTables := 1
	dirSize := 20 * numTables
	bodyOff := uint32(woffHeaderLen + dirSize)
	bodyLen := uint32(len(nameTable))
	total := bodyOff + bodyLen

	out := make([]byte, total)
	copy(out[0:4], []byte("wOFF"))
	binary.BigEndian.PutUint32(out[4:8], 0x00010000) // flavor = TTF
	binary.BigEndian.PutUint32(out[8:12], total)
	binary.BigEndian.PutUint16(out[12:14], uint16(numTables))
	// reserved (2) at 14 — zero
	// totalSfntSize at 16 — leave zero (parser doesn't read it for v1)

	// Single entry pointing at name.
	off := woffHeaderLen
	copy(out[off:off+4], []byte("name"))
	binary.BigEndian.PutUint32(out[off+4:off+8], bodyOff)
	binary.BigEndian.PutUint32(out[off+8:off+12], bodyLen)  // compLen
	binary.BigEndian.PutUint32(out[off+12:off+16], bodyLen) // origLen (uncompressed since equal)
	// checksum (4) at off+16 — zero

	copy(out[bodyOff:bodyOff+bodyLen], nameTable)
	return out
}

func TestParseWOFF1_UncompressedNameTable(t *testing.T) {
	nameTable := buildName([]testNameRecord{
		{3, 1, 0x409, nameIDFamily, "Source Code Pro"},
		{3, 1, 0x409, nameIDDesigner, "Paul D. Hunt"},
	})
	data := buildWOFF1(nameTable)
	attrs := parseWOFF(data)
	if attrs["font_family"] != "Source Code Pro" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["font_designer"] != "Paul D. Hunt" {
		t.Errorf("font_designer = %v", attrs["font_designer"])
	}
	if attrs["font_format"] != "woff" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
}

func TestParseWOFF1_MagicMismatch(t *testing.T) {
	out := make([]byte, woffHeaderLen)
	attrs := parseWOFF(out)
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs, got %v", attrs)
	}
}

// ----------------------------------------------------------------------------
// WOFF2 tests
// ----------------------------------------------------------------------------

// woff2TableBody pairs a WOFF2 table tag with its uncompressed body.
// The buildWOFF2 helper accepts these in directory order and produces
// a valid WOFF2 file with a brotli-compressed concatenated stream of
// all bodies. Transformed tables are NOT exercised — none of file-
// search-on's metadata tables (name / OS/2 / head / post / maxp / fvar)
// are eligible for transformation.
type woff2TableBody struct {
	tag  string
	body []byte
}

// buildWOFF2 produces a minimal valid WOFF2 file. flavor is the
// underlying sfnt's scaler version ('OTTO' for CFF, 0x00010000 for
// TrueType). Tables are written in the directory order given; no
// optional metadata / private blocks are emitted.
func buildWOFF2(flavor []byte, tables []woff2TableBody) []byte {
	// Build the directory bytes. We only ever emit transformVersion = 0
	// for non-glyf/loca tables (= "as-is"), and transformVersion = 3
	// for glyf/loca (= "as-is"). Neither path emits a transformLength.
	var dir bytes.Buffer
	for _, t := range tables {
		var flags byte
		idx := knownTagIndex(t.tag)
		if t.tag == "glyf" || t.tag == "loca" {
			// transformVersion 3 = as-is for glyf/loca.
			flags = byte(3<<6) | byte(idx&0x3F)
		} else if idx >= 0 {
			flags = byte(idx & 0x3F) // transformVersion 0 = as-is for everything else
		} else {
			// Custom tag — write inline.
			flags = 0x3F
		}
		dir.WriteByte(flags)
		if idx < 0 {
			dir.WriteString(t.tag)
		}
		writeUIntBase128(&dir, uint32(len(t.body)))
	}

	// Concatenate the table bodies into the to-be-compressed payload.
	var raw bytes.Buffer
	for _, t := range tables {
		raw.Write(t.body)
	}

	// Brotli-compress the concatenated bodies.
	var compressed bytes.Buffer
	bw := brotli.NewWriterLevel(&compressed, brotli.DefaultCompression)
	_, _ = bw.Write(raw.Bytes())
	_ = bw.Close()

	totalSfntSize := uint32(12 + len(tables)*16) // sfnt header + dir entries
	for _, t := range tables {
		padded := (uint32(len(t.body)) + 3) &^ 3
		totalSfntSize += padded
	}

	// Assemble the file.
	out := make([]byte, 0, woff2HeaderLen+dir.Len()+compressed.Len())
	hdr := make([]byte, woff2HeaderLen)
	copy(hdr[0:4], []byte("wOF2"))
	copy(hdr[4:8], flavor)
	binary.BigEndian.PutUint32(hdr[8:12], 0) // length — recomputed below
	binary.BigEndian.PutUint16(hdr[12:14], uint16(len(tables)))
	binary.BigEndian.PutUint32(hdr[16:20], totalSfntSize)
	binary.BigEndian.PutUint32(hdr[20:24], uint32(compressed.Len()))
	// majorVersion/minorVersion + meta/priv offsets all zero.
	out = append(out, hdr...)
	out = append(out, dir.Bytes()...)
	out = append(out, compressed.Bytes()...)
	binary.BigEndian.PutUint32(out[8:12], uint32(len(out)))
	return out
}

// knownTagIndex returns the WOFF2 known-tag index for tag, or -1 if
// the tag isn't in the known table.
func knownTagIndex(tag string) int {
	for i, k := range woff2KnownTags {
		if k == tag {
			return i
		}
	}
	return -1
}

// writeUIntBase128 encodes v into buf per the WOFF2 spec §3 — the
// inverse of readUIntBase128.
func writeUIntBase128(buf *bytes.Buffer, v uint32) {
	if v == 0 {
		buf.WriteByte(0)
		return
	}
	var bytes7 [5]byte
	n := 0
	for v > 0 {
		bytes7[n] = byte(v & 0x7F)
		v >>= 7
		n++
	}
	// Write MSB-first; set MSB on all but the last (= least-significant)
	// byte.
	for i := n - 1; i >= 0; i-- {
		b := bytes7[i]
		if i != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
	}
}

func TestParseWOFF2_HappyPath(t *testing.T) {
	nameTable := buildName([]testNameRecord{
		{3, 1, 0x409, nameIDFamily, "Inter"},
		{3, 1, 0x409, nameIDDesigner, "Rasmus Andersson"},
	})
	// OS/2 with weight 700 (Bold) — minimum v0 length 78 bytes.
	os2 := make([]byte, 78)
	binary.BigEndian.PutUint16(os2[4:6], 700) // usWeightClass
	binary.BigEndian.PutUint16(os2[6:8], 5)   // usWidthClass = Medium

	data := buildWOFF2([]byte("OTTO"), []woff2TableBody{
		{tag: "name", body: nameTable},
		{tag: "OS/2", body: os2},
	})

	attrs := parseWOFF2(data)
	if attrs["font_format"] != "woff2" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
	if attrs["font_outline_kind"] != "cff" {
		t.Errorf("font_outline_kind = %v", attrs["font_outline_kind"])
	}
	if attrs["font_family"] != "Inter" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["font_designer"] != "Rasmus Andersson" {
		t.Errorf("font_designer = %v", attrs["font_designer"])
	}
	if attrs["font_weight"] != int64(700) {
		t.Errorf("font_weight = %v", attrs["font_weight"])
	}
	if attrs["is_bold_font"] != true {
		t.Errorf("is_bold_font = %v", attrs["is_bold_font"])
	}
	if attrs["title"] != "Inter" {
		t.Errorf("title (family dual-surface) = %v", attrs["title"])
	}
}

func TestParseWOFF2_VariableFont(t *testing.T) {
	data := buildWOFF2([]byte{0x00, 0x01, 0x00, 0x00}, []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "VarTest"}})},
		{tag: "fvar", body: buildFvar([]string{"wght", "wdth", "opsz"})},
		{tag: "glyf", body: []byte{0x00}},
	})
	attrs := parseWOFF2(data)
	if attrs["font_family"] != "VarTest" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["font_outline_kind"] != "truetype" {
		t.Errorf("font_outline_kind = %v", attrs["font_outline_kind"])
	}
	if attrs["is_variable_font"] != true {
		t.Errorf("is_variable_font = %v", attrs["is_variable_font"])
	}
	axes, ok := attrs["font_axes"].([]string)
	if !ok || len(axes) != 3 {
		t.Errorf("font_axes = %v", attrs["font_axes"])
	}
}

func TestParseWOFF2_HeaderOnlySurfaces(t *testing.T) {
	// Header-only attrs surface even when the directory / brotli payload
	// is missing — agents can still discriminate WOFF2 from other
	// formats and read the size hints.
	out := make([]byte, woff2HeaderLen)
	copy(out[0:4], []byte("wOF2"))
	copy(out[4:8], []byte("OTTO"))
	binary.BigEndian.PutUint32(out[16:20], 100000)
	binary.BigEndian.PutUint32(out[20:24], 35000)
	attrs := parseWOFF2(out)
	if attrs["font_format"] != "woff2" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
	if attrs["woff2_total_sfnt_size"] != int64(100000) {
		t.Errorf("woff2_total_sfnt_size = %v", attrs["woff2_total_sfnt_size"])
	}
	if attrs["woff2_total_compressed_size"] != int64(35000) {
		t.Errorf("woff2_total_compressed_size = %v", attrs["woff2_total_compressed_size"])
	}
}

func TestParseWOFF2_MagicMismatch(t *testing.T) {
	out := make([]byte, woff2HeaderLen)
	attrs := parseWOFF2(out)
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs on magic mismatch, got %v", attrs)
	}
}

func TestParseWOFF2_TruncatedBrotli(t *testing.T) {
	// Build a valid WOFF2, then truncate the brotli payload mid-stream
	// — parseWOFF2 must return the header attrs but no font_family.
	data := buildWOFF2([]byte("OTTO"), []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Test"}})},
	})
	// Lop off the last few bytes of the brotli stream.
	truncated := data[:len(data)-3]
	attrs := parseWOFF2(truncated)
	if attrs["font_format"] != "woff2" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
	if _, ok := attrs["font_family"]; ok {
		t.Errorf("font_family should be unset on truncated brotli, got %v", attrs["font_family"])
	}
}

func TestReadUIntBase128(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint32
		n    int
		ok   bool
	}{
		{"zero", []byte{0x00}, 0, 1, true},
		{"one-byte-127", []byte{0x7F}, 127, 1, true},
		{"two-byte-128", []byte{0x81, 0x00}, 128, 2, true},
		{"two-byte-16383", []byte{0xFF, 0x7F}, 16383, 2, true},
		{"leading-zero-rejected", []byte{0x80, 0x01}, 0, 0, false},
		{"empty", []byte{}, 0, 0, false},
		{"overflow", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 0, 0, false}, // 5-byte with continuation still set
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, n, ok := readUIntBase128(tc.data)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got != tc.want {
				t.Errorf("value = %d, want %d", got, tc.want)
			}
			if n != tc.n {
				t.Errorf("consumed = %d, want %d", n, tc.n)
			}
		})
	}
}

func TestWOFF2_RoundtripUIntBase128(t *testing.T) {
	// Every value writeUIntBase128 emits must readUIntBase128 back to
	// the same value. Sampled across the uint32 range plus the
	// known-edge values.
	values := []uint32{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455, 0xFFFFFFFF}
	for _, v := range values {
		var buf bytes.Buffer
		writeUIntBase128(&buf, v)
		got, _, ok := readUIntBase128(buf.Bytes())
		if !ok {
			t.Fatalf("readUIntBase128 failed on writeUIntBase128(%d) bytes %x", v, buf.Bytes())
		}
		if got != v {
			t.Errorf("roundtrip %d -> %d", v, got)
		}
	}
}

// ----------------------------------------------------------------------------
// Registry detection
// ----------------------------------------------------------------------------

func TestFontDetection_RegistryByMagic(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"ttf-magic.bin", buildSFNT(sfntMagicTrueType, map[string][]byte{"glyf": {0}}), "font/ttf"},
		{"otf-magic.bin", buildSFNT(sfntMagicOpenType, map[string][]byte{"CFF ": {0}}), "font/otf"},
		{"ttc-magic.bin", buildTTC([][]byte{buildSFNT(sfntMagicTrueType, map[string][]byte{"glyf": {0}})}), "font/collection"},
		{"woff-magic.bin", buildWOFF1(buildName(nil)), "font/woff"},
		{"woff2-magic.bin", buildWOFF2([]byte("OTTO"), []woff2TableBody{{tag: "name", body: buildName(nil)}}), "font/woff2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{tc.name: {Data: tc.data}}
			ct := DefaultRegistry().Detect(fsys, tc.name)
			if ct == nil {
				t.Fatalf("Detect returned nil for %s", tc.name)
			}
			if ct.Name() != tc.want {
				t.Errorf("Detect(%s) = %s, want %s", tc.name, ct.Name(), tc.want)
			}
		})
	}
}

func TestFontType_AttributesViaRegistry(t *testing.T) {
	data := buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Inter"}}),
		"glyf": []byte{0x00},
	})
	fsys := fstest.MapFS{"Inter-Regular.ttf": {Data: data}}
	ct := DefaultRegistry().Detect(fsys, "Inter-Regular.ttf")
	if ct.Name() != "font/ttf" {
		t.Fatalf("Detect = %s, want font/ttf", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "Inter-Regular.ttf")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if attrs["font_family"] != "Inter" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["font_format"] != "ttf" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
}

func TestWOFF2_AttributesViaRegistry(t *testing.T) {
	data := buildWOFF2([]byte("OTTO"), []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Inter"}})},
	})
	fsys := fstest.MapFS{"Inter-Regular.woff2": {Data: data}}
	ct := DefaultRegistry().Detect(fsys, "Inter-Regular.woff2")
	if ct == nil || ct.Name() != "font/woff2" {
		t.Fatalf("Detect = %v, want font/woff2", ct)
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "Inter-Regular.woff2")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if attrs["font_family"] != "Inter" {
		t.Errorf("font_family = %v", attrs["font_family"])
	}
	if attrs["font_format"] != "woff2" {
		t.Errorf("font_format = %v", attrs["font_format"])
	}
}

// ----------------------------------------------------------------------------
// Real-file integration test (macOS only)
// ----------------------------------------------------------------------------

func TestParseSFNT_RealMacOSFont(t *testing.T) {
	candidates := []string{
		"/System/Library/Fonts/Helvetica.ttc",
		"/System/Library/Fonts/HelveticaNeue.ttc",
		"/System/Library/Fonts/Geneva.ttf",
		"/Library/Fonts/Arial.ttf",
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("no system fonts found; skipping macOS integration test")
	}
	dir, base := filepath.Split(path)
	fsys := os.DirFS(dir)
	ct := DefaultRegistry().Detect(fsys, base)
	if ct == nil {
		t.Fatalf("Detect returned nil for %s", path)
	}
	if !strings.HasPrefix(ct.Name(), "font/") {
		t.Fatalf("Detect = %s, expected font/* family", ct.Name())
	}
	attrs, err := ct.Attributes(context.Background(), fsys, base)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if attrs["font_family"] == nil {
		t.Errorf("expected font_family populated on %s, got attrs=%v", path, attrs)
	}
}
