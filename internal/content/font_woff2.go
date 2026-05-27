package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"maps"

	"github.com/andybalholm/brotli"
)

// WOFF2 (Web Open Font Format v2, W3C TR 2018). Brotli-compressed
// wrapper around an sfnt with additional transformed-table encoding
// for `glyf` / `loca` / `hmtx` to improve compression ratio.
//
// Extraction strategy: read the 48-byte header + variable-length table
// directory, brotli-decompress the data block, then slice each
// metadata table (name / OS/2 / head / post / maxp / fvar) from the
// decompressed stream and hand it to the existing per-table decoders.
// The transformed glyf / loca / hmtx tables are NOT reconstructed —
// file-search-on doesn't surface glyph data. The directory walker
// advances past those tables using their transformLength so the
// metadata-table offsets stay correct.
//
// Layout per https://www.w3.org/TR/WOFF2/ §4.1:
//
//	WOFF2 header (48 bytes):
//	  signature             u32   'wOF2'
//	  flavor                u32   underlying sfnt's scaler version
//	  length                u32   total file size
//	  numTables             u16
//	  reserved              u16
//	  totalSfntSize         u32   uncompressed sfnt size
//	  totalCompressedSize   u32   compressed brotli stream size
//	  majorVersion          u16
//	  minorVersion          u16
//	  metaOffset            u32
//	  metaLength            u32
//	  metaOrigLength        u32
//	  privOffset            u32
//	  privLength            u32
//
//	CompactDirectoryEntry (variable length, per table):
//	  flags         u8         bits 0..5: tag index (0..63), bits 6..7: transform version
//	  [tag          u32]       only if flags bits 0..5 == 0x3F (63)
//	  origLength    UIntBase128
//	  [transformLength UIntBase128]  present per the transform rules below
//
// Issue #196 — supersedes the v1 detect-only path from #197.

const woff2HeaderLen = 48

// woff2MaxDirSize bounds the directory walk. 256 tables × worst-case
// ~14 bytes per CompactDirectoryEntry (u8 flags + 4-byte inline tag +
// 5-byte UIntBase128 origLength + 5-byte UIntBase128 transformLength)
// = ~3584 bytes. 8 KiB is generous.
const woff2MaxDirSize = 8 << 10

// woff2KnownTags is the WOFF2 known-tags table indexed 0..62 per spec §3.
// Tag index 63 (0x3F) signals "read inline 4-byte tag" instead of a lookup.
var woff2KnownTags = [...]string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post",
	"cvt ", "fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT",
	"EBLC", "gasp", "hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea",
	"vmtx", "BASE", "GDEF", "GPOS", "GSUB", "EBSC", "JSTF", "MATH",
	"CBDT", "CBLC", "COLR", "CPAL", "SVG ", "sbix", "acnt", "avar",
	"bdat", "bloc", "bsln", "cvar", "fdsc", "feat", "fmtx", "fvar",
	"gvar", "hsty", "just", "lcar", "mort", "morx", "opbd", "prop",
	"trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

func init() {
	Register(&woff2Type{})
}

type woff2Type struct{}

func (*woff2Type) Name() string         { return "font/woff2" }
func (*woff2Type) Extensions() []string { return []string{".woff2"} }
func (*woff2Type) MagicBytes() [][]byte { return [][]byte{[]byte("wOF2")} }

func (*woff2Type) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, fontMaxBlobSize))
	if err != nil {
		return Attributes{}, nil
	}
	return parseWOFF2(buf), nil
}

// parseWOFF2 decodes a WOFF2 file end-to-end: header + directory +
// brotli payload + per-metadata-table parse. Returns empty / partial
// attrs on any failure; never panics.
func parseWOFF2(data []byte) Attributes {
	if len(data) < woff2HeaderLen {
		return Attributes{}
	}
	if string(data[0:4]) != "wOF2" {
		return Attributes{}
	}
	out := Attributes{
		"font_format": "woff2",
	}
	flavor := data[4:8]
	// Header byte counts surface unconditionally so an unparseable
	// directory still leaves some signal for agent queries.
	out["woff2_total_sfnt_size"] = int64(binary.BigEndian.Uint32(data[16:20]))
	out["woff2_total_compressed_size"] = int64(binary.BigEndian.Uint32(data[20:24]))

	// TTC collections via WOFF2 (flavor 'ttcf') are rare and not supported
	// — surface only the umbrella header fields and bail.
	if string(flavor) == "ttcf" {
		return out
	}
	switch {
	case string(flavor) == "OTTO":
		out["font_outline_kind"] = "cff"
	case flavor[0] == 0x00 && flavor[1] == 0x01 && flavor[2] == 0x00 && flavor[3] == 0x00:
		out["font_outline_kind"] = "truetype"
	}

	numTables := int(binary.BigEndian.Uint16(data[12:14]))
	totalCompressedSize := int(binary.BigEndian.Uint32(data[20:24]))
	if numTables == 0 || numTables > fontMaxTableCount {
		return out
	}

	entries, dirEnd, ok := parseWOFF2Directory(data[woff2HeaderLen:], numTables)
	if !ok {
		return out
	}
	streamStart := woff2HeaderLen + dirEnd
	if totalCompressedSize <= 0 || streamStart+totalCompressedSize > len(data) {
		return out
	}
	// Brotli-decompress the data block, bounded by fontMaxBlobSize so an
	// adversarial stream can't exhaust memory.
	br := brotli.NewReader(bytes.NewReader(data[streamStart : streamStart+totalCompressedSize]))
	decompressed, err := io.ReadAll(io.LimitReader(br, fontMaxBlobSize))
	if err != nil {
		return out
	}

	info := sfntInfo{Present: true}
	presence := make(map[string]tableRecord, len(entries))
	streamOff := 0
	for _, e := range entries {
		presence[e.tag] = tableRecord{offset: 0, length: e.origLength}
		onStream := e.streamLength()
		if onStream < 0 || streamOff+onStream > len(decompressed) {
			break
		}
		// Skip transformed tables — we don't reconstruct glyf/loca/hmtx.
		// The metadata tables we care about (name / OS/2 / head / post /
		// maxp / fvar) are never transformed in practice.
		if !e.transformed() {
			body := decompressed[streamOff : streamOff+onStream]
			rec := tableRecord{offset: 0, length: len(body)}
			switch e.tag {
			case "name":
				parseNameTable(body, rec, &info)
			case "OS/2":
				parseOS2Table(body, rec, &info)
			case "head":
				parseHeadTable(body, rec, &info)
			case "post":
				parsePostTable(body, rec, &info)
			case "maxp":
				parseMaxpTable(body, rec, &info)
			case "fvar":
				parseFvarTable(body, rec, &info)
			}
		}
		streamOff += onStream
	}

	// Outline kind from table presence — overrides the flavor-derived
	// hint above when both fire (CFF2 doesn't appear in flavor at all).
	switch {
	case hasTable(presence, "CFF2"):
		info.OutlineKind = "cff2"
	case hasTable(presence, "CFF "):
		info.OutlineKind = "cff"
	case hasTable(presence, "glyf"):
		info.OutlineKind = "truetype"
	}
	for _, t := range []string{"COLR", "SVG ", "sbix", "CBDT"} {
		if hasTable(presence, t) {
			info.IsColorFont = true
			break
		}
	}

	// Consolidated bold/italic — same rule as parseSFNT.
	if info.Weight >= 700 {
		info.IsBold = true
	}
	if info.ItalicAngle != 0 {
		info.IsItalic = true
	}

	// Merge the sfnt-derived attrs over the header-derived ones.
	merged := sfntAttrs(info, "woff2")
	maps.Copy(out, merged)
	return out
}

// woff2Entry captures one row of the WOFF2 table directory.
type woff2Entry struct {
	tag              string
	transformVersion uint8
	origLength       int
	transformLength  int
	hasTransformLen  bool
}

// transformed reports whether this entry's body is in transformed form
// in the decompressed stream — meaning we should SKIP it (we don't
// reconstruct glyf / loca / hmtx).
//
// Per spec §5.1: the transform-version bits invert their meaning for
// glyf and loca — version 0 (default) means transformed, version 3
// means as-is. hmtx is the reverse: version 0 means as-is, version 1
// means transformed. All other tables: version 0 = as-is, anything
// else = transformed.
func (e woff2Entry) transformed() bool {
	switch e.tag {
	case "glyf", "loca":
		return e.transformVersion != 3
	case "hmtx":
		return e.transformVersion == 1
	}
	return e.transformVersion != 0
}

// streamLength returns the on-stream byte count for this table — the
// transformLength when present, otherwise the origLength.
func (e woff2Entry) streamLength() int {
	if e.hasTransformLen {
		return e.transformLength
	}
	return e.origLength
}

// parseWOFF2Directory walks the WOFF2 table directory at the start of
// the given slice. Returns the parsed entries, the byte length consumed,
// and false on malformed input.
func parseWOFF2Directory(data []byte, numTables int) ([]woff2Entry, int, bool) {
	entries := make([]woff2Entry, 0, numTables)
	pos := 0
	for range numTables {
		if pos >= len(data) || pos >= woff2MaxDirSize {
			return nil, 0, false
		}
		flags := data[pos]
		pos++
		tagIndex := int(flags & 0x3F)
		transformVersion := uint8((flags >> 6) & 0x03)

		var tag string
		switch {
		case tagIndex == 0x3F:
			if pos+4 > len(data) {
				return nil, 0, false
			}
			tag = string(data[pos : pos+4])
			pos += 4
		case tagIndex < len(woff2KnownTags):
			tag = woff2KnownTags[tagIndex]
		default:
			return nil, 0, false
		}

		origLength, n, ok := readUIntBase128(data[pos:])
		if !ok {
			return nil, 0, false
		}
		pos += n

		e := woff2Entry{
			tag:              tag,
			transformVersion: transformVersion,
			origLength:       int(origLength),
		}

		needsTransformLen := false
		switch tag {
		case "glyf", "loca":
			needsTransformLen = transformVersion != 3
		default:
			needsTransformLen = transformVersion != 0
		}
		if needsTransformLen {
			transformLength, n2, ok2 := readUIntBase128(data[pos:])
			if !ok2 {
				return nil, 0, false
			}
			pos += n2
			e.transformLength = int(transformLength)
			e.hasTransformLen = true
		}

		entries = append(entries, e)
	}
	return entries, pos, true
}

// readUIntBase128 decodes a WOFF2 UIntBase128 from the start of data.
// Per spec §3 the encoding is up to 5 bytes; MSB=1 means more bytes
// follow; lower 7 bits hold the value (MSB-first). Leading zeros are
// forbidden (first byte cannot equal 0x80) and the result must fit in
// uint32.
func readUIntBase128(data []byte) (uint32, int, bool) {
	var v uint32
	for i := range 5 {
		if i >= len(data) {
			return 0, 0, false
		}
		b := data[i]
		// Leading-zero check: first byte cannot be exactly 0x80.
		if i == 0 && b == 0x80 {
			return 0, 0, false
		}
		// uint32 overflow guard — top 7 bits would shift past bit 31.
		if v&0xFE000000 != 0 {
			return 0, 0, false
		}
		v = (v << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v, i + 1, true
		}
	}
	// 5-byte sequence with continuation flag still set on the last byte
	// = overflow / malformed.
	return 0, 0, false
}
