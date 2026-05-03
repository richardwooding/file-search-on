package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// buildMP3WithXing builds a minimal MP3 file: a tiny ID3v2 header (just an
// empty TIT2 frame for parser realism) + a single MPEG1 Layer III frame
// header + Xing tag with a known frame count.
//
// The frame parameters are fixed: MPEG1, Layer III, 128 kbps, 44.1 kHz,
// stereo. samples_per_frame = 1152, so duration = frames * 1152 / 44100.
func buildMP3WithXing(frameCount uint32) []byte {
	// MPEG1 L3 frame header: FF FB 90 00
	//   FF FB    sync (11 bits) + version 11 (MPEG1) + layer 01 (L3) + protection 1
	//   90       bitrate index 9 (128k) + sample rate index 0 (44.1) + padding 0 + private 0
	//   00       channel mode 00 (stereo) + mode ext 00 + cr 0 + orig 0 + emph 00
	frameHeader := []byte{0xFF, 0xFB, 0x90, 0x00}

	// Pad to Xing offset (32 bytes after frame header for MPEG1 stereo).
	padding := make([]byte, 32)

	xing := bytes.Buffer{}
	xing.WriteString("Xing")
	// Flags: 0x00000001 (FRAMES present).
	_ = binary.Write(&xing, binary.BigEndian, uint32(0x01))
	_ = binary.Write(&xing, binary.BigEndian, frameCount)

	// Final body — frame doesn't need to be complete or audio-correct;
	// our parser stops at the Xing tag.
	var b bytes.Buffer
	b.Write(frameHeader)
	b.Write(padding)
	b.Write(xing.Bytes())
	// Pad out to several KB so the MP3 file looks plausible (and the bitrate
	// computation lands on a real number).
	b.Write(make([]byte, 8192))
	return b.Bytes()
}

func TestMP3WithXing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	// 3825 frames × 1152 samples / 44100 Hz ≈ 99.918 seconds (~100 s).
	const frames = 3825
	if err := os.WriteFile(path, buildMP3WithXing(frames), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "audio/mpeg" {
		t.Fatalf("Detect: got %v, want audio/mpeg", ct)
	}
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	dur, _ := attrs["duration"].(float64)
	if dur < 99 || dur > 101 {
		t.Errorf("duration = %v, want ≈100", dur)
	}
	if got := attrs["sample_rate"]; got != int64(44100) {
		t.Errorf("sample_rate = %v, want 44100", got)
	}
	if got := attrs["channels"]; got != int64(2) {
		t.Errorf("channels = %v, want 2", got)
	}
}

func TestMP3CBRFallback(t *testing.T) {
	// MP3 with frame header but no Xing tag — duration must come from the
	// (file_size * 8) / nominal_bitrate fallback path.
	dir := t.TempDir()
	path := filepath.Join(dir, "cbr.mp3")

	// MPEG1 L3 128 kbps stereo, no Xing payload after.
	frameHeader := []byte{0xFF, 0xFB, 0x90, 0x00}
	body := bytes.NewBuffer(frameHeader)
	// 16000 bytes of "audio" → expected duration = 16000*8 / 128000 = 1 sec.
	body.Write(make([]byte, 16000-len(frameHeader)))
	if err := os.WriteFile(path, body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	ct := content.DefaultRegistry().Detect(path)
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	dur, _ := attrs["duration"].(float64)
	// CBR fallback: 16000 bytes * 8 / 128000 = 1 second exactly.
	if dur < 0.95 || dur > 1.05 {
		t.Errorf("duration (CBR fallback) = %v, want ≈1", dur)
	}
}
