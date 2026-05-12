package content

import (
	"bytes"
	"testing"
)

// FuzzReadMP4VideoInfo targets the MP4 box walker (mp4_box.go +
// video_mp4.go: readBoxHeader, walkBoxes, descendBoxes,
// readMP4VideoInfo, plus the various per-box readers — tkhd, mvhd,
// stsd, mdhd, colour-info). Boxes are length-prefixed and nest
// recursively, classic territory for integer overflow on the size
// field and stack growth on nested adversarial input.
//
// Contract: never panic, never loop forever. We don't assert on
// outputs — corrupt input legitimately produces zero-valued attrs.
func FuzzReadMP4VideoInfo(f *testing.F) {
	f.Add([]byte{})
	// "ftyp" box header of size 8 (empty contents).
	f.Add([]byte{0x00, 0x00, 0x00, 0x08, 'f', 't', 'y', 'p'})
	// "moov" box of size 8 (empty contents) — exercises the walker
	// descending into a recognised container.
	f.Add([]byte{0x00, 0x00, 0x00, 0x08, 'm', 'o', 'o', 'v'})
	// 64-bit large-size marker: size=1 means "look at the next 8
	// bytes for the actual size". Adversarial inputs often abuse
	// this to claim a huge size, then truncate.
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 'm', 'd', 'a', 't',
		0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = readMP4VideoInfo(r, int64(len(data)))
	})
}
