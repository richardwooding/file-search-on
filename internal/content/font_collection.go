package content

import (
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"sort"
)

// TrueType / OpenType Collection (.ttc / .otc). One file holding N
// embedded sfnts (typically variations of the same family — e.g.
// macOS ships Helvetica.ttc with ~12 weight + style combinations).
// Layout per OpenType spec §10:
//
//	TTC header:
//	  ttcTag       u32   'ttcf'
//	  version      u32   0x00010000 (v1) or 0x00020000 (v2 — adds DSIG)
//	  numFonts     u32
//	  offsetTable  u32[numFonts]   file offsets to each member sfnt
//	  ...DSIG fields (v2 only, not parsed)
//
// We walk the offset table, parse each member via parseSFNT, surface:
//
//   - The PRIMARY (first member's) family / subfamily / etc. via the
//     standard font_* attrs — agents asking "what font is this" get
//     the first member.
//   - font_collection_count (int) — number of members.
//   - font_collection_families (list<str>) — sorted unique family
//     names across all members (capped at 32).
//
// Defensive: cap member count (fontMaxCollectionSize), refuse member
// offsets that overlap the TTC header itself (a classic adversarial
// shape), and treat any parser failure as "skip this member, keep going".

func init() {
	Register(&fontCollectionType{})
}

type fontCollectionType struct{}

func (*fontCollectionType) Name() string         { return "font/collection" }
func (*fontCollectionType) Extensions() []string { return []string{".ttc", ".otc"} }
func (*fontCollectionType) MagicBytes() [][]byte { return [][]byte{[]byte("ttcf")} }

func (*fontCollectionType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
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
	return parseTTC(buf), nil
}

// parseTTC walks the TTC header + per-member sfnts. Returns the
// merged attribute map. Surfaces format = "ttc" or "otc" based on
// the first member's outline kind (cff / cff2 → otc; truetype → ttc).
//
// Pure function — fuzz target reachable.
func parseTTC(data []byte) Attributes {
	if len(data) < 12 {
		return Attributes{}
	}
	// Magic + version + numFonts (12 bytes total).
	if string(data[0:4]) != "ttcf" {
		return Attributes{}
	}
	numFonts := int(binary.BigEndian.Uint32(data[8:12]))
	if numFonts == 0 || numFonts > fontMaxCollectionSize {
		return Attributes{}
	}
	offsetTableEnd := 12 + numFonts*4
	if offsetTableEnd > len(data) {
		return Attributes{}
	}

	familySet := make(map[string]struct{}, numFonts)
	var primary sfntInfo
	for i := range numFonts {
		off := int(binary.BigEndian.Uint32(data[12+i*4 : 16+i*4]))
		// Refuse members whose offsets dive back into the TTC header
		// (a classic adversarial loop shape) — the first valid sfnt
		// position is past the offset table.
		if off < offsetTableEnd || off >= len(data) {
			continue
		}
		info := parseSFNT(data, off)
		if !info.Present {
			continue
		}
		if i == 0 {
			primary = info
		}
		if info.FamilyName != "" {
			familySet[info.FamilyName] = struct{}{}
		}
		if len(familySet) >= fontMaxNameRecords {
			break
		}
	}

	// Format hint: distinguishes ttc (TrueType outlines) from otc
	// (CFF outlines). Uses the first member's outline kind.
	format := "ttc"
	if primary.OutlineKind == "cff" || primary.OutlineKind == "cff2" {
		format = "otc"
	}
	out := sfntAttrs(primary, format)
	out["font_collection_count"] = int64(numFonts)
	if len(familySet) > 0 {
		families := make([]string, 0, len(familySet))
		for n := range familySet {
			families = append(families, n)
		}
		sort.Strings(families)
		if len(families) > 32 {
			families = families[:32]
		}
		out["font_collection_families"] = families
	}
	return out
}
