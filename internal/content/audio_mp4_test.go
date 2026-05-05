package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

)

// atomBox builds an MP4 atom (size + 4-byte type + content). Returns the
// full bytes including the 8-byte header.
func atomBox(name string, content []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(8+len(content)))
	b.WriteString(name)
	b.Write(content)
	return b.Bytes()
}

// buildMVHD constructs a version-0 movie header atom body (without the
// outer atom header).
func buildMVHD(timescale, duration uint32) []byte {
	var b bytes.Buffer
	b.WriteByte(0)                                       // version 0
	b.Write([]byte{0, 0, 0})                              // flags
	_ = binary.Write(&b, binary.BigEndian, uint32(0))         // creation_time
	_ = binary.Write(&b, binary.BigEndian, uint32(0))         // modification_time
	_ = binary.Write(&b, binary.BigEndian, timescale)         // timescale
	_ = binary.Write(&b, binary.BigEndian, duration)          // duration
	_ = binary.Write(&b, binary.BigEndian, uint32(0x00010000)) // rate (1.0)
	_ = binary.Write(&b, binary.BigEndian, uint16(0x0100))    // volume
	b.Write(make([]byte, 10))                              // reserved
	// 9-element unity matrix (36 bytes), pre-defined (24 bytes), next_track_ID (4 bytes)
	b.Write(make([]byte, 36+24+4))
	return b.Bytes()
}

// buildMP4A constructs a minimal mp4a sample entry.
func buildMP4A(channels, sampleRate uint16) []byte {
	var b bytes.Buffer
	b.Write(make([]byte, 6))                                // reserved
	_ = binary.Write(&b, binary.BigEndian, uint16(1))           // data reference index
	b.Write(make([]byte, 8))                                // reserved
	_ = binary.Write(&b, binary.BigEndian, channels)            // channel count
	_ = binary.Write(&b, binary.BigEndian, uint16(16))          // sample size
	_ = binary.Write(&b, binary.BigEndian, uint16(0))           // pre-defined
	_ = binary.Write(&b, binary.BigEndian, uint16(0))           // reserved
	_ = binary.Write(&b, binary.BigEndian, sampleRate)          // sample_rate (high 16 bits)
	_ = binary.Write(&b, binary.BigEndian, uint16(0))           // sample_rate (low 16 bits)
	return b.Bytes()
}

// buildSTSD constructs a stsd box body containing one mp4a entry.
func buildSTSD(mp4a []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(0)                              // version
	b.Write([]byte{0, 0, 0})                     // flags
	_ = binary.Write(&b, binary.BigEndian, uint32(1)) // entry count
	b.Write(atomBox("mp4a", mp4a))
	return b.Bytes()
}

// buildMP4 builds a minimal MP4 file with mvhd + a sparse trak/mdia/minf/
// stbl/stsd/mp4a chain.
func buildMP4(timescale, duration uint32, sampleRate uint16, channels uint16) []byte {
	mp4a := buildMP4A(channels, sampleRate)
	stsd := buildSTSD(mp4a)
	stbl := atomBox("stsd", stsd)
	minf := atomBox("stbl", stbl)
	mdia := atomBox("minf", minf)
	trak := atomBox("mdia", mdia)
	moovBody := bytes.Buffer{}
	moovBody.Write(atomBox("mvhd", buildMVHD(timescale, duration)))
	moovBody.Write(atomBox("trak", trak))

	var out bytes.Buffer
	// Minimal ftyp.
	out.Write(atomBox("ftyp", []byte("M4A isomiso2mp41")))
	out.Write(atomBox("moov", moovBody.Bytes()))
	return out.Bytes()
}

func TestMP4Info(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.m4a")
	// timescale=1000 (ms), duration=100_000 → 100 seconds.
	// sample_rate=44100, channels=2.
	if err := os.WriteFile(path, buildMP4(1000, 100_000, 44100, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "audio/mp4" {
		t.Fatalf("Detect: got %v, want audio/mp4", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["duration"]; got != float64(100) {
		t.Errorf("duration = %v, want 100", got)
	}
	if got := attrs["sample_rate"]; got != int64(44100) {
		t.Errorf("sample_rate = %v, want 44100", got)
	}
	if got := attrs["channels"]; got != int64(2) {
		t.Errorf("channels = %v, want 2", got)
	}
}
