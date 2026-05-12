package content

import (
	"bytes"
	"testing"
)

// FuzzReadMP3Info feeds arbitrary bytes into the hand-rolled MP3
// header parser (ID3v2 skip + MPEG frame header + Xing/Info VBR tag).
// Contract: never panic; never read past the (file_size) we declare.
// The fuzzer crafts adversarial inputs that exercise frame-sync
// detection, Xing tag offsets, and ID3v2 size-with-syncsafe parsing.
//
// Bug class this catches: integer overflow on the ID3v2 size header
// (28-bit syncsafe), out-of-bounds reads on the Xing tag offset, and
// infinite-loop conditions in the frame-resync logic.
func FuzzReadMP3Info(f *testing.F) {
	// Seeds: an empty buffer, an ID3v2 header followed by garbage, a
	// minimal but parseable MPEG audio frame header (FF FB), and a
	// Xing tag prefix. None has to be "valid" — they just give the
	// fuzzer realistic starting points to mutate.
	f.Add([]byte{})
	f.Add([]byte("ID3\x04\x00\x00\x00\x00\x00\x0a" + "padding..."))
	f.Add([]byte{0xff, 0xfb, 0x90, 0x00})
	f.Add(append([]byte{0xff, 0xfb, 0x90, 0x00},
		// 32 bytes of padding then a "Xing" marker.
		append(make([]byte, 32), 'X', 'i', 'n', 'g')...))

	f.Fuzz(func(t *testing.T, data []byte) {
		// readMP3Info takes an io.ReadSeeker — wrap the fuzz bytes
		// in a bytes.Reader, which satisfies both Read and Seek.
		// Declare fileSize as len(data) so the parser stays bounded
		// to the supplied buffer; mismatched sizes are the parser's
		// problem and a legitimate fuzz target.
		r := bytes.NewReader(data)
		_, _ = readMP3Info(r, int64(len(data)))
		// We don't assert on outputs — the contract is "no panic".
	})
}
