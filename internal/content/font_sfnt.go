package content

import (
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"unicode/utf16"
)

// Pure-Go sfnt parser. sfnt is the binary container shared by TrueType
// (.ttf), OpenType (.otf), TrueType Collections (.ttc / .otc), and Web
// Open Font Format (.woff) — all decode through this single
// table-directory walker. Per-format wrappers (font_ttf.go,
// font_otf.go, font_collection.go, font_woff.go) just dispatch here
// after handling their format-specific magic / outer-container shape.
//
// Spec: https://docs.microsoft.com/en-us/typography/opentype/spec/otff
//
// All multi-byte fields in sfnt + sub-tables are big-endian on disk
// regardless of the host byte order — the spec is explicit.
//
// parseSFNT is the pure-function entry point exercised by FuzzParseSFNT.
// Returns an empty sfntInfo (Present=false) on magic mismatch or short
// input — never panics. Defensive caps on every cardinality-bearing
// field defend against adversarial mutators.

// Defensive caps. Real fonts fit well within all of these; anything
// larger is either malformed or adversarial.
const (
	fontMaxBlobSize       = 16 << 20 // 16 MiB; system fonts top out ~10 MiB
	fontMaxTableCount     = 256      // real fonts ~30
	fontMaxNameRecords    = 1024
	fontMaxNameStringLen  = 4096 // truncate license walls of text
	fontMaxAxisCount      = 64   // real variable fonts have 1-5
	fontMaxCollectionSize = 256  // Apple system .ttc top out ~30
	fontMaxUnicodeRanges  = 128
)

// sfnt header magic values. The first 4 bytes of an sfnt-shaped file
// (or sub-table within a TTC) discriminate the outline format.
var (
	sfntMagicTrueType = []byte{0x00, 0x01, 0x00, 0x00}
	sfntMagicOpenType = []byte("OTTO")
	// 'true' is the legacy Apple TrueType magic; some old fonts (and
	// macOS system fonts) still ship with it. Parse same as TTF.
	sfntMagicTrueLegacy = []byte("true")
	// 'typ1' is the legacy PostScript Type 1 sfnt-shaped wrapper —
	// rare; we recognise it for parsing but don't fire a dedicated
	// is_type1 predicate (the underlying outlines aren't sfnt).
	sfntMagicType1 = []byte("typ1")
)

// sfntInfo aggregates the parsed table-by-table surface for one sfnt.
// A TTC walker calls parseSFNT per member and collects the results.
type sfntInfo struct {
	Present bool

	// Name table strings (UTF-8, possibly truncated to fontMaxNameStringLen).
	FamilyName            string
	SubfamilyName         string
	FullName              string
	Version               string
	PostScriptName        string
	Manufacturer          string
	Designer              string
	License               string
	LicenseURL            string
	TypographicFamilyName string

	// OS/2 table.
	Weight         int64  // usWeightClass (100-900)
	Width          int64  // usWidthClass (1-9)
	Embedding      string // installable / restricted / preview-print / editable
	Panose         string // 10-byte hex
	UnicodeRanges  []string

	// head table.
	Revision     float64
	UnitsPerEm   int64
	MacStyle     []string // bold / italic / underline / condensed / extended

	// post table.
	ItalicAngle float64
	IsMonospace bool

	// maxp table.
	GlyphCount int64

	// fvar table (variable fonts).
	AxisCount int64
	Axes      []string

	// Outline kind from table-presence ('glyf' / 'CFF ' / 'CFF2').
	OutlineKind string // truetype / cff / cff2

	// Color-font detection. True when any of COLR / SVG / sbix / CBDT
	// is present.
	IsColorFont bool

	// Italic / bold flags consolidated from head.macStyle + post +
	// OS/2.usWeightClass for the typed FileAttributes predicates.
	IsItalic bool
	IsBold   bool
}

// parseSFNT walks an sfnt at the given offset within data (offset = 0
// for bare TTF / OTF; nonzero for TTC member sfnts). Returns the
// decoded info or an empty sfntInfo on any failure. Never panics.
func parseSFNT(data []byte, offset int) sfntInfo {
	if offset < 0 || offset >= len(data) || len(data)-offset < 12 {
		return sfntInfo{}
	}
	header := data[offset:]
	// First 4 bytes: sfnt version / outline-format discriminator.
	if !sfntHasKnownMagic(header[:4]) {
		return sfntInfo{}
	}

	numTables := int(binary.BigEndian.Uint16(header[4:6]))
	if numTables == 0 || numTables > fontMaxTableCount {
		return sfntInfo{Present: true}
	}
	// Table directory: 12-byte sfnt header + numTables × 16-byte entries.
	dirEnd := 12 + numTables*16
	if dirEnd > len(header) {
		return sfntInfo{Present: true}
	}

	tables := make(map[string]tableRecord, numTables)
	for i := range numTables {
		off := 12 + i*16
		tag := string(header[off : off+4])
		// Table offset is from the start of the FILE, not the sfnt header,
		// for bare TTF/OTF — but for TTC members, the spec says offsets
		// are still file-absolute. So we use `data` (not `header`) as the
		// base when slicing the table body.
		tableOff := binary.BigEndian.Uint32(header[off+8 : off+12])
		tableLen := binary.BigEndian.Uint32(header[off+12 : off+16])
		tables[tag] = tableRecord{offset: int(tableOff), length: int(tableLen)}
	}

	info := sfntInfo{Present: true}

	// Outline kind: 'glyf' = TrueType, 'CFF ' = OpenType-CFF, 'CFF2' = CFF2.
	switch {
	case hasTable(tables, "CFF2"):
		info.OutlineKind = "cff2"
	case hasTable(tables, "CFF "):
		info.OutlineKind = "cff"
	case hasTable(tables, "glyf"):
		info.OutlineKind = "truetype"
	}

	// Color font: any of COLR / SVG / sbix / CBDT.
	for _, t := range []string{"COLR", "SVG ", "sbix", "CBDT"} {
		if hasTable(tables, t) {
			info.IsColorFont = true
			break
		}
	}

	if rec, ok := tables["name"]; ok {
		parseNameTable(data, rec, &info)
	}
	if rec, ok := tables["OS/2"]; ok {
		parseOS2Table(data, rec, &info)
	}
	if rec, ok := tables["head"]; ok {
		parseHeadTable(data, rec, &info)
	}
	if rec, ok := tables["post"]; ok {
		parsePostTable(data, rec, &info)
	}
	if rec, ok := tables["maxp"]; ok {
		parseMaxpTable(data, rec, &info)
	}
	if rec, ok := tables["fvar"]; ok {
		parseFvarTable(data, rec, &info)
	}

	// Consolidated bold/italic flags from multiple sources.
	if info.Weight >= 700 {
		info.IsBold = true
	}
	if info.ItalicAngle != 0 {
		info.IsItalic = true
	}
	// macStyle bits are also surfaced individually via MacStyle list.

	return info
}

// tableRecord locates one sub-table within the file.
type tableRecord struct {
	offset int
	length int
}

// hasTable reports whether the named table appears in the directory.
func hasTable(tables map[string]tableRecord, tag string) bool {
	_, ok := tables[tag]
	return ok
}

// sfntHasKnownMagic recognises the four 4-byte sfnt header magics:
// TrueType (0x00010000), OpenType (OTTO), legacy Apple TrueType (true),
// and legacy PostScript Type 1 (typ1).
func sfntHasKnownMagic(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	for _, m := range [][]byte{sfntMagicTrueType, sfntMagicOpenType, sfntMagicTrueLegacy, sfntMagicType1} {
		if b[0] == m[0] && b[1] == m[1] && b[2] == m[2] && b[3] == m[3] {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// name table
// ----------------------------------------------------------------------------

// Name IDs we care about per the OpenType spec §6.3.
const (
	nameIDCopyright             = 0
	nameIDFamily                = 1
	nameIDSubfamily             = 2
	nameIDFullName              = 4
	nameIDVersion               = 5
	nameIDPostScript            = 6
	nameIDManufacturer          = 8
	nameIDDesigner              = 9
	nameIDLicenseDescription    = 13
	nameIDLicenseURL            = 14
	nameIDTypographicFamily     = 16
)

// (platformID, encodingID, languageID) selection priority for the name
// table. We pick the highest-priority record that's present and decode
// it according to the platform. The encoding matrix is the trickiest
// part of sfnt parsing — anyone touching this should read Microsoft's
// OpenType spec §6.3 and the comments below before changing anything.
//
// Priority (highest to lowest):
//
//  1. Windows Unicode BMP, English US           (3, 1, 0x409) — modern norm
//  2. Windows Unicode BMP, any language         (3, 1, *)     — non-en fonts
//  3. Mac Roman, English                        (1, 0, 0)     — legacy Apple
//  4. Any other Windows Unicode encoding        (3, *, *)     — Unicode full
//  5. First record present                                    — last resort
//
// UTF-16BE decode for platform 3 (Windows Unicode encodings).
// Mac Roman decode for platform 1 (a single-byte legacy encoding).
type nameRecord struct {
	platformID uint16
	encodingID uint16
	languageID uint16
	nameID     uint16
	length     uint16
	offset     uint16 // from name table's stringOffset
}

func parseNameTable(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 6 || rec.offset+rec.length > len(data) {
		return
	}
	table := data[rec.offset : rec.offset+rec.length]
	// Header: format (uint16), count (uint16), stringOffset (uint16).
	count := int(binary.BigEndian.Uint16(table[2:4]))
	stringOffset := int(binary.BigEndian.Uint16(table[4:6]))
	if count > fontMaxNameRecords {
		count = fontMaxNameRecords
	}
	if 6+count*12 > len(table) || stringOffset > len(table) {
		return
	}

	records := make([]nameRecord, 0, count)
	for i := range count {
		off := 6 + i*12
		records = append(records, nameRecord{
			platformID: binary.BigEndian.Uint16(table[off : off+2]),
			encodingID: binary.BigEndian.Uint16(table[off+2 : off+4]),
			languageID: binary.BigEndian.Uint16(table[off+4 : off+6]),
			nameID:     binary.BigEndian.Uint16(table[off+6 : off+8]),
			length:     binary.BigEndian.Uint16(table[off+8 : off+10]),
			offset:     binary.BigEndian.Uint16(table[off+10 : off+12]),
		})
	}

	// Per name ID we care about, pick the best record per the encoding
	// priority and decode it.
	wanted := map[uint16]*string{
		nameIDFamily:             &info.FamilyName,
		nameIDSubfamily:          &info.SubfamilyName,
		nameIDFullName:           &info.FullName,
		nameIDVersion:            &info.Version,
		nameIDPostScript:         &info.PostScriptName,
		nameIDManufacturer:       &info.Manufacturer,
		nameIDDesigner:           &info.Designer,
		nameIDLicenseDescription: &info.License,
		nameIDLicenseURL:         &info.LicenseURL,
		nameIDTypographicFamily:  &info.TypographicFamilyName,
	}
	for nid, target := range wanted {
		best := bestNameRecord(records, nid)
		if best == nil {
			continue
		}
		s := decodeNameRecord(table, stringOffset, *best)
		if s == "" {
			continue
		}
		if len(s) > fontMaxNameStringLen {
			s = s[:fontMaxNameStringLen]
		}
		*target = s
	}
}

// bestNameRecord picks the highest-priority name record for the given
// nameID per the priority list above.
func bestNameRecord(records []nameRecord, nameID uint16) *nameRecord {
	var winUnicodeEN, winUnicodeAny, macRomanEN, winUnicodeRest, anyRecord *nameRecord
	for i := range records {
		r := &records[i]
		if r.nameID != nameID {
			continue
		}
		if anyRecord == nil {
			anyRecord = r
		}
		switch {
		case r.platformID == 3 && r.encodingID == 1 && r.languageID == 0x409:
			if winUnicodeEN == nil {
				winUnicodeEN = r
			}
		case r.platformID == 3 && r.encodingID == 1:
			if winUnicodeAny == nil {
				winUnicodeAny = r
			}
		case r.platformID == 1 && r.encodingID == 0 && r.languageID == 0:
			if macRomanEN == nil {
				macRomanEN = r
			}
		case r.platformID == 3:
			if winUnicodeRest == nil {
				winUnicodeRest = r
			}
		}
	}
	for _, r := range []*nameRecord{winUnicodeEN, winUnicodeAny, macRomanEN, winUnicodeRest, anyRecord} {
		if r != nil {
			return r
		}
	}
	return nil
}

// decodeNameRecord pulls the string bytes out of the name table's
// string heap and decodes them according to the platformID's
// convention. UTF-16BE for Windows (platform 3); MacRoman 1:1 for
// platform 1 (close enough — real MacRoman has a 128-byte non-ASCII
// upper half but the family / designer / etc. fields are almost
// always ASCII).
func decodeNameRecord(table []byte, stringHeapOffset int, r nameRecord) string {
	start := stringHeapOffset + int(r.offset)
	end := start + int(r.length)
	if start < 0 || end > len(table) || start >= end {
		return ""
	}
	raw := table[start:end]

	if r.platformID == 3 { // Windows Unicode (UTF-16BE)
		if len(raw)%2 != 0 {
			raw = raw[:len(raw)-1]
		}
		u16 := make([]uint16, len(raw)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(raw[i*2 : i*2+2])
		}
		s := string(utf16.Decode(u16))
		return strings.TrimSpace(s)
	}
	// Platform 1 (Mac) — treat as 1-byte. Non-ASCII bytes pass through;
	// for the strings we care about (family / designer / license), this
	// is correct in practice.
	return strings.TrimSpace(string(raw))
}

// ----------------------------------------------------------------------------
// OS/2 table
// ----------------------------------------------------------------------------

// OS/2 fsType embedding bits (FontEmbedding licensing levels per
// OpenType spec §6.2.4). These are LEGAL not technical — "restricted"
// doesn't mean the format enforces anything; agents auditing font
// licensing on a deployed codebase should know this is informational.
const (
	os2FsTypeRestricted    = 0x0002 // Restricted Licence: no embedding
	os2FsTypePreviewPrint  = 0x0004 // Preview & Print
	os2FsTypeEditable      = 0x0008 // Editable embedding
	os2FsTypeNoSubset      = 0x0100 // No subsetting
	os2FsTypeBitmapOnly    = 0x0200 // Bitmap embedding only
)

func parseOS2Table(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 78 || rec.offset+rec.length > len(data) {
		return
	}
	t := data[rec.offset:]

	// Layout per OpenType spec §6.2 (OS/2 v0 — minimum we need is the
	// first 78 bytes, present in every OS/2 version since 1996).
	info.Weight = int64(binary.BigEndian.Uint16(t[4:6]))   // usWeightClass
	info.Width = int64(binary.BigEndian.Uint16(t[6:8]))    // usWidthClass
	info.Embedding = os2EmbeddingName(binary.BigEndian.Uint16(t[8:10]))

	// Panose: 10 bytes at offset 32.
	if len(t) >= 42 {
		info.Panose = hex.EncodeToString(t[32:42])
	}
	// ulUnicodeRange1..4 at offsets 42..58.
	if len(t) >= 58 {
		var bits [4]uint32
		bits[0] = binary.BigEndian.Uint32(t[42:46])
		bits[1] = binary.BigEndian.Uint32(t[46:50])
		bits[2] = binary.BigEndian.Uint32(t[50:54])
		bits[3] = binary.BigEndian.Uint32(t[54:58])
		info.UnicodeRanges = decodeUnicodeRangeBits(bits)
	}
}

// os2EmbeddingName maps the fsType bitmask to the highest-priority
// licensing level per the OpenType spec. Order matters: restricted
// beats everything; installable (the default zero bitmask) is the
// fallback.
func os2EmbeddingName(fsType uint16) string {
	switch {
	case fsType&os2FsTypeRestricted != 0:
		return "restricted"
	case fsType&os2FsTypePreviewPrint != 0:
		return "preview-print"
	case fsType&os2FsTypeEditable != 0:
		return "editable"
	}
	return "installable"
}

// decodeUnicodeRangeBits maps the OS/2 ulUnicodeRange1-4 128-bit
// bitfield to canonical Unicode block names. Only the most-common bits
// are decoded — the spec defines 128 ranges but most agents only care
// about "is this font Latin / Cyrillic / CJK / Arabic / Hebrew". Capped
// at fontMaxUnicodeRanges entries.
func decodeUnicodeRangeBits(bits [4]uint32) []string {
	out := make([]string, 0, 16)
	check := func(word int, bit uint, name string) {
		if bits[word]&(1<<bit) != 0 {
			out = append(out, name)
		}
	}
	// Word 0 (ulUnicodeRange1) — Basic Latin & Western scripts.
	check(0, 0, "Basic Latin")
	check(0, 1, "Latin-1 Supplement")
	check(0, 2, "Latin Extended-A")
	check(0, 3, "Latin Extended-B")
	check(0, 4, "IPA Extensions")
	check(0, 7, "Greek and Coptic")
	check(0, 9, "Cyrillic")
	check(0, 10, "Armenian")
	check(0, 11, "Hebrew")
	check(0, 13, "Arabic")
	check(0, 15, "Devanagari")
	check(0, 16, "Bengali")
	check(0, 21, "Thai")
	check(0, 23, "Georgian")
	// Word 1 (ulUnicodeRange2) — Symbols + East Asian gateway blocks.
	check(1, 16, "General Punctuation")
	check(1, 17, "Superscripts and Subscripts")
	check(1, 18, "Currency Symbols")
	check(1, 22, "Mathematical Operators")
	// Word 2 (ulUnicodeRange3) — CJK + Korean.
	check(2, 16, "Hiragana")
	check(2, 17, "Katakana")
	check(2, 19, "Hangul Syllables")
	check(2, 20, "Non-Plane 0 (Astral Plane)")
	check(2, 21, "CJK Unified Ideographs")
	check(2, 22, "Phonetic Extensions")
	check(2, 28, "Hangul Jamo")
	// Word 3 (ulUnicodeRange4) — Plane 1+ surrogates + emoji-adjacent.
	check(3, 25, "Variation Selectors")
	check(3, 30, "Tags")
	if len(out) > fontMaxUnicodeRanges {
		out = out[:fontMaxUnicodeRanges]
	}
	return out
}

// ----------------------------------------------------------------------------
// head table
// ----------------------------------------------------------------------------

// head.macStyle bits per OpenType spec §6.1.
const (
	macStyleBold      = 0x0001
	macStyleItalic    = 0x0002
	macStyleUnderline = 0x0004
	macStyleOutline   = 0x0008
	macStyleShadow    = 0x0010
	macStyleCondensed = 0x0020
	macStyleExtended  = 0x0040
)

func parseHeadTable(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 54 || rec.offset+rec.length > len(data) {
		return
	}
	t := data[rec.offset:]
	// fontRevision at offset 4: Fixed (16.16).
	info.Revision = fixed1616(binary.BigEndian.Uint32(t[4:8]))
	// unitsPerEm at offset 18: uint16.
	info.UnitsPerEm = int64(binary.BigEndian.Uint16(t[18:20]))
	// macStyle at offset 44: uint16 bitfield.
	macStyle := binary.BigEndian.Uint16(t[44:46])
	if macStyle&macStyleBold != 0 {
		info.MacStyle = append(info.MacStyle, "bold")
		info.IsBold = true
	}
	if macStyle&macStyleItalic != 0 {
		info.MacStyle = append(info.MacStyle, "italic")
		info.IsItalic = true
	}
	if macStyle&macStyleUnderline != 0 {
		info.MacStyle = append(info.MacStyle, "underline")
	}
	if macStyle&macStyleOutline != 0 {
		info.MacStyle = append(info.MacStyle, "outline")
	}
	if macStyle&macStyleShadow != 0 {
		info.MacStyle = append(info.MacStyle, "shadow")
	}
	if macStyle&macStyleCondensed != 0 {
		info.MacStyle = append(info.MacStyle, "condensed")
	}
	if macStyle&macStyleExtended != 0 {
		info.MacStyle = append(info.MacStyle, "extended")
	}
}

// fixed1616 converts an OpenType Fixed (16.16) to float64.
func fixed1616(raw uint32) float64 {
	return float64(int32(raw)) / 65536.0
}

// ----------------------------------------------------------------------------
// post table
// ----------------------------------------------------------------------------

func parsePostTable(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 16 || rec.offset+rec.length > len(data) {
		return
	}
	t := data[rec.offset:]
	// italicAngle at offset 4: Fixed (16.16).
	info.ItalicAngle = fixed1616(binary.BigEndian.Uint32(t[4:8]))
	// isFixedPitch at offset 12: uint32 (non-zero = monospace).
	info.IsMonospace = binary.BigEndian.Uint32(t[12:16]) != 0
}

// ----------------------------------------------------------------------------
// maxp table
// ----------------------------------------------------------------------------

func parseMaxpTable(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 6 || rec.offset+rec.length > len(data) {
		return
	}
	t := data[rec.offset:]
	// numGlyphs at offset 4: uint16.
	info.GlyphCount = int64(binary.BigEndian.Uint16(t[4:6]))
}

// ----------------------------------------------------------------------------
// fvar table (variable fonts)
// ----------------------------------------------------------------------------

func parseFvarTable(data []byte, rec tableRecord, info *sfntInfo) {
	if rec.offset < 0 || rec.length < 16 || rec.offset+rec.length > len(data) {
		return
	}
	t := data[rec.offset:]
	// Header: version (4) + axesArrayOffset (2) + reserved (2) +
	// axisCount (2) + axisSize (2) + instanceCount (2) + instanceSize (2)
	axesArrayOffset := int(binary.BigEndian.Uint16(t[4:6]))
	axisCount := int(binary.BigEndian.Uint16(t[8:10]))
	axisSize := int(binary.BigEndian.Uint16(t[10:12]))

	if axisCount == 0 || axisCount > fontMaxAxisCount || axisSize < 20 {
		return
	}
	info.AxisCount = int64(axisCount)
	end := axesArrayOffset + axisCount*axisSize
	if end > len(t) {
		return
	}

	for i := range axisCount {
		off := axesArrayOffset + i*axisSize
		// VariationAxisRecord: axisTag (4) + minValue/defaultValue/maxValue (4×3) + flags (2) + axisNameID (2)
		tag := strings.TrimSpace(string(t[off : off+4]))
		if tag != "" {
			info.Axes = append(info.Axes, tag)
		}
	}
}

// ----------------------------------------------------------------------------
// Attributes mapper
// ----------------------------------------------------------------------------

// sfntAttrs maps the parsed info into the Attributes the celexpr layer
// consumes. Empty / zero-valued fields are omitted so the JSON wire
// shape stays clean for sparse fonts (e.g. system fonts without
// license metadata).
//
// format controls the font_format attribute ("ttf" / "otf" / "ttc" /
// "otc" / "woff" / "woff2"); per-format wrappers pass the right value.
func sfntAttrs(info sfntInfo, format string) Attributes {
	out := Attributes{}
	if !info.Present {
		return out
	}
	if format != "" {
		out["font_format"] = format
	}
	if info.OutlineKind != "" {
		out["font_outline_kind"] = info.OutlineKind
	}

	// Name table strings. Cross-family dual-surface: family → title,
	// designer → author so generic queries like `title.contains("Inter")`
	// fire on fonts as well as documents. Matches the FITS pattern of
	// `OBJECT → title` / `OBSERVER → author`. The typed font_* keys
	// are always retained alongside the shared vocabulary keys.
	if info.FamilyName != "" {
		out["font_family"] = info.FamilyName
		out["title"] = info.FamilyName
	}
	if info.SubfamilyName != "" {
		out["font_subfamily"] = info.SubfamilyName
	}
	if info.FullName != "" {
		out["font_full_name"] = info.FullName
	}
	if info.Version != "" {
		out["font_version"] = info.Version
	}
	if info.PostScriptName != "" {
		out["font_postscript_name"] = info.PostScriptName
	}
	if info.Manufacturer != "" {
		out["font_manufacturer"] = info.Manufacturer
	}
	if info.Designer != "" {
		out["font_designer"] = info.Designer
		out["author"] = info.Designer
	}
	if info.License != "" {
		out["font_license"] = info.License
	}
	if info.LicenseURL != "" {
		out["font_license_url"] = info.LicenseURL
	}
	if info.TypographicFamilyName != "" {
		out["font_typographic_family"] = info.TypographicFamilyName
	}

	// OS/2.
	if info.Weight > 0 {
		out["font_weight"] = info.Weight
	}
	if info.Width > 0 {
		out["font_width"] = info.Width
	}
	if info.Embedding != "" {
		out["font_embedding"] = info.Embedding
	}
	if info.Panose != "" && info.Panose != "00000000000000000000" {
		out["font_panose"] = info.Panose
	}
	if len(info.UnicodeRanges) > 0 {
		// Stable order for repeatable JSON output.
		ranges := make([]string, len(info.UnicodeRanges))
		copy(ranges, info.UnicodeRanges)
		sort.Strings(ranges)
		out["font_unicode_ranges"] = ranges
	}

	// head.
	if info.Revision != 0 {
		out["font_revision"] = info.Revision
	}
	if info.UnitsPerEm > 0 {
		out["font_units_per_em"] = info.UnitsPerEm
	}
	if len(info.MacStyle) > 0 {
		out["font_mac_style"] = info.MacStyle
	}

	// post.
	if info.ItalicAngle != 0 {
		out["font_italic_angle"] = info.ItalicAngle
	}

	// maxp.
	if info.GlyphCount > 0 {
		out["font_glyph_count"] = info.GlyphCount
	}

	// fvar.
	if info.AxisCount > 0 {
		out["font_axis_count"] = info.AxisCount
		out["is_variable_font"] = true
	}
	if len(info.Axes) > 0 {
		out["font_axes"] = info.Axes
	}

	// Per-trait predicates derived from parser output. These fall
	// through the celexpr activation switch to the Extra-map lookup
	// (same shape as is_codesigned in #187) — keeping them out of
	// FileAttributes' typed-field set since they're parser-dependent
	// rather than content-type-dependent.
	if info.IsColorFont {
		out["is_color_font"] = true
	}
	if info.IsMonospace {
		out["is_monospace_font"] = true
	}
	if info.IsItalic {
		out["is_italic_font"] = true
	}
	if info.IsBold {
		out["is_bold_font"] = true
	}

	return out
}

