package content

import (
	"encoding/binary"
	"io"
	"io/fs"
	"time"
)

// pycMinHeaderSize is the minimum bytes a valid PEP 552-format .pyc
// occupies before the marshalled code object: 4 magic + 4 bit field
// + 8 metadata (timestamp+size OR hash). Legacy (pre-3.7) format is
// 12 bytes (4 magic + 4 mtime + 4 source size) or 8 bytes (4 magic
// + 4 mtime, pre-3.3).
const pycMinHeaderSize = 16

// pycHeaderReadCap caps how much we read for header parsing. The
// header itself is 16 bytes; reading a small buffer covers the
// header without slurping the entire marshalled code object.
const pycHeaderReadCap = 64

// readPYCInfo parses a Python .pyc header. Surfaces python_version,
// source_mtime (zero for hash-based PEP 552 .pyc), plus the cross-
// format bytecode_format = "python" + runtime_version (e.g.
// "Python 3.11"). The marshalled code object isn't decoded —
// pure stdlib doesn't expose a code unmarshaller and the header is
// what agents typically want anyway.
func readPYCInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	hdr := make([]byte, pycHeaderReadCap)
	n, err := io.ReadFull(f, hdr)
	// io.ErrUnexpectedEOF means the file is shorter than the cap —
	// fine, we trim and parse what we have.
	if err != nil && err != io.ErrUnexpectedEOF {
		return Attributes{}, nil //nolint:nilerr
	}
	return parsePYCHeader(hdr[:n]), nil
}

func parsePYCHeader(hdr []byte) Attributes {
	if len(hdr) < 4 {
		return Attributes{}
	}
	// Python .pyc magic is a 2-byte LE version-specific number
	// followed by "\r\n" (0x0D 0x0A). The "\r\n" trailer catches
	// accidental text-mode corruption; it's invariant across
	// Python releases, so any valid .pyc has bytes 2-3 == 0x0D 0x0A.
	if hdr[2] != 0x0D || hdr[3] != 0x0A {
		return Attributes{}
	}
	magic := binary.LittleEndian.Uint16(hdr[:2])
	version := pythonVersion(magic)
	if version == "" {
		// Unknown magic — still mark as python bytecode but skip
		// version-specific metadata parsing (the bit-field layout
		// might differ in versions we don't know).
		return bytecodeAttrs("python", "", nil)
	}

	extras := Attributes{}
	if pyVer := pythonVersionShort(magic); pyVer != "" {
		extras["python_version"] = pyVer
	}

	// PEP 552 format (Python 3.7+): bytes 4-7 are a uint32 LE bit
	// field. Bit 0 set ⇒ hash-based; clear ⇒ timestamp-based (bytes
	// 8-11 are mtime as 4-byte LE Unix seconds; bytes 12-15 are
	// source size).
	if len(hdr) >= pycMinHeaderSize && pep552(magic) {
		flags := binary.LittleEndian.Uint32(hdr[4:8])
		if flags&1 == 0 {
			// Timestamp-based — surface source_mtime.
			mtimeSecs := binary.LittleEndian.Uint32(hdr[8:12])
			if mtimeSecs > 0 {
				extras["source_mtime"] = time.Unix(int64(mtimeSecs), 0).UTC()
			}
		}
	} else if len(hdr) >= 8 {
		// Legacy format (pre-3.7): bytes 4-7 are source_mtime
		// directly.
		mtimeSecs := binary.LittleEndian.Uint32(hdr[4:8])
		if mtimeSecs > 0 {
			extras["source_mtime"] = time.Unix(int64(mtimeSecs), 0).UTC()
		}
	}

	return bytecodeAttrs("python", version, extras)
}

// pythonVersion maps a 2-byte LE magic number to a human-readable
// release string (e.g. "Python 3.11"). Coverage: 3.7-3.14 (the
// versions in common use as of 2026). Unknown magics return "".
//
// Canonical reference: CPython's
// Lib/importlib/_bootstrap_external.py MAGIC_NUMBER. Each release
// can pick multiple magic numbers across alphas/betas/RCs; we
// register the FINAL release magic of each minor version since
// agents searching for compiled artefacts care about which Python
// release wrote them.
func pythonVersion(magic uint16) string {
	switch magic {
	case 3394:
		return "Python 3.7"
	case 3413:
		return "Python 3.8"
	case 3425:
		return "Python 3.9"
	case 3439:
		return "Python 3.10"
	case 3495:
		return "Python 3.11"
	case 3531:
		return "Python 3.12"
	case 3571:
		return "Python 3.13"
	case 3627:
		return "Python 3.14"
	}
	return ""
}

// pythonVersionShort returns just the "3.11"-style version string
// (no "Python " prefix) for the python_version per-format
// attribute. Same coverage as pythonVersion.
func pythonVersionShort(magic uint16) string {
	switch magic {
	case 3394:
		return "3.7"
	case 3413:
		return "3.8"
	case 3425:
		return "3.9"
	case 3439:
		return "3.10"
	case 3495:
		return "3.11"
	case 3531:
		return "3.12"
	case 3571:
		return "3.13"
	case 3627:
		return "3.14"
	}
	return ""
}

// pep552 reports whether the given magic uses the PEP 552 hash-or-
// timestamp header format (Python 3.7+). Earlier versions used a
// simpler magic+mtime+size layout.
func pep552(magic uint16) bool {
	return magic >= 3394
}
