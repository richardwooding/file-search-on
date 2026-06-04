package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV constructs a minimal valid PCM WAV (header + fmt + data) for
// the given parameters, with dataBytes of (zeroed) sample data.
func buildWAV(channels, bitsPerSample int, sampleRate, dataBytes uint32) []byte {
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	blockAlign := uint16(channels * bitsPerSample / 8)
	var b bytes.Buffer
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) } // never errors on a bytes.Buffer
	b.WriteString("RIFF")
	put(uint32(36 + dataBytes))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	put(uint32(16))
	put(uint16(1)) // PCM
	put(uint16(channels))
	put(sampleRate)
	put(byteRate)
	put(blockAlign)
	put(uint16(bitsPerSample))
	b.WriteString("data")
	put(dataBytes)
	b.Write(make([]byte, dataBytes))
	return b.Bytes()
}

func TestReadWAVInfo(t *testing.T) {
	// 2ch, 16-bit, 44100Hz, 1 second of data (byteRate bytes).
	sr := uint32(44100)
	byteRate := sr * 2 * 16 / 8
	wav := buildWAV(2, 16, sr, byteRate) // 1.0s
	info, err := readWAVInfo(bytes.NewReader(wav))
	if err != nil {
		t.Fatalf("readWAVInfo: %v", err)
	}
	if info.SampleRate != 44100 {
		t.Errorf("SampleRate=%d want 44100", info.SampleRate)
	}
	if info.Channels != 2 {
		t.Errorf("Channels=%d want 2", info.Channels)
	}
	if info.BitDepth != 16 {
		t.Errorf("BitDepth=%d want 16", info.BitDepth)
	}
	if info.Duration < 0.99 || info.Duration > 1.01 {
		t.Errorf("Duration=%v want ~1.0", info.Duration)
	}
	if info.NominalBitrate <= 0 {
		t.Errorf("NominalBitrate=%d want > 0", info.NominalBitrate)
	}
}

func TestReadWAVInfo_NotWAV(t *testing.T) {
	if _, err := readWAVInfo(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00WEBPxxxx"))); err == nil {
		t.Error("expected error for non-WAVE RIFF input")
	}
}
