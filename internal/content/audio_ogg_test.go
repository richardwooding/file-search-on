package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// oggPage wraps payload in a single-page OggS header carrying granule at
// offset 6, enough for readOGGInfo's codec sniff + last-granule scan.
func oggPage(granule uint64, payload []byte) []byte {
	b := make([]byte, 28)
	copy(b, "OggS")
	binary.LittleEndian.PutUint64(b[6:14], granule)
	b[26] = 1
	b[27] = byte(len(payload))
	return append(b, payload...)
}

// TestReadOGGInfo_Codecs is the regression for issue #325: readOGGInfo
// handled only Vorbis, so Opus / FLAC-in-OGG surfaced no metadata. It
// now dispatches on the codec identification header.
func TestReadOGGInfo_Codecs(t *testing.T) {
	t.Run("vorbis", func(t *testing.T) {
		p := []byte("\x01vorbis")
		p = binary.LittleEndian.AppendUint32(p, 0)      // version
		p = append(p, 2)                                // channels (+11)
		p = binary.LittleEndian.AppendUint32(p, 44100)  // sample_rate (+12)
		p = binary.LittleEndian.AppendUint32(p, 0)      // bitrate_max
		p = binary.LittleEndian.AppendUint32(p, 128000) // bitrate_nominal (+20)
		p = binary.LittleEndian.AppendUint32(p, 0)      // bitrate_min
		p = append(p, 0, 0)                             // blocksizes, framing
		buf := oggPage(88200, p)                        // 2s @ 44100
		info, err := readOGGInfo(bytes.NewReader(buf), int64(len(buf)))
		if err != nil {
			t.Fatal(err)
		}
		if info.SampleRate != 44100 || info.Channels != 2 || info.NominalBitrate != 128 {
			t.Errorf("vorbis: %+v", info)
		}
		if info.Duration < 1.99 || info.Duration > 2.01 {
			t.Errorf("vorbis Duration=%v want ~2", info.Duration)
		}
	})

	t.Run("opus", func(t *testing.T) {
		p := []byte("OpusHead")
		p = append(p, 1, 2)                            // version, channels (+9)
		p = binary.LittleEndian.AppendUint16(p, 312)   // pre_skip (+10)
		p = binary.LittleEndian.AppendUint32(p, 48000) // input rate
		p = append(p, 0, 0, 0)
		buf := oggPage(336312, p) // (336312-312)/48000 = 7s
		info, err := readOGGInfo(bytes.NewReader(buf), int64(len(buf)))
		if err != nil {
			t.Fatal(err)
		}
		if info.SampleRate != 48000 || info.Channels != 2 {
			t.Errorf("opus: %+v", info)
		}
		if info.Duration < 6.99 || info.Duration > 7.01 {
			t.Errorf("opus Duration=%v want ~7", info.Duration)
		}
	})

	t.Run("unrecognised", func(t *testing.T) {
		if _, err := readOGGInfo(bytes.NewReader(oggPage(0, []byte("SpeexNotSupported"))), 100); err == nil {
			t.Error("expected error for unrecognised OGG codec")
		}
	})
}
