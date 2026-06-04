package content

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// readOGGInfo recovers sample_rate / channels / duration from an OGG
// container, dispatching on the codec carried in the first page: Vorbis
// (the classic .ogg/.oga), Opus (.opus and modern .ogg), and FLAC-in-OGG.
// Previously only Vorbis was handled, so Opus / FLAC-in-OGG files
// detected as audio/ogg but surfaced no playback metadata (issue #325).
func readOGGInfo(r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	// Read the start of the file — enough to span the first page header
	// and the codec identification header (typically <100 bytes).
	head := make([]byte, 4096)
	n, _ := io.ReadFull(r, head)
	head = head[:n]

	switch {
	case bytes.Contains(head, []byte("\x01vorbis")):
		return readOGGVorbis(r, head, fileSize)
	case bytes.Contains(head, []byte("OpusHead")):
		return readOGGOpus(r, head, fileSize)
	case bytes.Contains(head, []byte("\x7fFLAC")):
		return readOGGFLAC(r, head, fileSize)
	}
	return audioInfo{}, errors.New("no recognised OGG codec (Vorbis/Opus/FLAC) header")
}

// oggLastGranule scans the trailing 64 KiB backwards for the last OggS
// page header and returns its granule_position (8 bytes LE at offset 6
// of the 27-byte page header). The granule unit is codec-defined:
// samples-at-sample-rate for Vorbis/FLAC, 48 kHz samples for Opus.
func oggLastGranule(r io.ReadSeeker, fileSize int64) (int64, bool) {
	tailSize := min(fileSize, int64(64*1024))
	if tailSize < 27 {
		return 0, false
	}
	tail := make([]byte, tailSize)
	if _, err := r.Seek(fileSize-tailSize, io.SeekStart); err != nil {
		return 0, false
	}
	if _, err := io.ReadFull(r, tail); err != nil {
		return 0, false
	}
	for off := len(tail) - 27; off >= 0; off-- {
		if tail[off] == 'O' && tail[off+1] == 'g' && tail[off+2] == 'g' && tail[off+3] == 'S' {
			return int64(binary.LittleEndian.Uint64(tail[off+6 : off+14])), true
		}
	}
	return 0, false
}

// readOGGVorbis parses the Vorbis identification header (channels at +11,
// sample_rate LE at +12, bitrate_nominal LE at +20) + the last granule.
func readOGGVorbis(r io.ReadSeeker, head []byte, fileSize int64) (audioInfo, error) {
	idx := bytes.Index(head, []byte("\x01vorbis"))
	if idx < 0 || idx+30 > len(head) {
		return audioInfo{}, errors.New("truncated Vorbis identification header")
	}
	info := audioInfo{
		Channels:   int64(head[idx+11]),
		SampleRate: int64(binary.LittleEndian.Uint32(head[idx+12 : idx+16])),
	}
	// bitrate_nominal (bps) → kbps; <= 0 means "not specified".
	if bn := int32(binary.LittleEndian.Uint32(head[idx+20 : idx+24])); bn > 0 {
		info.NominalBitrate = int64(bn) / 1000
	}
	if info.SampleRate > 0 {
		if g, ok := oggLastGranule(r, fileSize); ok && g > 0 {
			info.Duration = float64(g) / float64(info.SampleRate)
		}
	}
	return info, nil
}

// readOGGOpus parses the OpusHead identification header. Opus always
// decodes at 48 kHz and its granule positions are in 48 kHz samples, so
// duration = (last_granule - pre_skip) / 48000. channels at +9, pre_skip
// (LE u16) at +10.
func readOGGOpus(r io.ReadSeeker, head []byte, fileSize int64) (audioInfo, error) {
	idx := bytes.Index(head, []byte("OpusHead"))
	if idx < 0 || idx+12 > len(head) {
		return audioInfo{}, errors.New("truncated OpusHead header")
	}
	const opusRate = 48000
	preSkip := int64(binary.LittleEndian.Uint16(head[idx+10 : idx+12]))
	info := audioInfo{
		Channels:   int64(head[idx+9]),
		SampleRate: opusRate,
	}
	if g, ok := oggLastGranule(r, fileSize); ok {
		if samples := g - preSkip; samples > 0 {
			info.Duration = float64(samples) / float64(opusRate)
		}
	}
	return info, nil
}

// readOGGFLAC handles FLAC-in-OGG: the first page payload is the
// FLAC-in-Ogg mapping header (0x7F "FLAC" version + packet count) followed
// by the native "fLaC" signature + STREAMINFO. Slicing to the native
// signature lets readFLACInfo recover sample_rate / channels / bit_depth /
// duration (STREAMINFO carries total_samples, so no granule scan needed).
func readOGGFLAC(r io.ReadSeeker, head []byte, fileSize int64) (audioInfo, error) {
	idx := bytes.Index(head, []byte("\x7fFLAC"))
	if idx < 0 {
		return audioInfo{}, errors.New("no FLAC-in-OGG mapping header")
	}
	native := bytes.Index(head[idx:], []byte("fLaC"))
	if native < 0 {
		return audioInfo{}, errors.New("no native fLaC block in OGG-FLAC")
	}
	info, err := readFLACInfo(bytes.NewReader(head[idx+native:]))
	if err != nil {
		return info, err
	}
	// Streamed FLAC-in-OGG often leaves STREAMINFO total_samples = 0
	// (unknown length); fall back to the OGG granule, which counts
	// samples at the FLAC sample rate.
	if info.Duration == 0 && info.SampleRate > 0 {
		if g, ok := oggLastGranule(r, fileSize); ok && g > 0 {
			info.Duration = float64(g) / float64(info.SampleRate)
		}
	}
	return info, nil
}
