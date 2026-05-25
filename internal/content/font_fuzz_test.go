package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// fuzzFontInputCap bounds per-input size so a mutator can't drive a
// single exec past the worker grace window when -fuzztime expires.
// 4 KiB is enough to exercise table-directory walking, name-table
// encoding-matrix bugs, and adversarial nesting / pointer cycles
// without giving the mutator room for pathological inputs that hold
// the parser for too long inside a single Token() / decode call.
//
// Matches the cap pattern from #191 (XML / HTML fuzz tightening) and
// the bookmarks fuzz cap from #188.
const fuzzFontInputCap = 4 * 1024

// FuzzParseSFNT targets the table directory walker + per-table
// decoders. Fixed-offset big-endian parsing across all of TTF / OTF
// / CFF / glyf — exactly the territory where bounds-check bugs hide.
// Contract: never panic, attribute caps honoured.
func FuzzParseSFNT(f *testing.F) {
	// Seed 1: minimal valid TTF with name + glyf.
	f.Add(buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Test"}}),
		"glyf": []byte{0x00},
	}))

	// Seed 2: minimal valid OTF with CFF.
	f.Add(buildSFNT(sfntMagicOpenType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Test"}}),
		"CFF ": []byte{0x00},
	}))

	// Seed 3: variable font with fvar.
	f.Add(buildSFNT(sfntMagicOpenType, map[string][]byte{
		"name": buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "VarTest"}}),
		"CFF2": []byte{0x00},
		"fvar": buildFvar([]string{"wght", "wdth"}),
	}))

	// Seed 4: TTF claiming many tables (boundary check for fontMaxTableCount).
	f.Add(buildSFNT(sfntMagicTrueType, map[string][]byte{
		"name": buildName(nil),
	}))

	// Seed 5: truncated header.
	f.Add([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x05})

	// Seed 6: magic-only.
	f.Add([]byte{0x00, 0x01, 0x00, 0x00})

	// Seed 7: empty input.
	f.Add([]byte{})

	// Seed 8: all-0xFF junk.
	f.Add(bytes.Repeat([]byte{0xFF}, 256))

	// Seed 9: valid magic + claimed numTables=64 but no directory body.
	junk := make([]byte, 12)
	copy(junk[0:4], sfntMagicTrueType)
	junk[4], junk[5] = 0, 64
	f.Add(junk)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzFontInputCap {
			return
		}
		info := parseSFNT(data, 0)
		// Shape contract: surface caps honoured.
		if int64(len(info.Axes)) > fontMaxAxisCount {
			t.Fatalf("Axes count %d exceeds cap %d", len(info.Axes), fontMaxAxisCount)
		}
		if int64(len(info.UnicodeRanges)) > fontMaxUnicodeRanges {
			t.Fatalf("UnicodeRanges count %d exceeds cap %d", len(info.UnicodeRanges), fontMaxUnicodeRanges)
		}
		for _, s := range []string{info.FamilyName, info.SubfamilyName, info.Designer, info.License} {
			if len(s) > fontMaxNameStringLen {
				t.Fatalf("name string exceeds cap: len=%d", len(s))
			}
		}
		// sfntAttrs must not panic on any parser output.
		_ = sfntAttrs(info, "ttf")
	})
}

// FuzzParseWOFF2 targets the full WOFF2 extraction path: the
// variable-length table-directory walker (with UIntBase128 origLength /
// transformLength decoding), the brotli decompression hop, and the
// per-metadata-table dispatch. Brotli mutations are the highest-
// leverage attack surface — the decoder is a third-party dep and a
// malformed stream that drives the decoder into an infinite loop or
// memory blow-up would surface here. Contract: never panic.
func FuzzParseWOFF2(f *testing.F) {
	// Seed 1: minimal WOFF2 with a name table only.
	f.Add(buildWOFF2([]byte("OTTO"), []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Inter"}})},
	}))

	// Seed 2: WOFF2 with multiple metadata tables (full happy-path shape).
	os2 := make([]byte, 78)
	binary.BigEndian.PutUint16(os2[4:6], 700)
	f.Add(buildWOFF2([]byte("OTTO"), []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Bold"}})},
		{tag: "OS/2", body: os2},
	}))

	// Seed 3: WOFF2 with a glyf table (= transformVersion 3 / as-is).
	f.Add(buildWOFF2([]byte{0x00, 0x01, 0x00, 0x00}, []woff2TableBody{
		{tag: "name", body: buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "TTF"}})},
		{tag: "glyf", body: []byte{0x00}},
	}))

	// Seed 4: header-only — magic + size hints, no directory body.
	headerOnly := make([]byte, woff2HeaderLen)
	copy(headerOnly[0:4], []byte("wOF2"))
	copy(headerOnly[4:8], []byte("OTTO"))
	binary.BigEndian.PutUint32(headerOnly[16:20], 100000)
	binary.BigEndian.PutUint32(headerOnly[20:24], 35000)
	f.Add(headerOnly)

	// Seed 5: truncated header.
	f.Add([]byte{0x77, 0x4f, 0x46, 0x32}) // 'wOF2' magic + nothing

	// Seed 6: magic + claimed numTables=63 (the inline-tag sentinel
	// boundary) but no directory body.
	junk := make([]byte, woff2HeaderLen)
	copy(junk[0:4], []byte("wOF2"))
	junk[13] = 63
	f.Add(junk)

	// Seed 7: empty input.
	f.Add([]byte{})

	// Seed 8: all-0xFF junk.
	f.Add(bytes.Repeat([]byte{0xFF}, 128))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzFontInputCap {
			return
		}
		attrs := parseWOFF2(data)
		// Shape contract: caps honoured for any sfnt-derived string fields.
		for _, k := range []string{"font_family", "font_subfamily", "font_designer", "font_license"} {
			if s, ok := attrs[k].(string); ok && len(s) > fontMaxNameStringLen {
				t.Fatalf("attr %s exceeds cap: len=%d", k, len(s))
			}
		}
	})
}

// FuzzReadUIntBase128 targets the variable-length integer decoder used
// by the WOFF2 table directory walker. Contract: never panic, never
// claim to consume more bytes than were provided.
func FuzzReadUIntBase128(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x7F})
	f.Add([]byte{0x81, 0x00})
	f.Add([]byte{0xFF, 0x7F})
	f.Add([]byte{0x80, 0x01}) // leading zero — must reject
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 16 {
			return
		}
		_, n, ok := readUIntBase128(data)
		if ok && n > len(data) {
			t.Fatalf("readUIntBase128 claimed to consume %d bytes from %d-byte input", n, len(data))
		}
		if !ok && n != 0 {
			t.Fatalf("readUIntBase128 returned ok=false but n=%d (must be 0)", n)
		}
	})
}

// FuzzParseNameTable targets the encoding-matrix-aware name decoder
// directly. The (platformID, encodingID, languageID) priority logic
// + UTF-16BE decoding are the highest-leverage targets — anyone
// touching parseNameTable later will get bitten by them.
func FuzzParseNameTable(f *testing.F) {
	// Valid name table with one record.
	f.Add(buildName([]testNameRecord{{3, 1, 0x409, nameIDFamily, "Test"}}))

	// Valid with multiple records across encodings (priority test).
	f.Add(buildName([]testNameRecord{
		{1, 0, 0, nameIDFamily, "MacRoman"},
		{3, 1, 0x409, nameIDFamily, "WinUnicodeEN"},
		{3, 1, 0x40c, nameIDFamily, "WinUnicodeFR"},
	}))

	// Truncated header.
	f.Add([]byte{0x00, 0x00, 0x00, 0x05})

	// Empty.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzFontInputCap {
			return
		}
		info := sfntInfo{Present: true}
		// Synthesise a tableRecord pointing at the whole input.
		parseNameTable(data, tableRecord{offset: 0, length: len(data)}, &info)
		// Same string caps contract.
		for _, s := range []string{info.FamilyName, info.SubfamilyName, info.Designer, info.License} {
			if len(s) > fontMaxNameStringLen {
				t.Fatalf("name string exceeds cap: len=%d", len(s))
			}
		}
	})
}
