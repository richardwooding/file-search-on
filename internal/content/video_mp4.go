package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// videoInfo is the shape returned by every video-format parser. Zero values
// mean "unknown".
type videoInfo struct {
	Duration   float64 // seconds
	Width      int64
	Height     int64
	VideoCodec string // "h264", "h265", ...
	AudioCodec string // "aac", "mp3", ...
	FrameRate  float64

	// First-audio-track playback metadata. Files almost always have a
	// single audio track; multi-track files (multi-language films) use
	// the first one — same convention as the codec-name fields above.
	AudioSampleRate int64 // Hz
	AudioChannels   int64

	// Rotation in degrees (0 / 90 / 180 / 270) — derived from the MP4
	// tkhd display matrix. Matters for portrait-recorded phone clips
	// stored as e.g. 1920×1080 with a 90° rotation matrix. Other
	// container formats (MKV / AVI) leave this 0; MKV's rotation lives
	// in Video/Projection (newer spec, less common; out of scope for
	// now). For non-pure-rotation matrices (skew, mirror, projective)
	// stays 0.
	Rotation int64

	// NominalBitrate is the codec/container-stored video bitrate in
	// kbps, distinct from videotype.go's computed `bitrate` (file
	// size × 8 / duration). Sources:
	//   - MP4 / MOV: btrt box's avgBitrate inside the visual sample
	//     entry (avc1 / hvc1 / av01 / vpcC / etc.).
	//   - MKV / WebM: TrackEntry's Bitrate element (0x4FB1).
	//   - AVI:        avih's maxBytesPerSec × 8 / 1000.
	NominalBitrate int64

	// Colour-space metadata, decoded from MP4 colr (nclx form) or
	// MKV Colour (0x55B0). Empty / false when not present.
	//   - ColourPrimaries: friendly name — "bt709", "bt2020", "p3",
	//     "" (unknown / not present).
	//   - ColourTransfer:  friendly name — "bt709", "pq", "hlg", "".
	//   - IsHDR:           true when transfer is PQ (SMPTE ST 2084)
	//     or HLG; the canonical "this is HDR content" signal.
	ColourPrimaries string
	ColourTransfer  string
	IsHDR           bool
}

// readMP4VideoInfo walks an MP4/MOV/M4V atom tree extracting playback +
// video-track metadata. Reuses the box walkers in mp4_box.go.
func readMP4VideoInfo(r io.ReadSeeker, fileSize int64) (videoInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return videoInfo{}, err
	}
	var info videoInfo
	if err := walkBoxes(r, 0, fileSize, []string{"moov"}, func(end int64) error {
		return videoScanMOOV(r, end, &info)
	}); err != nil {
		return info, err
	}
	return info, nil
}

func videoScanMOOV(r io.ReadSeeker, end int64, info *videoInfo) error {
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
			if err := readVideoMVHD(r, contentLen, info); err != nil {
				return err
			}
		case "trak":
			trakContentStart, _ := r.Seek(0, io.SeekCurrent)
			if err := videoScanTRAK(r, trakContentStart, next, info); err != nil {
				return err
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// videoScanTRAK does two passes through the trak's mdia box:
//  1. Collect track type (hdlr) + track timescale (mdhd) + minf bounds.
//  2. Walk minf/stbl for stsd (codec + width/height) and stts (frame rate).
//
// Two passes because hdlr and minf can appear in either order in the file,
// and we need both pieces of info before we can correctly interpret stsd.
func videoScanTRAK(r io.ReadSeeker, trakStart, trakEnd int64, info *videoInfo) error {
	// Find mdia bounds + tkhd contents inside trak.
	var mdiaStart, mdiaEnd int64 = -1, -1
	if err := walkBoxes(r, trakStart, trakEnd, []string{"mdia"}, func(end int64) error {
		cur, _ := r.Seek(0, io.SeekCurrent)
		mdiaStart = cur
		mdiaEnd = end
		return nil
	}); err != nil {
		return err
	}
	if mdiaStart < 0 {
		return nil
	}
	// Tkhd is a sibling of mdia inside trak; only read it for video
	// tracks (we don't know trackType yet here, but the matrix lives in
	// every tkhd — so scan unconditionally and only assign Rotation
	// later when the track is identified as video).
	if rotation := readMP4Tkhd(r, trakStart, trakEnd); rotation != 0 && info.Rotation == 0 {
		info.Rotation = rotation
	}

	// Pass 1: collect track type, timescale, minf bounds.
	var trackType string
	var timescale uint32
	var minfStart, minfEnd int64 = -1, -1
	if _, err := r.Seek(mdiaStart, io.SeekStart); err != nil {
		return err
	}
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= mdiaEnd {
			break
		}
		size, name, contentLen, err := readBoxHeader(r)
		if err != nil {
			return err
		}
		next := pos + size
		switch name {
		case "hdlr":
			// 1 version + 3 flags + 4 pre_defined + 4 handler_type + ...
			var head [12]byte
			if _, err := io.ReadFull(r, head[:]); err != nil {
				return err
			}
			trackType = string(head[8:12])
		case "mdhd":
			timescale = readMDHDTimescale(r, contentLen)
		case "minf":
			cur, _ := r.Seek(0, io.SeekCurrent)
			minfStart = cur
			minfEnd = next
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}

	if minfStart < 0 || trackType == "" {
		return nil
	}

	// Pass 2: walk minf for stbl, then stbl children for stsd + stts.
	return walkBoxes(r, minfStart, minfEnd, []string{"stbl"}, func(stblEnd int64) error {
		stblStart, _ := r.Seek(0, io.SeekCurrent)
		if _, err := r.Seek(stblStart, io.SeekStart); err != nil {
			return err
		}
		for {
			pos, _ := r.Seek(0, io.SeekCurrent)
			if pos >= stblEnd {
				return nil
			}
			size, name, _, err := readBoxHeader(r)
			if err != nil {
				return err
			}
			next := pos + size
			switch name {
			case "stsd":
				if err := readVideoSTSD(r, next, trackType, info); err != nil {
					return err
				}
			case "stts":
				if trackType == "vide" && timescale > 0 {
					readSTTS(r, next-pos-8, timescale, info)
				}
			}
			if _, err := r.Seek(next, io.SeekStart); err != nil {
				return err
			}
		}
	})
}

// readMP4Tkhd looks inside trak for a tkhd box and decodes its 3×3 display
// matrix into a rotation angle (0 / 90 / 180 / 270). Returns 0 for the
// identity matrix or any matrix that isn't a pure axis-aligned rotation
// (skew, mirror, projective transforms).
//
// The tkhd matrix is at offset 40 (v0) or 52 (v1) from the start of the
// box content (after the box header consumed by readBoxHeader). It is 9
// int32 values in 16.16 signed fixed-point: a, b, u, c, d, v, x, y, w.
// For pure rotations (a, b, c, d) is one of:
//
//	(1, 0, 0, 1)   →   0°
//	(0, 1, -1, 0)  →  90° clockwise
//	(-1, 0, 0, -1) → 180°
//	(0, -1, 1, 0)  → 270°
//
// Stored as 32-bit fixed-point so 1.0 = 0x00010000, -1.0 = -0x00010000.
func readMP4Tkhd(r io.ReadSeeker, trakStart, trakEnd int64) int64 {
	if _, err := r.Seek(trakStart, io.SeekStart); err != nil {
		return 0
	}
	var rotation int64
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= trakEnd {
			break
		}
		size, name, contentLen, err := readBoxHeader(r)
		if err != nil {
			return 0
		}
		next := pos + size
		if name == "tkhd" {
			rotation = decodeTkhdRotation(r, contentLen)
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return 0
		}
	}
	return rotation
}

func decodeTkhdRotation(r io.ReadSeeker, contentLen int64) int64 {
	const v0Off, v1Off = 40, 52
	if contentLen < v0Off+36 {
		return 0
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0
	}
	matOff := v0Off
	if buf[0] == 1 {
		matOff = v1Off
	}
	if int64(matOff+36) > contentLen {
		return 0
	}
	a := int32(binary.BigEndian.Uint32(buf[matOff : matOff+4]))
	b := int32(binary.BigEndian.Uint32(buf[matOff+4 : matOff+8]))
	c := int32(binary.BigEndian.Uint32(buf[matOff+12 : matOff+16]))
	d := int32(binary.BigEndian.Uint32(buf[matOff+16 : matOff+20]))

	const fp1 = int32(0x00010000)
	const fpN = -fp1
	switch {
	case a == fp1 && b == 0 && c == 0 && d == fp1:
		return 0
	case a == 0 && b == fp1 && c == fpN && d == 0:
		return 90
	case a == fpN && b == 0 && c == 0 && d == fpN:
		return 180
	case a == 0 && b == fpN && c == fp1 && d == 0:
		return 270
	}
	return 0
}

// readVisualSampleEntryChildren walks the children of a VisualSampleEntry
// (avc1 / hvc1 / av01 / vpcC / etc.) looking for the btrt box. btrt is a
// MPEG-4 BitRateBox carrying codec-stored bitrates:
//
//	uint32 bufferSizeDB
//	uint32 maxBitrate    (bits per second)
//	uint32 avgBitrate    (bits per second)
//
// We surface avgBitrate as videoInfo.NominalBitrate (kbps), preferring
// it over maxBitrate. If only maxBitrate is non-zero it falls through.
//
// The reader is positioned just past the 78-byte VisualSampleEntry body;
// `end` is the end of the parent sample-entry box.
func readVisualSampleEntryChildren(r io.ReadSeeker, end int64, info *videoInfo) {
	for {
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return
		}
		size, name, _, err := readBoxHeader(r)
		if err != nil {
			return
		}
		next := pos + size
		switch name {
		case "btrt":
			var body [12]byte
			if _, err := io.ReadFull(r, body[:]); err == nil {
				avg := binary.BigEndian.Uint32(body[8:12])
				maxBR := binary.BigEndian.Uint32(body[4:8])
				if avg > 0 {
					info.NominalBitrate = int64(avg) / 1000
				} else if maxBR > 0 {
					info.NominalBitrate = int64(maxBR) / 1000
				}
			}
		case "colr":
			// ColourInformationBox. Layout:
			//   4 bytes colour_type ("nclx" / "rICC" / "prof")
			//   For "nclx":
			//     uint16 colour_primaries
			//     uint16 transfer_characteristics
			//     uint16 matrix_coefficients
			//     uint8  full_range_flag (top bit; rest reserved)
			var head [4]byte
			if _, err := io.ReadFull(r, head[:]); err == nil && string(head[:]) == "nclx" {
				var body [7]byte
				if _, err := io.ReadFull(r, body[:]); err == nil {
					primaries := binary.BigEndian.Uint16(body[0:2])
					transfer := binary.BigEndian.Uint16(body[2:4])
					info.ColourPrimaries = nameColourPrimaries(primaries)
					info.ColourTransfer = nameColourTransfer(transfer)
					info.IsHDR = transfer == 16 || transfer == 18
				}
			}
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return
		}
	}
}

// nameColourPrimaries maps the H.273 colour_primaries enum to a friendly
// short name. Anything outside the recognised set yields "".
func nameColourPrimaries(v uint16) string {
	switch v {
	case 1:
		return "bt709"
	case 9:
		return "bt2020"
	case 11, 12: // 11 = SMPTE RP 431-2 (DCI-P3), 12 = SMPTE EG 432-1 (Display P3)
		return "p3"
	}
	return ""
}

// nameColourTransfer maps transfer_characteristics to a friendly short name.
// PQ (16) and HLG (18) are the two HDR signals; everything else maps to a
// short name where it makes sense, "" otherwise.
func nameColourTransfer(v uint16) string {
	switch v {
	case 1, 6, 14, 15: // BT.709 / BT.601 / BT.2020 SDR variants
		return "bt709"
	case 16:
		return "pq"
	case 18:
		return "hlg"
	}
	return ""
}

// readVideoMVHD pulls duration_seconds from mvhd; same logic as audio MVHD
// but writes to videoInfo.
func readVideoMVHD(r io.ReadSeeker, contentLen int64, info *videoInfo) error {
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
		if len(buf) < 32 {
			return errors.New("mvhd v1 too short")
		}
		timescale = uint64(binary.BigEndian.Uint32(buf[20:24]))
		duration = binary.BigEndian.Uint64(buf[24:32])
	} else {
		if len(buf) < 20 {
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

// readMDHDTimescale reads the 4-byte timescale from a track-level media
// header. Layout same shape as mvhd (version-conditional offsets).
func readMDHDTimescale(r io.ReadSeeker, contentLen int64) uint32 {
	if contentLen < 4 {
		return 0
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0
	}
	version := buf[0]
	if version == 1 {
		if len(buf) < 24 {
			return 0
		}
		return binary.BigEndian.Uint32(buf[20:24])
	}
	if len(buf) < 16 {
		return 0
	}
	return binary.BigEndian.Uint32(buf[12:16])
}

// readVideoSTSD parses stsd. For video tracks we read VisualSampleEntry
// fields (codec name, width, height). For audio tracks, we capture only
// the codec name.
func readVideoSTSD(r io.ReadSeeker, end int64, trackType string, info *videoInfo) error {
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
		switch trackType {
		case "vide":
			// VisualSampleEntry: 6 reserved + 2 ref_index + 16 reserved
			// + 2 width + 2 height + ... (78 bytes total).
			var body [78]byte
			if _, err := io.ReadFull(r, body[:]); err != nil {
				return err
			}
			info.VideoCodec = mp4VideoCodecName(name)
			info.Width = int64(binary.BigEndian.Uint16(body[24:26]))
			info.Height = int64(binary.BigEndian.Uint16(body[26:28]))
			// Walk remaining children of the visual sample entry for the
			// btrt box (codec-stored avg bitrate). Children may include
			// avcC / hvcC / colr / btrt / pasp / etc. — we only consume btrt.
			readVisualSampleEntryChildren(r, next, info)
			return nil
		case "soun":
			info.AudioCodec = mp4AudioCodecName(name)
			// AudioSampleEntry preamble layout matches the standalone
			// MP4 audio parser: channels at +16, sample_rate at +24
			// (uint16 of a fixed-point uint32). Read both opportunistically;
			// short entries fall through with zero values.
			var body [28]byte
			if _, err := io.ReadFull(r, body[:]); err == nil {
				info.AudioChannels = int64(binary.BigEndian.Uint16(body[16:18]))
				info.AudioSampleRate = int64(binary.BigEndian.Uint16(body[24:26]))
			}
			return nil
		}
		if _, err := r.Seek(next, io.SeekStart); err != nil {
			return err
		}
	}
}

// readSTTS computes frame_rate from the first stts entry. stts layout:
// 1 version + 3 flags + 4 entry_count + N × (4 sample_count, 4 sample_delta).
// frame_rate = timescale / sample_delta. For VFR there are multiple entries;
// we use the first as an approximation.
func readSTTS(r io.ReadSeeker, contentLen int64, timescale uint32, info *videoInfo) {
	if contentLen < 16 {
		return
	}
	buf := make([]byte, contentLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return
	}
	entryCount := binary.BigEndian.Uint32(buf[4:8])
	if entryCount == 0 || len(buf) < 16 {
		return
	}
	sampleDelta := binary.BigEndian.Uint32(buf[12:16])
	if sampleDelta == 0 {
		return
	}
	info.FrameRate = float64(timescale) / float64(sampleDelta)
}

// mp4VideoCodecName maps MP4 video sample-entry types to friendly names.
func mp4VideoCodecName(name string) string {
	switch name {
	case "avc1", "avc3":
		return "h264"
	case "hvc1", "hev1":
		return "h265"
	case "av01":
		return "av1"
	case "vp08":
		return "vp8"
	case "vp09":
		return "vp9"
	case "mp4v":
		return "mpeg4"
	case "encv":
		return "encrypted"
	}
	return name
}

// mp4AudioCodecName maps MP4 audio sample-entry types to friendly names.
func mp4AudioCodecName(name string) string {
	switch name {
	case "mp4a":
		return "aac"
	case "alac":
		return "alac"
	case "Opus":
		return "opus"
	case "ac-3":
		return "ac3"
	case "ec-3":
		return "eac3"
	case "samr":
		return "amr"
	}
	return name
}
