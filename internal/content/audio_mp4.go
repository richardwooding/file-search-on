package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// readMP4Info walks an MP4/M4A atom (box) tree extracting playback metadata.
//
//   - moov/mvhd          → timescale + duration (movie-level)
//   - moov/trak/mdia/minf/stbl/stsd/mp4a → channels + sample_rate
//
// Box-walking primitives live in mp4_box.go and are shared with video_mp4.go.
//
// References: ISO/IEC 14496-12 (Box structure), 14496-14 (MP4 file format).
func readMP4Info(r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return audioInfo{}, err
	}
	var info audioInfo
	if err := walkBoxes(r, 0, fileSize, []string{"moov"}, func(end int64) error {
		return audioScanMOOV(r, end, &info)
	}); err != nil {
		return info, err
	}
	return info, nil
}

// audioScanMOOV walks moov children looking for mvhd (duration) and trak
// (recurses to stsd for sample_rate + channels).
func audioScanMOOV(r io.ReadSeeker, end int64, info *audioInfo) error {
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, contentLen, err := readBoxHeader(r)
		if err != nil {
			return err
		}
		if size == 0 {
			contentLen = end - pos - 8
		}
		next := pos + size
		switch name {
		case "mvhd":
			if err := readMVHD(r, contentLen, info); err != nil {
				return err
			}
		case "trak":
			if err := descendBoxes(r, next, []string{"mdia", "minf", "stbl", "stsd"}, func(stsdEnd int64) error {
				return readAudioSTSD(r, stsdEnd, info)
			}); err != nil {
				return err
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

func readMVHD(r io.ReadSeeker, contentLen int64, info *audioInfo) error {
	if contentLen < 16 {
		return errors.New("mvhd too short")
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	version := buf[0]
	var timescale, duration uint64
	if version == 1 {
		if len(buf) < 4+8+8+4+8 {
			return errors.New("mvhd v1 too short")
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[20:24]))
		duration = binary.BigEndian.Uint64(buf[24:32])
	} else {
		if len(buf) < 4+4+4+4+4 {
			return errors.New("mvhd v0 too short")
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[12:16]))
		duration = uint64(binary.BigEndian.Uint32(buf[16:20]))
	}
	if timescale > 0 {
		info.Duration = float64(duration) / float64(timescale)
	}
	return nil
}

// readAudioSTSD parses stsd looking for the first audio sample entry. Layout:
// 1 byte version + 3 bytes flags + 4 bytes entry_count + first entry. Each
// entry starts with the standard 8-byte box header + a 28-byte
// AudioSampleEntry preamble; channels are at preamble offset +16 (uint16),
// sample_size (bits per sample) at +18 (uint16), and sample_rate at +24
// (uint16, fixed-point — only the integer part).
func readAudioSTSD(r io.ReadSeeker, end int64, info *audioInfo) error {
	var preamble [8]byte
	if _, err := io.ReadFull(r, preamble[:]); err != nil {
		return err
	}
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		size, name, _, err := readBoxHeader(r)
		if err != nil {
			return err
		}
		next := pos + size
		if isAudioSampleEntry(name) {
			var body [28]byte
			if _, err := io.ReadFull(r, body[:]); err != nil {
				return err
			}
			info.Channels = int64(binary.BigEndian.Uint16(body[16:18]))
			info.BitDepth = int64(binary.BigEndian.Uint16(body[18:20]))
			info.SampleRate = int64(binary.BigEndian.Uint16(body[24:26]))
			return nil
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// isAudioSampleEntry returns true for atom types that are audio sample
// entries (AAC, ALAC, plain PCM, AMR, etc.).
func isAudioSampleEntry(name string) bool {
	switch name {
	case "mp4a", "alac", "samr", "sawb", "sawp", "sevc", "sqcp", "ssmv", "twos", "sowt", "raw ":
		return true
	}
	return false
}
