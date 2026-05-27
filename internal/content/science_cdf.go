package content

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
)

// CDF (Common Data Format) constants per the CDF v3.8 Internal
// Format Description (NASA/SP-2024-001). Used by NASA heliophysics
// missions — ACE, Wind, Cluster, MMS, Parker Solar Probe, Solar
// Orbiter — for space-physics time-series data.
const (
	// cdfReadCap bounds the initial chunk read off disk. The CDR is
	// 312 bytes; the GDR (at GDROffset, often near the start of the
	// file) is 84 bytes minimum. 64 KiB covers both for typical
	// files; larger files fall back to Seek when fs.FS exposes it.
	cdfReadCap = 64 * 1024

	// cdfMagicV3 is the 4-byte big-endian file signature for CDF
	// v3.0 and later — `CDF` + version 3 + magic byte 1.
	cdfMagicV3 = 0xCDF30001

	cdfRecordTypeCDR = 1
	cdfRecordTypeGDR = 2

	// CDR flags bit positions per CDF Internal Format Description
	// §2.1. bit 0 = single-file; bit 1 = row-majority; bit 2 = MD5
	// checksum on dotCDF; bit 3 = MD5 of file present.
	cdfFlagMajorityRow = 0x2
)

func init() {
	Register(&cdfType{})
}

// cdfType registers the science/cdf content type. CDF files use a
// deterministic 4-byte big-endian magic at offset 0 (`0xCDF30001`
// for v3+), which the first-512-byte sniffer catches.
type cdfType struct{}

func (c *cdfType) Name() string         { return "science/cdf" }
func (c *cdfType) Extensions() []string { return []string{".cdf"} }
func (c *cdfType) MagicBytes() [][]byte {
	return [][]byte{
		{0xCD, 0xF3, 0x00, 0x01}, // v3+ magic, big-endian
	}
}

// Attributes parses the CDR at offset 0 and, if reachable, the GDR
// it points at. v1 surfaces version + encoding + majority +
// variable_count + attribute_count. Walking the ADR linked list
// for ISTP-convention global attributes (TITLE / PI_name / etc.)
// is deferred to a follow-up.
func (c *cdfType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readCDFInfo(fsys, path)
}

func readCDFInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf, err := io.ReadAll(io.LimitReader(f, cdfReadCap))
	if err != nil {
		return Attributes{}, nil
	}

	attrs, gdrOffset := parseCDFCDR(buf)
	if attrs == nil {
		return Attributes{}, nil
	}

	// Try to read GDR for variable_count + attribute_count. First
	// path: GDR within the initial read window. Second path: seek
	// to gdrOffset if the underlying file is seekable (real
	// os.DirFS files are; some test fs.FS implementations aren't).
	if gdrOffset > 0 {
		switch {
		case int64(len(buf)) >= gdrOffset+64:
			mergeCDFGDR(attrs, buf[gdrOffset:])
		default:
			if seeker, ok := f.(io.Seeker); ok {
				if _, err := seeker.Seek(gdrOffset, io.SeekStart); err == nil {
					gdrBuf := make([]byte, 64)
					if n, _ := io.ReadFull(f, gdrBuf); n >= 64 {
						mergeCDFGDR(attrs, gdrBuf[:n])
					}
				}
			}
		}
	}

	return scienceAttrs("cdf", attrs), nil
}

// parseCDFCDR reads the CDR fields we care about from a buffer that
// starts at file offset 0. Returns the per-field attributes (NOT
// yet wrapped in scienceAttrs) plus the absolute file offset of the
// GDR. Returns (nil, 0) on magic mismatch.
//
// Pure function — fuzz target FuzzParseCDFHeader exercises it.
func parseCDFCDR(data []byte) (Attributes, int64) {
	if len(data) < 4 {
		return nil, 0
	}
	if binary.BigEndian.Uint32(data[0:4]) != cdfMagicV3 {
		return nil, 0
	}

	// Need bytes 4-51 to read RecordSize, RecordType, GDROffset,
	// Version, Release, Encoding, Flags, rfuA, rfuB, Increment.
	if len(data) < 52 {
		return Attributes{"cdf_version": "3.x"}, 0
	}
	if int32(binary.BigEndian.Uint32(data[12:16])) != cdfRecordTypeCDR {
		// File magic matched but the CDR record-type byte didn't —
		// surface a coarse cdf_version anyway since detection fired.
		return Attributes{"cdf_version": "3.x"}, 0
	}

	gdrOffset := int64(binary.BigEndian.Uint64(data[16:24]))
	version := int32(binary.BigEndian.Uint32(data[24:28]))
	release := int32(binary.BigEndian.Uint32(data[28:32]))
	encoding := int32(binary.BigEndian.Uint32(data[32:36]))
	flags := int32(binary.BigEndian.Uint32(data[36:40]))
	increment := int32(binary.BigEndian.Uint32(data[48:52]))

	out := Attributes{
		"cdf_version": fmt.Sprintf("%d.%d.%d", version, release, increment),
	}
	if enc := cdfEncodingName(encoding); enc != "" {
		out["cdf_encoding"] = enc
	}
	if flags&cdfFlagMajorityRow != 0 {
		out["cdf_majority"] = "row"
	} else {
		out["cdf_majority"] = "column"
	}

	// Sanity-cap. GDR offset must be positive and beyond the CDR.
	// Negative / zero / out-of-bounds values disable the GDR read.
	if gdrOffset <= 0 {
		gdrOffset = 0
	}
	return out, gdrOffset
}

// mergeCDFGDR walks the GDR (Global Descriptor Record) fields we
// surface — NrVars + NzVars (combined into variable_count) and
// NumAttr (attribute_count). gdrData must hold at least the first
// 64 bytes of the GDR.
//
// Pure function — fuzz target exercises it.
func mergeCDFGDR(out Attributes, gdrData []byte) {
	if len(gdrData) < 64 {
		return
	}
	if int32(binary.BigEndian.Uint32(gdrData[8:12])) != cdfRecordTypeGDR {
		return
	}
	nrVars := int32(binary.BigEndian.Uint32(gdrData[44:48]))
	numAttr := int32(binary.BigEndian.Uint32(gdrData[48:52]))
	nzVars := int32(binary.BigEndian.Uint32(gdrData[60:64]))
	if nrVars < 0 {
		nrVars = 0
	}
	if nzVars < 0 {
		nzVars = 0
	}
	if numAttr < 0 {
		numAttr = 0
	}
	out["variable_count"] = int64(nrVars) + int64(nzVars)
	out["attribute_count"] = int64(numAttr)
}

// cdfEncodingName maps the CDR encoding integer to the canonical
// short name used by the CDF reference implementation. Covers every
// encoding value defined in the CDF v3.8 IFD; unknown values pass
// through as the empty string so the cdf_encoding attribute is just
// absent rather than surfaced as a bare integer.
func cdfEncodingName(e int32) string {
	switch e {
	case 1:
		return "network"
	case 2:
		return "sun"
	case 3:
		return "vax"
	case 4:
		return "decstation"
	case 5:
		return "sgi"
	case 6:
		return "ibmpc"
	case 7:
		return "ibmrs"
	case 9:
		return "mac"
	case 11:
		return "hp"
	case 12:
		return "next"
	case 13:
		return "alpha-osf1"
	case 14:
		return "alpha-vms-d"
	case 15:
		return "alpha-vms-g"
	case 16:
		return "alpha-vms-i"
	case 17:
		return "arm-little"
	case 18:
		return "arm-big"
	}
	return ""
}
