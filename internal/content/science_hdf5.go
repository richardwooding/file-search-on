package content

import (
	"context"
	"io"
	"io/fs"
)

// HDF5 (Hierarchical Data Format v5) constants per the HDF5 File
// Format Specification v3.
//
// The 8-byte signature uses the PNG-style trick of mixing DOS + Unix
// line endings and a Ctrl-Z so any line-ending translation during
// transfer corrupts the file detectably.
var hdf5Signature = []byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1A, '\n'}

const (
	hdf5SignatureLen = 8

	// hdf5ReadCap bounds disk reads. The superblock alone is ≤ 100
	// bytes for v0/v1 and 47 bytes for v2/v3 with 8-byte offsets.
	// 64 KiB is generous and reserves headroom for the future
	// root-group walk follow-up.
	hdf5ReadCap = 64 * 1024

	// hdf5MaxOffsetSize / hdf5MaxLengthSize cap the Size of Offsets
	// / Size of Lengths fields. Per the spec these are uint8 values
	// but real-world files use 8 (sometimes 4 on 32-bit-era files).
	// 16 is a generous ceiling that defends against adversarial
	// inputs claiming 255-byte addresses.
	hdf5MaxOffsetSize = 16
	hdf5MaxLengthSize = 16
)

func init() {
	Register(&hdf5Type{})
}

// hdf5Type registers the science/hdf5 content type. HDF5 is used by
// LSST, LIGO, NetCDF4 (which is built on top of HDF5), every modern
// simulation pipeline, PyTorch / NumPy checkpoint formats, and many
// scientific archives. v1 scope is superblock-only — the recursive
// object hierarchy walk (group_count / dataset_count /
// top_level_groups) is deferred because parsing v0/v1 B-trees and
// v2/v3 fractal heaps without real-world fixtures is high-risk; the
// superblock metadata alone is already enough for is_hdf5 detection
// and format-version filtering.
type hdf5Type struct{}

func (h *hdf5Type) Name() string         { return "science/hdf5" }
func (h *hdf5Type) Extensions() []string { return []string{".h5", ".hdf5", ".hdf", ".he5"} }
func (h *hdf5Type) MagicBytes() [][]byte { return [][]byte{hdf5Signature} }

// Attributes parses the HDF5 superblock at offset 0. Files that put
// the superblock at a non-zero offset (the spec allows 512 / 1024 /
// 2048 / ... but they're rare in practice) surface empty attrs but
// still detect by magic when the signature lives at offset 0.
// Corrupt / truncated headers surface empty attrs rather than
// failing the walk.
func (h *hdf5Type) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readHDF5Info(fsys, path)
}

func readHDF5Info(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, hdf5ReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseHDF5Superblock(buf), nil
}

// parseHDF5Superblock is the pure-function parser exercised by tests
// and fuzz. Accepts the file's first hdf5ReadCap bytes. Returns
// empty Attributes on any failure — the broken-file-doesn't-fail-
// the-walk contract.
//
// Layout per format version (offsets are within the superblock, not
// the file — the superblock starts at offset 0 for files we parse):
//
//	v0/v1 superblock (byte 8 = 0 or 1):
//	  0-7   8  Signature
//	  8     1  Superblock version
//	  9     1  Free-space Info version
//	  10    1  Root Group Symbol Table Entry version
//	  11    1  Reserved
//	  12    1  Shared Header Message Format version
//	  13    1  Size of Offsets (O)
//	  14    1  Size of Lengths (L)
//	  15    1  Reserved
//	  ... (variable-length fields per format version)
//
//	v2/v3 superblock (byte 8 = 2 or 3):
//	  0-7   8  Signature
//	  8     1  Version (2 or 3)
//	  9     1  Size of Offsets (O)
//	  10    1  Size of Lengths (L)
//	  11    1  File Consistency Flags
//	  ... (Base / Superblock Extension / EOF / Root Group addresses + checksum)
func parseHDF5Superblock(data []byte) Attributes {
	if len(data) < hdf5SignatureLen+1 {
		return Attributes{}
	}
	for i, b := range hdf5Signature {
		if data[i] != b {
			return Attributes{}
		}
	}
	version := data[hdf5SignatureLen]

	switch version {
	case 0, 1:
		return parseHDF5SuperblockV0V1(data, int(version))
	case 2, 3:
		return parseHDF5SuperblockV2V3(data, int(version))
	default:
		// Unknown superblock version — best-effort surfaces only what
		// we know (the magic matched, so it's an HDF5-shaped file).
		return scienceAttrs("hdf5", Attributes{
			"hdf5_format_version": int64(version),
		})
	}
}

// parseHDF5SuperblockV0V1 reads Size of Offsets / Size of Lengths
// from their v0/v1 positions (bytes 13 + 14). Both are capped
// defensively — a claimed 255-byte offset would break every
// downstream computation.
func parseHDF5SuperblockV0V1(data []byte, version int) Attributes {
	if len(data) < 15 {
		return scienceAttrs("hdf5", Attributes{
			"hdf5_format_version": int64(version),
		})
	}
	return assembleHDF5Attrs(version, int64(data[13]), int64(data[14]))
}

// parseHDF5SuperblockV2V3 reads the much more compact v2/v3 layout —
// Size of Offsets and Size of Lengths live at bytes 9 + 10 (right
// after the 1-byte version), not 13 + 14.
func parseHDF5SuperblockV2V3(data []byte, version int) Attributes {
	if len(data) < 11 {
		return scienceAttrs("hdf5", Attributes{
			"hdf5_format_version": int64(version),
		})
	}
	return assembleHDF5Attrs(version, int64(data[9]), int64(data[10]))
}

// assembleHDF5Attrs packs the superblock-derived attributes into the
// scienceAttrs envelope. Caps Size of Offsets / Lengths defensively.
func assembleHDF5Attrs(version int, offsetSize, lengthSize int64) Attributes {
	if offsetSize > hdf5MaxOffsetSize || offsetSize <= 0 {
		offsetSize = 0
	}
	if lengthSize > hdf5MaxLengthSize || lengthSize <= 0 {
		lengthSize = 0
	}
	out := Attributes{
		"hdf5_format_version": int64(version),
	}
	if offsetSize > 0 {
		out["hdf5_size_of_offsets"] = offsetSize
	}
	if lengthSize > 0 {
		out["hdf5_size_of_lengths"] = lengthSize
	}
	return scienceAttrs("hdf5", out)
}
