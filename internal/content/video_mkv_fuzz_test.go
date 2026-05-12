package content

import (
	"bytes"
	"testing"
)

// FuzzReadMKVInfo targets the hand-rolled EBML parser
// (readMKVInfo / readMKVTrackEntry / walkEBML / readVINTRaw). EBML
// uses variable-length integers and is structurally recursive — the
// kind of format where off-by-one and integer-overflow bugs hide.
//
// Contract: never panic, never loop forever. The fuzzer mutates from
// a tiny "EBML header" seed and from the empty buffer; with the
// permissive content type that doesn't care about correctness, this
// reaches deep into the parse tree quickly.
func FuzzReadMKVInfo(f *testing.F) {
	f.Add([]byte{})
	// Minimal EBML magic (0x1A45DFA3) followed by a size byte and a
	// few payload bytes. Not a valid file, just a place to start.
	f.Add([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x81, 0x00})
	// Magic + an empty segment.
	f.Add([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x81, 0x00, 0x18, 0x53, 0x80, 0x67, 0x80})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = readMKVInfo(r, int64(len(data)))
	})
}
