package content_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

)

// vintEncode encodes a uint64 as an EBML variable-length integer using the
// minimum number of bytes necessary.
func vintEncode(v uint64) []byte {
	for w := 1; w <= 8; w++ {
		max := (uint64(1) << uint(7*w)) - 1
		if v <= max {
			out := make([]byte, w)
			marker := byte(0x80) >> uint(w-1)
			for i := w - 1; i > 0; i-- {
				out[i] = byte(v & 0xFF)
				v >>= 8
			}
			out[0] = byte(v) | marker
			return out
		}
	}
	panic("vint too large")
}

// ebmlElem builds an EBML element: raw ID bytes + VINT-encoded size + data.
func ebmlElem(id []byte, data []byte) []byte {
	var out bytes.Buffer
	out.Write(id)
	out.Write(vintEncode(uint64(len(data))))
	out.Write(data)
	return out.Bytes()
}

func ebmlUint(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	for i := range b {
		if b[i] != 0 {
			return b[i:]
		}
	}
	return b[:]
}

func ebmlFloat(v float64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	return b[:]
}

func buildMKV(timecodeScaleNs uint64, durationTU float64, width, height uint64, defaultDurationNs uint64) []byte {
	// Tracks → TrackEntry → TrackType=1 (video), CodecID, DefaultDuration,
	// Video → PixelWidth, PixelHeight.
	video := bytes.Buffer{}
	video.Write(ebmlElem([]byte{0xB0}, ebmlUint(width)))         // PixelWidth
	video.Write(ebmlElem([]byte{0xBA}, ebmlUint(height)))        // PixelHeight

	track := bytes.Buffer{}
	track.Write(ebmlElem([]byte{0x83}, []byte{1}))               // TrackType=1 video
	track.Write(ebmlElem([]byte{0x86}, []byte("V_AV1")))         // CodecID=AV1
	track.Write(ebmlElem([]byte{0x23, 0xE3, 0x83}, ebmlUint(defaultDurationNs))) // DefaultDuration
	track.Write(ebmlElem([]byte{0xE0}, video.Bytes()))           // Video

	tracks := ebmlElem([]byte{0x16, 0x54, 0xAE, 0x6B}, ebmlElem([]byte{0xAE}, track.Bytes()))

	info := bytes.Buffer{}
	info.Write(ebmlElem([]byte{0x2A, 0xD7, 0xB1}, ebmlUint(timecodeScaleNs)))
	info.Write(ebmlElem([]byte{0x44, 0x89}, ebmlFloat(durationTU)))
	infoElem := ebmlElem([]byte{0x15, 0x49, 0xA9, 0x66}, info.Bytes())

	segmentBody := append(infoElem, tracks...)
	segment := ebmlElem([]byte{0x18, 0x53, 0x80, 0x67}, segmentBody)

	// EBML header — minimal, just an empty container so readMKVInfo skips it.
	header := ebmlElem([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte{})

	return append(header, segment...)
}

func TestMKVInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mkv")
	// timecodeScale 1ms (1_000_000 ns), duration 100_000 timecode units → 100s.
	// 1920×1080, AV1, defaultDuration 33_333_333 ns ≈ 30 fps.
	if err := os.WriteFile(path, buildMKV(1_000_000, 100_000, 1920, 1080, 33_333_333), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "video/x-matroska" {
		t.Fatalf("Detect: got %v, want video/x-matroska", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["duration"]; got != float64(100) {
		t.Errorf("duration = %v, want 100", got)
	}
	if got := attrs["video_width"]; got != int64(1920) {
		t.Errorf("video_width = %v, want 1920", got)
	}
	if got := attrs["video_height"]; got != int64(1080) {
		t.Errorf("video_height = %v, want 1080", got)
	}
	if got := attrs["video_codec"]; got != "av1" {
		t.Errorf("video_codec = %v, want av1", got)
	}
	if got, _ := attrs["frame_rate"].(float64); got < 29.9 || got > 30.1 {
		t.Errorf("frame_rate = %v, want ≈30", got)
	}
}
