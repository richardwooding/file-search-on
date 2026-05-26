package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"testing/fstest"
)

// FuzzParseSTL targets the STL parser — the binary-vs-ascii decision,
// the 84+50·n size formula, the binary triangle stream walker, and the
// ascii line scanner. Hand-rolled binary header walking + float
// decoding is exactly where bounds bugs hide. Contract: never panic.
func FuzzParseSTL(f *testing.F) {
	// Valid binary STL, 2 triangles.
	bin := make([]byte, 84+2*50)
	binary.LittleEndian.PutUint32(bin[80:84], 2)
	f.Add(bin)

	// ASCII STL.
	f.Add([]byte("solid s\nfacet normal 0 0 1\n outer loop\n  vertex 0 0 0\n  vertex 1 0 0\n  vertex 0 1 0\n endloop\nendfacet\nendsolid s\n"))

	// Binary header claiming a huge triangle count (size mismatch →
	// treated as ascii → scanned as text).
	huge := make([]byte, 84)
	binary.LittleEndian.PutUint32(huge[80:84], 0xFFFFFFFF)
	f.Add(huge)

	// Truncated header.
	f.Add([]byte{0x73, 0x6f, 0x6c})

	// Empty.
	f.Add([]byte{})

	// All-0xFF junk.
	f.Add(bytes.Repeat([]byte{0xFF}, 200))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		fsys := fstest.MapFS{"m.stl": {Data: data}}
		// Must not panic; attrs / error both acceptable.
		_, _ = parseSTL(context.Background(), fsys, "m.stl")
	})
}

// FuzzReadGLBJSONChunk targets the GLB binary container walker — the
// 12-byte header, chunk-length / chunk-type decoding, and the bounded
// JSON-chunk read. Adversarial lengths (oversized, zero, mismatched)
// are the high-leverage inputs. Contract: never panic, never read
// past the declared chunk.
func FuzzReadGLBJSONChunk(f *testing.F) {
	// Valid minimal GLB with a tiny JSON chunk.
	jsonChunk := []byte(`{"asset":{}}`)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	var glb []byte
	glb = append(glb, []byte("glTF")...)
	glb = binary.LittleEndian.AppendUint32(glb, 2)
	glb = binary.LittleEndian.AppendUint32(glb, uint32(12+8+len(jsonChunk)))
	glb = binary.LittleEndian.AppendUint32(glb, uint32(len(jsonChunk)))
	glb = binary.LittleEndian.AppendUint32(glb, 0x4E4F534A)
	glb = append(glb, jsonChunk...)
	f.Add(glb)

	// Header only.
	f.Add([]byte("glTF\x02\x00\x00\x00\x00\x00\x00\x00"))

	// Chunk claiming a gigantic length.
	big := append([]byte("glTF"), make([]byte, 8)...)
	big = binary.LittleEndian.AppendUint32(big, 0xFFFFFFFF) // chunk len
	big = binary.LittleEndian.AppendUint32(big, 0x4E4F534A) // JSON
	f.Add(big)

	// Wrong magic.
	f.Add([]byte("XXXX\x02\x00\x00\x00"))

	// Empty.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		fsys := fstest.MapFS{"m.glb": {Data: data}}
		b, ok := readGLBJSONChunk(fsys, "m.glb")
		if ok && len(b) > glbMaxJSONChunk {
			t.Fatalf("readGLBJSONChunk returned %d bytes, exceeds cap %d", len(b), glbMaxJSONChunk)
		}
	})
}
