package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// buildFLAC builds a minimal FLAC file with a STREAMINFO block containing
// the given metadata. No audio frames; STREAMINFO alone is enough for our
// duration / sample_rate / channels parser.
func buildFLAC(sampleRate uint32, channels uint8, totalSamples uint64) []byte {
	var b bytes.Buffer
	b.WriteString("fLaC")

	// Block header: last-block flag (0x80) + type 0 (STREAMINFO) + 24-bit length.
	const streamInfoLen = 34
	b.WriteByte(0x80)                                     // last block, type STREAMINFO
	b.Write([]byte{0, 0, streamInfoLen})                  // length

	// STREAMINFO body (34 bytes total):
	//   16 min_block_size, 16 max_block_size,
	//   24 min_frame_size, 24 max_frame_size,
	//   20 sample_rate, 3 channels-1, 5 bits-per-sample-1, 36 total_samples,
	//   128 MD5
	b.Write([]byte{0x10, 0x00, 0x10, 0x00})               // min/max block (4096)
	b.Write([]byte{0, 0, 0, 0, 0, 0})                     // min/max frame (zero)

	// sample_rate (20 bits) + channels-1 (3 bits) + bps-1 (5 bits, set to 15 = 16-bit).
	// + top 4 bits of total_samples in the next byte.
	bpsMinus1 := uint8(15) // 16-bit
	chMinus1 := channels - 1
	// pack into 3 bytes: SR[19:12] | SR[11:4] | SR[3:0] CH[2:0] BPS[4:1]
	b.WriteByte(byte((sampleRate >> 12) & 0xFF))
	b.WriteByte(byte((sampleRate >> 4) & 0xFF))
	b.WriteByte(byte(((sampleRate & 0x0F) << 4) | (uint32(chMinus1) << 1) | (uint32(bpsMinus1)>>4)&0x01))

	// 4 bits = bps-1[3:0] | 36 bits total_samples — pack in 5 bytes.
	hi := byte(((bpsMinus1 & 0x0F) << 4) | byte((totalSamples>>32)&0x0F))
	b.WriteByte(hi)
	low4 := make([]byte, 4)
	binary.BigEndian.PutUint32(low4, uint32(totalSamples&0xFFFFFFFF))
	b.Write(low4)

	// 128-bit MD5 (zero).
	b.Write(make([]byte, 16))
	return b.Bytes()
}

func TestFLACInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	// 44100 Hz, 2 channels, 4_410_000 samples = exactly 100 seconds.
	if err := os.WriteFile(path, buildFLAC(44100, 2, 4_410_000), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "audio/flac" {
		t.Fatalf("Detect: got %v, want audio/flac", ct)
	}
	attrs, err := ct.Attributes(t.Context(), path)
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
	// bitrate is computed file_size*8/duration/1000; the synthesised fixture
	// has no audio frames so the calculation rounds to 0 and bitrate is
	// omitted. Real FLACs have non-zero bitrate; covered by smoke-testing.
}

func TestFLACBadMagic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.flac")
	if err := os.WriteFile(path, []byte("not a flac"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if _, ok := attrs["duration"]; ok {
		t.Errorf("duration present on bad FLAC: %v", attrs["duration"])
	}
}
