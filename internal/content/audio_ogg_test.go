package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

)

// buildOGG builds a minimal OGG file containing:
//   - one page wrapping a Vorbis identification header (gives sample_rate +
//     channels)
//   - one final page whose granule_position encodes the total sample count
//
// The pages aren't checksummed correctly — the parser doesn't verify CRCs.
func buildOGG(sampleRate uint32, channels uint8, totalSamples int64) []byte {
	// Vorbis identification packet (30 bytes).
	var packet bytes.Buffer
	packet.WriteString("\x01vorbis")
	_ = binary.Write(&packet, binary.LittleEndian, uint32(0)) // vorbis version
	packet.WriteByte(channels)
	_ = binary.Write(&packet, binary.LittleEndian, sampleRate)
	_ = binary.Write(&packet, binary.LittleEndian, uint32(0))   // bitrate_max
	_ = binary.Write(&packet, binary.LittleEndian, uint32(0))   // bitrate_nominal
	_ = binary.Write(&packet, binary.LittleEndian, uint32(0))   // bitrate_min
	packet.WriteByte(0xB8)                                  // blocksizes 8/8
	packet.WriteByte(0x01)                                  // framing flag

	page := func(granule int64, payload []byte) []byte {
		var p bytes.Buffer
		p.WriteString("OggS")
		p.WriteByte(0)                                       // version
		p.WriteByte(0)                                       // header type
		_ = binary.Write(&p, binary.LittleEndian, granule)        // granule_position (int64)
		_ = binary.Write(&p, binary.LittleEndian, uint32(0))     // serial number
		_ = binary.Write(&p, binary.LittleEndian, uint32(0))     // page sequence
		_ = binary.Write(&p, binary.LittleEndian, uint32(0))     // checksum (we cheat)
		p.WriteByte(1)                                       // page segments
		p.WriteByte(byte(len(payload)))                      // segment table
		p.Write(payload)
		return p.Bytes()
	}

	var out bytes.Buffer
	out.Write(page(0, packet.Bytes()))            // first audio page
	out.Write(make([]byte, 1024))                  // some filler so the file isn't tiny
	out.Write(page(totalSamples, []byte("end"))) // last page with the granule
	return out.Bytes()
}

func TestOGGInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.ogg")
	// 48000 Hz, 2 channels, 4_800_000 samples = 100 seconds.
	if err := os.WriteFile(path, buildOGG(48000, 2, 4_800_000), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "audio/ogg" {
		t.Fatalf("Detect: got %v, want audio/ogg", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["sample_rate"]; got != int64(48000) {
		t.Errorf("sample_rate = %v, want 48000", got)
	}
	if got := attrs["channels"]; got != int64(2) {
		t.Errorf("channels = %v, want 2", got)
	}
	if got := attrs["duration"]; got != float64(100) {
		t.Errorf("duration = %v, want 100", got)
	}
}
