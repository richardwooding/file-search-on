package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// MPEG bitrate tables (kbps) by [version_index][layer_index][bitrate_index].
// version_index: 0=MPEG1, 1=MPEG2/2.5
// layer_index:   0=Layer III, 1=Layer II, 2=Layer I
// bitrate_index: 0-15 (0 = free, 15 = bad)
var mpegBitrate = [2][3][16]int{
	{ // MPEG 1
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},  // L3
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0}, // L2
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0}, // L1
	},
	{ // MPEG 2 / 2.5
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},     // L3
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},     // L2
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0}, // L1
	},
}

// MPEG sample-rate tables (Hz) by [version][srate_index].
// version: 0=MPEG2.5, 1=reserved, 2=MPEG2, 3=MPEG1.
var mpegSampleRate = [4][3]int{
	{11025, 12000, 8000},  // MPEG 2.5
	{0, 0, 0},             // reserved
	{22050, 24000, 16000}, // MPEG 2
	{44100, 48000, 32000}, // MPEG 1
}

// readMP3Info skips any ID3v2 prefix, decodes the first MPEG audio frame
// header, and looks for a Xing/Info VBR header to compute duration. Falls
// back to (file_size - id3v2_size) * 8 / nominal_bitrate when no Xing tag is
// present (typical for CBR or pre-2001 VBR encoders).
func readMP3Info(r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	id3Size, err := skipID3v2(r)
	if err != nil {
		return audioInfo{}, err
	}

	// Read the next 4096 bytes — enough to cover an MP3 frame + Xing tag.
	buf := make([]byte, 4096)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	if n < 32 {
		return audioInfo{}, errors.New("not enough data after ID3v2 for MP3 frame")
	}

	// Find the first valid frame sync (FF Ex with Ex >= E0).
	off := -1
	for i := 0; i+3 < len(buf); i++ {
		if buf[i] == 0xFF && (buf[i+1]&0xE0) == 0xE0 {
			off = i
			break
		}
	}
	if off < 0 {
		return audioInfo{}, errors.New("no MP3 frame sync found")
	}

	versionIdx := (buf[off+1] >> 3) & 0x03   // 0=MPEG2.5, 2=MPEG2, 3=MPEG1
	layerIdx := (buf[off+1] >> 1) & 0x03     // 1=L3, 2=L2, 3=L1
	bitrateIdx := (buf[off+2] >> 4) & 0x0F
	sampleRateIdx := (buf[off+2] >> 2) & 0x03
	channelMode := (buf[off+3] >> 6) & 0x03 // 3 = mono

	if versionIdx == 1 || layerIdx == 0 || sampleRateIdx == 3 {
		return audioInfo{}, errors.New("invalid MP3 frame header")
	}
	// Convert layerIdx (1=L3, 2=L2, 3=L1) to table layer index (0=L3, 1=L2, 2=L1).
	layer := int(layerIdx) - 1
	mpegFamily := 0
	if versionIdx != 3 {
		mpegFamily = 1 // MPEG 2 or 2.5
	}
	bitrate := mpegBitrate[mpegFamily][layer][bitrateIdx]
	sampleRate := mpegSampleRate[versionIdx][sampleRateIdx]
	channels := int64(2)
	if channelMode == 3 {
		channels = 1
	}

	info := audioInfo{
		SampleRate: int64(sampleRate),
		Channels:   channels,
	}

	// Locate Xing/Info offset within the first frame.
	var xingOffset int
	switch {
	case versionIdx == 3 && channelMode == 3: // MPEG1 mono
		xingOffset = off + 4 + 17
	case versionIdx == 3: // MPEG1 stereo / JS / dual
		xingOffset = off + 4 + 32
	case channelMode == 3: // MPEG2/2.5 mono
		xingOffset = off + 4 + 9
	default: // MPEG2/2.5 stereo / JS / dual
		xingOffset = off + 4 + 17
	}

	if xingOffset+12 < len(buf) {
		tag := string(buf[xingOffset : xingOffset+4])
		if tag == "Xing" || tag == "Info" {
			flags := binary.BigEndian.Uint32(buf[xingOffset+4 : xingOffset+8])
			if flags&0x01 != 0 { // FRAMES present
				frames := binary.BigEndian.Uint32(buf[xingOffset+8 : xingOffset+12])
				samplesPerFrame := 1152
				if versionIdx != 3 { // MPEG2 / 2.5 Layer III
					samplesPerFrame = 576
				}
				if sampleRate > 0 {
					info.Duration = float64(uint64(frames)*uint64(samplesPerFrame)) / float64(sampleRate)
				}
			}
		}
	}

	// Fallback: estimate from CBR bitrate (skewed on VBR but better than zero).
	if info.Duration == 0 && bitrate > 0 {
		audioBytes := fileSize - id3Size
		if audioBytes > 0 {
			info.Duration = float64(audioBytes*8) / float64(bitrate*1000)
		}
	}
	return info, nil
}

// skipID3v2 advances r past any ID3v2 tag at the start of the file. Returns
// the absolute byte offset of the first non-tag byte (= ID3v2 size or 0).
func skipID3v2(r io.ReadSeeker) (int64, error) {
	var hdr [10]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, err
	}
	if string(hdr[:3]) != "ID3" {
		// Not ID3v2 — rewind and report 0.
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		return 0, nil
	}
	// Synchsafe-encoded size: 4 bytes, 7 bits each.
	size := uint32(hdr[6]&0x7F)<<21 | uint32(hdr[7]&0x7F)<<14 | uint32(hdr[8]&0x7F)<<7 | uint32(hdr[9]&0x7F)
	if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
		return 0, err
	}
	return int64(size) + 10, nil
}
