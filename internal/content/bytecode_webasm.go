package content

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// WASM section IDs (binary spec §5.5). We only care about import,
// function, export — the per-section count fields are what we
// surface.
const (
	wasmSectionCustom   = 0
	wasmSectionType     = 1
	wasmSectionImport   = 2
	wasmSectionFunction = 3
	wasmSectionTable    = 4
	wasmSectionMemory   = 5
	wasmSectionGlobal   = 6
	wasmSectionExport   = 7
	wasmSectionStart    = 8
	wasmSectionElement  = 9
	wasmSectionCode     = 10
	wasmSectionData     = 11
	wasmSectionDataCount = 12
)

// wasmReadCap is a sanity ceiling on header walking. Real WASM
// modules can be tens of MB but the section-count metadata we
// surface only needs the leading import/export sections; 16 MiB is
// a generous upper bound.
const wasmReadCap = 16 * 1024 * 1024

// wasmMaxSections caps how many sections we'll walk per module. A
// well-formed module has < 20 sections (one per type ID); we cap at
// 128 to defend against malicious section spam.
const wasmMaxSections = 128

// wasmMaxImportsExports caps the per-section vector length we'll
// read just to count entries. Real-world modules go up to ~10k
// imports for binding-heavy WebGPU code; cap at 1 million for
// safety while staying well above legitimate values.
const wasmMaxImportsExports = 1_000_000

// readWASMInfo parses a WebAssembly module header per the WASM
// binary spec (https://webassembly.github.io/spec/core/binary/).
//
// Header layout:
//
//	0x00 [4]    Magic = "\0asm"
//	0x04 [4]    Version (LE uint32; currently 1)
//	0x08 ...    Sections (each: u8 id + LEB128 size + payload)
//
// Surfaces wasm_version (int), section_count (int), import_count
// (int), export_count (int), plus the cross-format
// bytecode_format = "wasm" + runtime_version (e.g.
// "WebAssembly 1.0").
func readWASMInfo(fsys fs.FS, path string) (Attributes, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, wasmReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parseWASMModule(buf), nil
}

func parseWASMModule(data []byte) Attributes {
	if len(data) < 8 {
		return Attributes{}
	}
	if !bytes.Equal(data[0:4], []byte{0x00, 0x61, 0x73, 0x6D}) {
		return Attributes{}
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	runtimeVer := fmt.Sprintf("WebAssembly %d.0", version)
	if version == 0 {
		runtimeVer = ""
	}

	r := &wasmReader{data: data[8:]}
	var (
		sectionCount int64
		importCount  int64
		exportCount  int64
	)
	for sectionCount < wasmMaxSections {
		if r.eof() {
			break
		}
		id, ok := r.u8()
		if !ok {
			break
		}
		size, ok := r.uvarint()
		if !ok || size > uint64(len(r.data)-r.pos) {
			break
		}
		sectionCount++
		// We need to peek inside import / export sections to count
		// their entries. Other sections we skip — the size prefix
		// lets us advance without parsing the payload.
		switch id {
		case wasmSectionImport:
			if n, ok := readWasmVectorCount(r, int(size)); ok {
				importCount = n
			} else {
				r.advance(int(size))
			}
		case wasmSectionExport:
			if n, ok := readWasmVectorCount(r, int(size)); ok {
				exportCount = n
			} else {
				r.advance(int(size))
			}
		default:
			r.advance(int(size))
		}
	}

	return bytecodeAttrs("wasm", runtimeVer, Attributes{
		"wasm_version":  int64(version),
		"section_count": sectionCount,
		"import_count":  importCount,
		"export_count":  exportCount,
	})
}

// readWasmVectorCount peeks the first uvarint of a section payload
// (which is the vector length for import / export sections) without
// consuming the rest of the section. The caller advances past the
// remainder via the size prefix.
func readWasmVectorCount(r *wasmReader, sectionSize int) (int64, bool) {
	if sectionSize <= 0 || r.pos+sectionSize > len(r.data) {
		return 0, false
	}
	// Snapshot the cursor so we can advance to the section end
	// regardless of whether the uvarint parse succeeded.
	start := r.pos
	end := start + sectionSize
	count, ok := r.uvarint()
	// Always position the reader at the section end so the next
	// iteration sees a fresh section.
	r.pos = end
	if !ok || count > wasmMaxImportsExports {
		return 0, false
	}
	return int64(count), true
}

// wasmReader is a bounds-checking cursor over a WASM byte buffer
// starting at the first byte AFTER the 8-byte header. Reads return
// (value, false) on EOF.
type wasmReader struct {
	data []byte
	pos  int
}

func (r *wasmReader) eof() bool { return r.pos >= len(r.data) }

func (r *wasmReader) u8() (uint8, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	v := r.data[r.pos]
	r.pos++
	return v, true
}

func (r *wasmReader) advance(n int) bool {
	if n < 0 || r.pos+n > len(r.data) {
		return false
	}
	r.pos += n
	return true
}

// uvarint reads an LEB128-encoded unsigned integer (WASM's variable-
// length encoding). The WASM spec caps unsigned-32 LEB128 at 5 bytes
// and unsigned-64 at 10; we cap at 10 bytes total to defend against
// pathological inputs.
//
// Returns (value, false) on overlong / truncated / overflowing input.
var errLEB128Overflow = errors.New("LEB128 overflow")

func (r *wasmReader) uvarint() (uint64, bool) {
	var result uint64
	var shift uint
	const maxBytes = 10
	for i := range maxBytes {
		b, ok := r.u8()
		if !ok {
			return 0, false
		}
		result |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, true
		}
		shift += 7
		if shift >= 64 {
			_ = i // unreachable in practice due to maxBytes cap
			return 0, false
		}
	}
	// Beyond 10 bytes — overflow.
	_ = errLEB128Overflow
	return 0, false
}
