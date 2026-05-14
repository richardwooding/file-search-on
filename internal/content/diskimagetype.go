package content

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"time"
)

func init() {
	Register(&diskImageType{
		name: "disk-image/dmg",
		exts: []string{".dmg"},
		// koly trailer lives at EOF — start-of-file sniffer can't see
		// it, so this type is extension-only (same pattern as TAR).
		magic: nil,
	})
	Register(&diskImageType{
		name: "disk-image/iso9660",
		exts: []string{".iso"},
		// "CD001" lives at offset 0x8001 (system area + primary volume
		// descriptor) — out of reach of the 512-byte prefix sniffer.
		magic: nil,
	})
	Register(&diskImageType{
		name: "disk-image/vhd",
		exts: []string{".vhd"},
		// "conectix" footer lives at EOF.
		magic: nil,
	})
	Register(&diskImageType{
		name:  "disk-image/vhdx",
		exts:  []string{".vhdx"},
		magic: [][]byte{[]byte("vhdxfile")},
	})
	Register(&diskImageType{
		name: "disk-image/vmdk",
		exts: []string{".vmdk"},
		// "KDMV" magic only fires for the sparse-extent binary form;
		// descriptor-only `.vmdk` files are plain text and fall
		// through to the text content type.
		magic: [][]byte{{'K', 'D', 'M', 'V'}},
	})
	Register(&diskImageType{
		name:  "disk-image/qcow2",
		exts:  []string{".qcow2", ".qcow"},
		magic: [][]byte{{'Q', 'F', 'I', 0xFB}},
	})
	Register(&diskImageType{
		name:  "disk-image/wim",
		exts:  []string{".wim"},
		magic: [][]byte{{'M', 'S', 'W', 'I', 'M', 0, 0, 0}},
	})
}

type diskImageType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (d *diskImageType) Name() string         { return d.name }
func (d *diskImageType) Extensions() []string { return d.exts }
func (d *diskImageType) MagicBytes() [][]byte { return d.magic }

// Attributes dispatches to a per-format disk-image parser. Each parser
// reads the format's header (or footer) only — no payload walk — and
// surfaces the cross-format `disk_image_format` + `virtual_size` plus
// whatever extra metadata the format makes cheap to read.
//
// All parsers degrade gracefully: a corrupt or truncated header returns
// empty attrs + nil error so the walker keeps moving. ctx is checked at
// entry; none of the parsers loop unboundedly (they all do fixed-offset
// reads), so a single entry-point check is sufficient.
func (d *diskImageType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch d.name {
	case "disk-image/dmg":
		return readDMGInfo(fsys, path)
	case "disk-image/iso9660":
		return readISO9660Info(fsys, path)
	case "disk-image/vhd":
		return readVHDInfo(fsys, path)
	case "disk-image/vhdx":
		return readVHDXInfo(fsys, path)
	case "disk-image/vmdk":
		return readVMDKInfo(fsys, path)
	case "disk-image/qcow2":
		return readQCOW2Info(fsys, path)
	case "disk-image/wim":
		return readWIMInfo(fsys, path)
	}
	return nil, errors.New("unsupported disk-image type")
}

// diskImageAttrs packs the cross-format surface (always present:
// disk_image_format + virtual_size) plus any extras. Per-format
// callers add `disk_type`, `volume_label`, `cluster_bits`,
// `is_encrypted`, `image_count`, `created_at` on top via extras.
// Mirrors archiveAttrs / binaryAttrs.
func diskImageAttrs(format string, virtualSize int64, extras Attributes) Attributes {
	out := Attributes{
		"disk_image_format": format,
		"virtual_size":      virtualSize,
	}
	maps.Copy(out, extras)
	return out
}

// zeroTime is the package-level "no date" sentinel. Per-format parsers
// return this when the format declares no creation timestamp; the CEL
// activation populates `created_at` with the zero time so filters like
// `created_at > timestamp("2020-01-01T00:00:00Z")` work uniformly.
var zeroTime = time.Time{}
