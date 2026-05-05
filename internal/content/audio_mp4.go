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
// (uint16, fixed-point — only the integer part). Children (esds for AAC)
// follow the preamble and end at the sample-entry box's `next` offset.
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
			// Walk children of the audio sample entry for esds (AAC
			// Elementary Stream Descriptor) carrying maxBitrate / avgBitrate.
			readAudioSampleEntryChildren(r, next, info)
			return nil
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// readAudioSampleEntryChildren walks children of an mp4a / alac / etc.
// AudioSampleEntry, looking for the esds box. Reader is positioned just
// after the 28-byte preamble; `end` is the end of the parent sample-entry
// box. esds carries codec-stored avg / max bitrate inside the MPEG-4
// Elementary Stream Descriptor (ES_DescrTag = 0x03) →
// DecoderConfigDescriptor (tag 0x04) chain.
func readAudioSampleEntryChildren(r io.ReadSeeker, end int64, info *audioInfo) {
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return
		}
		size, name, contentLen, err := readBoxHeader(r)
		if err != nil {
			return
		}
		next := pos + size
		if name == "esds" {
			body := make([]byte, contentLen)
			if _, err := io.ReadFull(r, body); err == nil {
				if avg, maxBR, ok := readESDSBitrates(body); ok {
					switch {
					case avg > 0:
						info.NominalBitrate = int64(avg) / 1000
					case maxBR > 0:
						info.NominalBitrate = int64(maxBR) / 1000
					}
				}
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return
		}
	}
}

// readESDSBitrates parses an esds box body and extracts the
// avgBitrate / maxBitrate from the DecoderConfigDescriptor. esds layout:
//
//	4 bytes  version + flags
//	1 byte   tag (0x03 = ES_DescrTag)
//	N bytes  variable-length size  (readVarLenSize: 7 bits/byte with
//	                                 high-bit continuation flag)
//	2 bytes  ES_ID
//	1 byte   flags (top 3 bits gate optional fields below)
//	[2 bytes dependsOn_ES_ID]      if streamDependenceFlag (0x80)
//	[1+L bytes URL]                if URL_Flag (0x40)
//	[2 bytes OCR_ES_ID]            if OCRstreamFlag (0x20)
//	1 byte   tag (0x04 = DecoderConfigDescrTag)
//	N bytes  variable-length size
//	1 byte   objectTypeIndication
//	1 byte   streamType + upStream + reserved
//	3 bytes  bufferSizeDB
//	4 bytes  maxBitrate    ← we want this
//	4 bytes  avgBitrate    ← and this
//	... DecoderSpecificInfo (tag 0x05) follows but we stop here ...
//
// References: ISO/IEC 14496-1 §7.2 (ES_Descriptor),
// ISO/IEC 14496-14 (MP4 file format).
func readESDSBitrates(buf []byte) (avg, maxBR uint32, ok bool) {
	if len(buf) < 4 {
		return 0, 0, false
	}
	p := 4 // skip version + flags
	if p >= len(buf) || buf[p] != 0x03 {
		return 0, 0, false
	}
	p++
	_, n := readDescriptorSize(buf[p:])
	if n == 0 {
		return 0, 0, false
	}
	p += n
	if p+3 > len(buf) {
		return 0, 0, false
	}
	flags := buf[p+2]
	p += 3
	if flags&0x80 != 0 { // streamDependenceFlag
		p += 2
	}
	if flags&0x40 != 0 { // URL_Flag — 1 byte length, then URL bytes
		if p >= len(buf) {
			return 0, 0, false
		}
		p += 1 + int(buf[p])
	}
	if flags&0x20 != 0 { // OCRstreamFlag
		p += 2
	}
	if p >= len(buf) || buf[p] != 0x04 {
		return 0, 0, false
	}
	p++
	_, n = readDescriptorSize(buf[p:])
	if n == 0 {
		return 0, 0, false
	}
	p += n
	// DecoderConfigDescriptor body: 1 + 1 + 3 + 4 + 4 = 13 bytes minimum.
	if p+13 > len(buf) {
		return 0, 0, false
	}
	maxBR = binary.BigEndian.Uint32(buf[p+5 : p+9])
	avg = binary.BigEndian.Uint32(buf[p+9 : p+13])
	return avg, maxBR, true
}

// readDescriptorSize decodes the MPEG-4 expandable-class size field:
// 7 bits per byte with a continuation bit at the top of each. Spec caps
// it at 4 bytes; anything longer is treated as malformed (returns 0, 0).
func readDescriptorSize(buf []byte) (size uint32, consumed int) {
	var v uint32
	for i := 0; i < len(buf) && i < 4; i++ {
		b := buf[i]
		v = (v << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
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
