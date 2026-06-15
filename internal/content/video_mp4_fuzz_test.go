package content

import (
	"bytes"
	"context"
	"encoding/binary"
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
	// Regression seed: a fully-formed moov→trak→mdia→minf→stbl→stsd→avc1
	// tree whose VisualSampleEntry contains a child box declaring size 0.
	// Before the forward-progress guard in readVisualSampleEntryChildren
	// this pinned next==pos and looped until the -fuzztime deadline
	// ("context deadline exceeded"), never writing a crasher to testdata.
	f.Add(mp4WithZeroSizeSampleEntryChild())

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, _ = readMP4VideoInfo(context.Background(), r, int64(len(data)))
	})
}

// mp4Box wraps content in an ISO base media box: 4-byte big-endian size
// (header + content) followed by the 4-byte type.
func mp4Box(typ string, content []byte) []byte {
	size := uint32(8 + len(content))
	b := make([]byte, 4, 8+len(content))
	binary.BigEndian.PutUint32(b, size)
	b = append(b, typ...)
	return append(b, content...)
}

// mp4WithZeroSizeSampleEntryChild builds the smallest MP4 that reaches
// readVisualSampleEntryChildren with a size-0 child box — the input class
// that hung FuzzReadMP4VideoInfo.
func mp4WithZeroSizeSampleEntryChild() []byte {
	zeroChild := []byte{0x00, 0x00, 0x00, 0x00, 'x', 'x', 'x', 'x'} // size 0
	avc1 := mp4Box("avc1", append(make([]byte, 78), zeroChild...))  // 78-byte VisualSampleEntry body + child
	stsd := mp4Box("stsd", append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, avc1...))
	stbl := mp4Box("stbl", stsd)
	minf := mp4Box("minf", stbl)
	hdlr := mp4Box("hdlr", append(make([]byte, 8), 'v', 'i', 'd', 'e')) // handler_type at +8
	mdia := mp4Box("mdia", append(hdlr, minf...))
	trak := mp4Box("trak", mdia)
	return mp4Box("moov", trak)
}
