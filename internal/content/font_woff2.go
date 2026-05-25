package content

import (
	"context"
	"encoding/binary"
	"io"
	"io/fs"
)

// WOFF2 (Web Open Font Format v2, W3C TR 2018). Brotli-compressed
// wrapper around an sfnt with additional transformed-table encoding
// for `glyf` / `loca` / `hmtx`.
//
// v1 of file-search-on is DETECT-ONLY for WOFF2 — we surface
// `font_format = "woff2"` plus the header byte counts but DO NOT
// extract name / OS/2 / fvar attributes. Full WOFF2 extraction
// requires:
//
//   - brotli decompression dep (github.com/andybalholm/brotli — pure-Go,
//     ~6 KB compiled but a new module)
//   - implementing the WOFF2 transformed-table encoding for glyf /
//     loca / hmtx to reconstruct the underlying sfnt
//
// Tracked as a follow-up issue. Filed at the same time as this v1 PR
// ships so the deferral has a clear path forward; web designers
// querying .woff2 collections will see only `is_woff2` + the header
// counts in v1 (clearly called out in README + examples).
//
// Layout per https://www.w3.org/TR/WOFF2/ §4.1:
//
//	WOFF2 header:
//	  signature             u32   'wOF2'
//	  flavor                u32   underlying sfnt's scaler version
//	  length                u32   total file size
//	  numTables             u16
//	  reserved              u16
//	  totalSfntSize         u32   uncompressed sfnt size
//	  totalCompressedSize   u32   compressed data block size
//	  majorVersion          u16
//	  minorVersion          u16
//	  metaOffset            u32
//	  metaLength            u32
//	  metaOrigLength        u32
//	  privOffset            u32
//	  privLength            u32
//
// Issue #197 + WOFF2 follow-up.

const woff2HeaderLen = 48

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
	buf, err := io.ReadAll(io.LimitReader(f, woff2HeaderLen))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseWOFF2Header(buf), nil
}

// parseWOFF2Header decodes just the 48-byte WOFF2 header. Surfaces:
//   - font_format: "woff2"
//   - font_outline_kind: derived from flavor field (TTF/OTF/etc.)
//   - woff2_total_sfnt_size / woff2_total_compressed_size as size hints
//
// Skips the table directory + brotli payload entirely. Defer to the
// follow-up issue for full extraction.
func parseWOFF2Header(data []byte) Attributes {
	if len(data) < woff2HeaderLen {
		return Attributes{}
	}
	if string(data[0:4]) != "wOF2" {
		return Attributes{}
	}
	out := Attributes{
		"font_format": "woff2",
	}
	// Flavor field at offset 4: scaler version of underlying sfnt.
	// 0x00010000 = TrueType, 'OTTO' = OpenType-CFF. Surface as the
	// outline-kind hint so agents can still discriminate.
	flavor := data[4:8]
	switch {
	case string(flavor) == "OTTO":
		out["font_outline_kind"] = "cff"
	case flavor[0] == 0x00 && flavor[1] == 0x01 && flavor[2] == 0x00 && flavor[3] == 0x00:
		out["font_outline_kind"] = "truetype"
	}
	out["woff2_total_sfnt_size"] = int64(binary.BigEndian.Uint32(data[16:20]))
	out["woff2_total_compressed_size"] = int64(binary.BigEndian.Uint32(data[20:24]))
	return out
}
