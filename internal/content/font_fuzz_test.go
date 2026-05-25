package content

import (
	"bytes"
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
