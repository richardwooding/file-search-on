package content

import (
	"context"
	"io/fs"
)

// RAW camera photo content types. file-search-on supports 8 of the most
// widely-deployed RAW formats — Canon CR2 + CR3, Nikon NEF (+ NRW alias),
// Sony ARW (+ SRF / SR2 aliases), Adobe DNG, Fujifilm RAF, Olympus ORF,
// Panasonic RW2.
//
// Detection: the registry's longest-suffix extension pass wins over magic
// sniffing, so a `.cr2` file dispatches to `image/raw-cr2` via the
// extension. TIFF-based RAW types (CR2 / NEF / ARW / DNG / RW2 / ORF
// standard variant) deliberately do NOT register the shared TIFF magic —
// it's already claimed by `image/tiff`, and duplicate magic registration
// makes the magic-fallback dispatch ambiguous. Files with stripped
// extensions fall through to `image/tiff` (acceptable; same as the
// pre-RAW behaviour). RAF (`FUJIFILMCCD-RAW`) and ORF's vendor-specific
// variants (`IIRO` / `IIRS` / `MMOR`) DO register magic — those are
// unique prefixes with no conflict.
//
// EXIF extraction: routes through the same `imagemeta.Decode` path used
// by JPEG / PNG / TIFF / HEIC via the shared `extractImageEXIF` helper
// in imagetype.go. The `evanoberholster/imagemeta` v1.0.0 README lists
// CR2 / CR3 / NEF / ARW / DNG / RAF / ORF / RW2 in its supported-formats
// section — camera_make / camera_model / lens / taken_at / GPS / ISO /
// focal_length / f_stop / exposure_time / orientation populate
// automatically.
//
// Issue #196.

func init() {
	for _, t := range rawImageTypes {
		Register(t)
	}
}

// rawImageType is one RAW format registration. All eight formats share
// the same struct + Attributes method; per-format config (name, kind,
// vendor, extensions, magic) lives in `rawImageTypes` below. Modelled
// after sourcetype.go's sourceType.
type rawImageType struct {
	name   string   // content_type, e.g. "image/raw-cr2"
	kind   string   // raw_kind, e.g. "cr2"
	vendor string   // raw_vendor, e.g. "canon"
	exts   []string // file extensions
	magic  [][]byte // magic-byte prefixes (nil = extension-only dispatch)
}

func (r *rawImageType) Name() string         { return r.name }
func (r *rawImageType) Extensions() []string { return r.exts }
func (r *rawImageType) MagicBytes() [][]byte { return r.magic }

func (r *rawImageType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{
		"raw_kind":   r.kind,
		"raw_vendor": r.vendor,
	}
	rs, _, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = closer() }()
	extractImageEXIF(rs, attrs)
	return attrs, nil
}

// rawImageTypes is the registry of supported RAW formats. Adding a new
// vendor / format is one struct literal plus the matching switch case
// in setTypeFlags (internal/celexpr/evaluator.go) and the AttributeDoc
// in schema.go.
var rawImageTypes = []*rawImageType{
	{
		name:   "image/raw-cr2",
		kind:   "cr2",
		vendor: "canon",
		exts:   []string{".cr2"},
		magic:  nil,
	},
	{
		// CR3 is an ISO Base Media File Format container with `ftyp` at
		// offset 4 and the brand `crx ` at offset 8 — neither sits at
		// byte 0, so detection is extension-only (matches the HEIC
		// pattern in imagetype.go).
		name:   "image/raw-cr3",
		kind:   "cr3",
		vendor: "canon",
		exts:   []string{".cr3"},
		magic:  nil,
	},
	{
		name:   "image/raw-nef",
		kind:   "nef",
		vendor: "nikon",
		// NRW is Nikon's Coolpix small-sensor variant — same TIFF shape,
		// shares the parser path.
		exts:  []string{".nef", ".nrw"},
		magic: nil,
	},
	{
		name:   "image/raw-arw",
		kind:   "arw",
		vendor: "sony",
		// SRF / SR2 are older Sony Alpha / Cyber-shot RAW; same TIFF
		// shape, share the parser path.
		exts:  []string{".arw", ".srf", ".sr2"},
		magic: nil,
	},
	{
		name:   "image/raw-dng",
		kind:   "dng",
		vendor: "adobe",
		exts:   []string{".dng"},
		magic:  nil,
	},
	{
		name:   "image/raw-raf",
		kind:   "raf",
		vendor: "fujifilm",
		exts:   []string{".raf"},
		// Fuji RAF has a unique 15-byte magic 'FUJIFILMCCD-RAW' at
		// offset 0 — the most discriminative of the RAW magics, no
		// conflict with any other registered type.
		magic: [][]byte{[]byte("FUJIFILMCCD-RAW")},
	},
	{
		name:   "image/raw-orf",
		kind:   "orf",
		vendor: "olympus",
		exts:   []string{".orf", ".ori"},
		// Olympus uses TIFF-shaped headers with vendor-specific magic
		// words `IIRO` / `IIRS` (little-endian) and `MMOR` (big-endian)
		// in place of the standard `II*\0` / `MM\0*`. These are unique
		// to ORF — no conflict with image/tiff. ORF files written with
		// standard TIFF magic fall through to image/tiff on a stripped
		// extension; that's acceptable.
		magic: [][]byte{
			[]byte("IIRO"),
			[]byte("IIRS"),
			[]byte("MMOR"),
		},
	},
	{
		name:   "image/raw-rw2",
		kind:   "rw2",
		vendor: "panasonic",
		// Some older Panasonic cameras still ship `.raw` — the
		// longest-suffix detector tries `.rw2` first, then falls back to
		// `.raw` if no other type owns that extension. Reusing `.raw`
		// here means generic `.raw` dumps from non-Panasonic sources
		// will be misattributed; small cost — Panasonic is the dominant
		// `.raw` producer.
		exts:  []string{".rw2", ".raw"},
		magic: nil,
	},
}
