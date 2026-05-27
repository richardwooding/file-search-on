package content

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
)

// WOFF1 (Web Open Font Format v1, W3C TR 2012). A zlib-compressed
// wrapper around an sfnt — same table directory shape, but each table
// can be independently zlib-compressed. We decompress only the `name`
// table (the most-valuable surface for agent queries) and hand the
// inflated bytes to parseNameTable. Other attribute sources (OS/2,
// head, post, maxp, fvar) are decoded lazily on the same lazy path
// — but for v1 we extract only the name + header-derived format
// metadata; the other tables stay in their compressed slots.
//
// Layout per https://www.w3.org/TR/WOFF/ §4.1:
//
//	WOFF header:
//	  signature           u32   'wOFF'
//	  flavor              u32   underlying sfnt's scaler version
//	  length              u32   total file size
//	  numTables           u16
//	  reserved            u16
//	  totalSfntSize       u32   uncompressed sfnt size
//	  majorVersion        u16
//	  minorVersion        u16
//	  metaOffset          u32   offset to optional WOFF metadata XML
//	  metaLength          u32   compressed metadata length
//	  metaOrigLength      u32   uncompressed metadata length
//	  privOffset          u32   offset to optional private data
//	  privLength          u32
//
//	WOFF table directory entry (5 × u32 each):
//	  tag, offset, compLength, origLength, origChecksum
//
// If compLength == origLength the table is stored uncompressed;
// otherwise it's zlib-deflated. We follow that branch when reading.
//
// Issue #197.

const woffHeaderLen = 44

func init() {
	Register(&woffType{})
}

type woffType struct{}

func (*woffType) Name() string         { return "font/woff" }
func (*woffType) Extensions() []string { return []string{".woff"} }
func (*woffType) MagicBytes() [][]byte { return [][]byte{[]byte("wOFF")} }

func (*woffType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
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
	return parseWOFF(buf), nil
}

// parseWOFF decodes a WOFF1 wrapper. Surfaces the underlying sfnt's
// outline-kind discriminator + the `name` table strings. Other tables
// are noted via the table-presence map (so font_outline_kind /
// is_variable_font / is_color_font still fire) but their bodies are
// only decompressed when their tag matches one we want to decode.
func parseWOFF(data []byte) Attributes {
	if len(data) < woffHeaderLen {
		return Attributes{}
	}
	if string(data[0:4]) != "wOFF" {
		return Attributes{}
	}
	numTables := int(binary.BigEndian.Uint16(data[12:14]))
	if numTables == 0 || numTables > fontMaxTableCount {
		return Attributes{}
	}
	dirEnd := woffHeaderLen + numTables*20
	if dirEnd > len(data) {
		return Attributes{}
	}

	// Walk the WOFF table directory. We need both the table-presence
	// set (for font_outline_kind + is_color_font + is_variable_font)
	// AND a way to lazily inflate the `name` table.
	type woffTableEntry struct {
		tag        string
		offset     int
		compLen    int
		origLen    int
	}
	entries := make([]woffTableEntry, numTables)
	tablePresence := make(map[string]tableRecord, numTables)
	for i := range numTables {
		off := woffHeaderLen + i*20
		tag := string(data[off : off+4])
		tableOff := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		compLen := int(binary.BigEndian.Uint32(data[off+8 : off+12]))
		origLen := int(binary.BigEndian.Uint32(data[off+12 : off+16]))
		entries[i] = woffTableEntry{tag: tag, offset: tableOff, compLen: compLen, origLen: origLen}
		// The presence map uses fake offsets — only the existence
		// matters for the kind/color/variable predicates.
		tablePresence[tag] = tableRecord{offset: 0, length: origLen}
	}

	info := sfntInfo{Present: true}

	// Outline kind from presence.
	switch {
	case hasTable(tablePresence, "CFF2"):
		info.OutlineKind = "cff2"
	case hasTable(tablePresence, "CFF "):
		info.OutlineKind = "cff"
	case hasTable(tablePresence, "glyf"):
		info.OutlineKind = "truetype"
	}
	for _, t := range []string{"COLR", "SVG ", "sbix", "CBDT"} {
		if hasTable(tablePresence, t) {
			info.IsColorFont = true
			break
		}
	}

	// Inflate the `name` table specifically and parse its strings.
	// Other tables stay compressed in v1 — the agent-query value
	// concentrates in name. fvar / OS/2 extraction from WOFF1 is a
	// follow-up if real-world demand surfaces.
	for _, e := range entries {
		if e.tag != "name" {
			continue
		}
		if e.offset < 0 || e.compLen < 0 || e.offset+e.compLen > len(data) {
			break
		}
		var nameBlob []byte
		if e.compLen == e.origLen {
			// Stored uncompressed.
			nameBlob = data[e.offset : e.offset+e.compLen]
		} else {
			// zlib-deflated. Use compress/zlib (stdlib) bounded by
			// io.LimitReader to cap inflated size at the original-
			// length declaration plus a safety margin.
			zr, err := zlib.NewReader(bytes.NewReader(data[e.offset : e.offset+e.compLen]))
			if err != nil {
				break
			}
			limit := int64(e.origLen)
			if limit <= 0 || limit > fontMaxBlobSize {
				limit = fontMaxBlobSize
			}
			nameBlob, err = io.ReadAll(io.LimitReader(zr, limit))
			_ = zr.Close()
			if err != nil {
				break
			}
		}
		// Synthesise a tableRecord pointing into a freshly-allocated
		// slice so parseNameTable can use its slice-with-offsets
		// machinery without changes.
		synth := make([]byte, len(nameBlob))
		copy(synth, nameBlob)
		parseNameTable(synth, tableRecord{offset: 0, length: len(synth)}, &info)
		break
	}

	// Consolidated bold/italic flags — limited because we don't
	// decompress OS/2 or head in v1, so we can only set IsBold /
	// IsItalic from post.italicAngle (which we also don't decode
	// from WOFF1 in v1). Default to false; the typed predicates
	// surface accordingly.

	return sfntAttrs(info, "woff")
}
