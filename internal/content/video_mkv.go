package content

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
)

// EBML / Matroska element IDs we care about. Each ID is the raw bytes of
// the variable-length integer encoding the element ID.
var (
	mkvIDSegment        = []byte{0x18, 0x53, 0x80, 0x67}
	mkvIDInfo           = []byte{0x15, 0x49, 0xA9, 0x66}
	mkvIDTimecodeScale  = []byte{0x2A, 0xD7, 0xB1}
	mkvIDDuration       = []byte{0x44, 0x89}
	mkvIDTracks         = []byte{0x16, 0x54, 0xAE, 0x6B}
	mkvIDTrackEntry     = []byte{0xAE}
	mkvIDTrackType      = []byte{0x83}
	mkvIDCodecID        = []byte{0x86}
	mkvIDDefaultDur     = []byte{0x23, 0xE3, 0x83}
	mkvIDVideo          = []byte{0xE0}
	mkvIDPixelWidth     = []byte{0xB0}
	mkvIDPixelHeight    = []byte{0xBA}
	mkvIDAudio          = []byte{0xE1}
	mkvIDSampleRate     = []byte{0xB5} // SamplingFrequency, EBML float
	mkvIDChannels       = []byte{0x9F}
	mkvIDBitrate        = []byte{0x4F, 0xB1} // bits/sec, optional, video TrackEntry only
	mkvIDColour         = []byte{0x55, 0xB0} // child of Video; H.273 colour metadata
	mkvIDColPrimaries   = []byte{0x55, 0xBB}
	mkvIDColTransfer    = []byte{0x55, 0xBA}
	mkvIDLanguage       = []byte{0x22, 0xB5, 0x9C} // ISO 639-2 string in TrackEntry
)

// readMKVInfo walks the EBML tree of a MKV/WebM file extracting playback
// + video metadata. EBML is a binary format with variable-length integers
// for both element IDs and sizes; we hand-roll the basics.
//
// References: https://www.matroska.org/technical/elements.html
func readMKVInfo(ctx context.Context, r io.ReadSeeker, fileSize int64) (videoInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return videoInfo{}, err
	}

	// Skip the top-level EBML header element. We need to skip BOTH its
	// header (id + size VINTs, consumed by readEBMLElement) and its
	// body — real ffmpeg output populates the header with child
	// elements (EBMLVersion, EBMLReadVersion, DocType, etc.). Without
	// the body skip, the next readEBMLElement call lands on the
	// header's first child instead of the Segment.
	_, hdrSize, err := readEBMLElement(r)
	if err != nil {
		return videoInfo{}, err
	}
	if hdrSize > 0 {
		if _, err := r.Seek(hdrSize, io.SeekCurrent); err != nil {
			return videoInfo{}, err
		}
	}
	id, size, err := readEBMLElement(r)
	if err != nil {
		return videoInfo{}, err
	}
	if !idEquals(id, mkvIDSegment) {
		return videoInfo{}, errors.New("expected Segment element")
	}
	segStart, _ := r.Seek(0, io.SeekCurrent)
	segEnd := segStart + size
	if size < 0 {
		segEnd = fileSize
	}

	var info videoInfo
	var timecodeScale uint64 = 1_000_000 // default per spec: 1 ms = 1_000_000 ns
	var rawDuration float64

	if err := walkEBML(ctx, r, segStart, segEnd, func(id []byte, end int64) error {
		switch {
		case idEquals(id, mkvIDInfo):
			return walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
				switch {
				case idEquals(id, mkvIDTimecodeScale):
					if v, err := readEBMLUint(r, end); err == nil {
						timecodeScale = v
					}
				case idEquals(id, mkvIDDuration):
					if v, err := readEBMLFloat(r, end); err == nil {
						rawDuration = v
					}
				}
				return nil
			})
		case idEquals(id, mkvIDTracks):
			return walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
				if idEquals(id, mkvIDTrackEntry) {
					return readMKVTrackEntry(ctx, r, end, &info)
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return info, err
	}

	if rawDuration > 0 {
		info.Duration = rawDuration * float64(timecodeScale) / 1e9
	}
	return info, nil
}

func readMKVTrackEntry(ctx context.Context, r io.ReadSeeker, end int64, info *videoInfo) error {
	var trackType uint64
	var codecID string
	var defaultDur uint64
	var width, height uint64
	var sampleRate float64
	var channels uint64
	var bitrate uint64 // bits per second (TrackEntry/Bitrate)
	var language string

	if err := walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
		switch {
		case idEquals(id, mkvIDTrackType):
			if v, err := readEBMLUint(r, end); err == nil {
				trackType = v
			}
		case idEquals(id, mkvIDCodecID):
			if v, err := readEBMLString(r, end); err == nil {
				codecID = v
			}
		case idEquals(id, mkvIDDefaultDur):
			if v, err := readEBMLUint(r, end); err == nil {
				defaultDur = v
			}
		case idEquals(id, mkvIDBitrate):
			if v, err := readEBMLUint(r, end); err == nil {
				bitrate = v
			}
		case idEquals(id, mkvIDLanguage):
			if v, err := readEBMLString(r, end); err == nil {
				language = v
			}
		case idEquals(id, mkvIDVideo):
			return walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
				switch {
				case idEquals(id, mkvIDPixelWidth):
					if v, err := readEBMLUint(r, end); err == nil {
						width = v
					}
				case idEquals(id, mkvIDPixelHeight):
					if v, err := readEBMLUint(r, end); err == nil {
						height = v
					}
				case idEquals(id, mkvIDColour):
					return walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
						switch {
						case idEquals(id, mkvIDColPrimaries):
							if v, err := readEBMLUint(r, end); err == nil {
								info.ColourPrimaries = nameColourPrimaries(uint16(v))
							}
						case idEquals(id, mkvIDColTransfer):
							if v, err := readEBMLUint(r, end); err == nil {
								info.ColourTransfer = nameColourTransfer(uint16(v))
								info.IsHDR = v == 16 || v == 18
							}
						}
						return nil
					})
				}
				return nil
			})
		case idEquals(id, mkvIDAudio):
			return walkEBML(ctx, r, mustPos(r), end, func(id []byte, end int64) error {
				switch {
				case idEquals(id, mkvIDSampleRate):
					if v, err := readEBMLFloat(r, end); err == nil {
						sampleRate = v
					}
				case idEquals(id, mkvIDChannels):
					if v, err := readEBMLUint(r, end); err == nil {
						channels = v
					}
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return err
	}

	switch trackType {
	case 1: // video
		info.VideoCodec = mkvCodecName(codecID)
		if width > 0 {
			info.Width = int64(width)
		}
		if height > 0 {
			info.Height = int64(height)
		}
		if defaultDur > 0 {
			info.FrameRate = 1e9 / float64(defaultDur)
		}
		// First video track wins. Bitrate (0x4FB1) is bits/sec; convert to kbps.
		if bitrate > 0 && info.NominalBitrate == 0 {
			info.NominalBitrate = int64(bitrate / 1000)
		}
	case 2: // audio
		// First audio track wins; multi-track files (multi-language films)
		// use track-1 audio for the AudioCodec / sample rate / channel
		// fields. Subsequent audio tracks are ignored.
		if info.AudioCodec == "" {
			info.AudioCodec = mkvCodecName(codecID)
			if sampleRate > 0 {
				info.AudioSampleRate = int64(sampleRate)
			}
			if channels > 0 {
				info.AudioChannels = int64(channels)
			}
		}
	case 0x11: // subtitles (Matroska TrackType 17)
		info.Subtitles = true
		// Spec default language when omitted is "eng"; readEBMLString
		// will leave language="" if the field is absent. Surface
		// whatever's there (including "" when undeclared) so callers
		// can distinguish missing from explicitly empty.
		info.SubtitleLanguages = append(info.SubtitleLanguages, language)
	}
	return nil
}

// walkEBML iterates the children of a container element, invoking cb with
// each child's ID and end offset. cb may seek freely; on return, walkEBML
// re-seeks past the child before continuing. ctx is checked at the top of
// every iteration so a multi-GB MKV that's mid-walk surrenders to a
// cancelled context within one element's worth of work.
func walkEBML(ctx context.Context, r io.ReadSeeker, start, end int64, cb func(id []byte, end int64) error) error {
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pos, _ := r.Seek(0, io.SeekCurrent)
		if pos >= end {
			return nil
		}
		id, size, err := readEBMLElement(r)
		if err != nil {
			return err
		}
		bodyStart, _ := r.Seek(0, io.SeekCurrent)
		bodyEnd := bodyStart + size
		if size < 0 {
			bodyEnd = end
		}
		if err := cb(id, bodyEnd); err != nil {
			return err
		}
		if _, err := r.Seek(bodyEnd, io.SeekStart); err != nil {
			return err
		}
	}
}

// readEBMLElement reads a VINT-encoded element ID and a VINT-encoded size.
// Returns the raw ID bytes (preserving the leading length-marker bits, so
// callers can compare against the spec's ID byte sequences) and the
// decoded body length. Body length of -1 means "unknown size".
func readEBMLElement(r io.Reader) (id []byte, size int64, err error) {
	id, err = readVINTRaw(r)
	if err != nil {
		return nil, 0, err
	}
	szBytes, err := readVINTRaw(r)
	if err != nil {
		return nil, 0, err
	}
	size = vintValue(szBytes)
	if size == -1 {
		// All-ones VINT encodes "unknown size" — treat as 0; caller
		// using bodyEnd = end logic (above) will read until parent ends.
		size = -1
	}
	return id, size, nil
}

// readVINTRaw reads a single VINT and returns its raw byte sequence (so
// IDs preserve the marker bits used in spec comparisons).
func readVINTRaw(r io.Reader) ([]byte, error) {
	var first [1]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return nil, err
	}
	b := first[0]
	if b == 0 {
		return nil, errors.New("invalid VINT length marker")
	}
	// Find first set bit from MSB.
	width := 1
	for mask := byte(0x80); mask > 0; mask >>= 1 {
		if b&mask != 0 {
			break
		}
		width++
	}
	if width > 8 {
		return nil, errors.New("VINT too wide")
	}
	out := make([]byte, width)
	out[0] = b
	if width > 1 {
		if _, err := io.ReadFull(r, out[1:]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// vintValue decodes a VINT value (after stripping the length marker).
// Returns -1 for the all-ones "unknown size" encoding.
func vintValue(raw []byte) int64 {
	if len(raw) == 0 {
		return 0
	}
	width := len(raw)
	mask := byte(0xFF) >> width
	v := uint64(raw[0] & mask)
	allOnes := raw[0]&mask == mask
	for i := 1; i < width; i++ {
		v = (v << 8) | uint64(raw[i])
		if raw[i] != 0xFF {
			allOnes = false
		}
	}
	if allOnes {
		return -1
	}
	return int64(v)
}

func readEBMLUint(r io.ReadSeeker, end int64) (uint64, error) {
	pos, _ := r.Seek(0, io.SeekCurrent)
	n := end - pos
	if n <= 0 || n > 8 {
		return 0, errors.New("uint length out of range")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	var v uint64
	for _, b := range buf {
		v = (v << 8) | uint64(b)
	}
	return v, nil
}

func readEBMLFloat(r io.ReadSeeker, end int64) (float64, error) {
	pos, _ := r.Seek(0, io.SeekCurrent)
	n := end - pos
	switch n {
	case 4:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b[:]))), nil
	case 8:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b[:])), nil
	}
	return 0, errors.New("float length must be 4 or 8")
}

func readEBMLString(r io.ReadSeeker, end int64) (string, error) {
	pos, _ := r.Seek(0, io.SeekCurrent)
	n := end - pos
	if n < 0 {
		return "", errors.New("negative string length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return strings.TrimRight(string(buf), "\x00"), nil
}

func idEquals(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustPos(r io.ReadSeeker) int64 {
	pos, _ := r.Seek(0, io.SeekCurrent)
	return pos
}

// mkvCodecName maps Matroska CodecID strings to short friendly names.
// References: https://www.matroska.org/technical/codec_specs.html
func mkvCodecName(codecID string) string {
	switch codecID {
	case "V_MPEG4/ISO/AVC":
		return "h264"
	case "V_MPEGH/ISO/HEVC":
		return "h265"
	case "V_AV1":
		return "av1"
	case "V_VP8":
		return "vp8"
	case "V_VP9":
		return "vp9"
	case "V_MPEG4/ISO/SP", "V_MPEG4/ISO/ASP":
		return "mpeg4"
	case "V_MPEG2":
		return "mpeg2"
	case "V_THEORA":
		return "theora"
	case "A_AAC":
		return "aac"
	case "A_AC3":
		return "ac3"
	case "A_EAC3":
		return "eac3"
	case "A_MPEG/L3":
		return "mp3"
	case "A_OPUS":
		return "opus"
	case "A_VORBIS":
		return "vorbis"
	case "A_FLAC":
		return "flac"
	}
	return codecID
}
