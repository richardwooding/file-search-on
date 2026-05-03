package content

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// audioInfo holds the playback metadata that the per-format parsers extract.
// All fields are zero when unknown.
type audioInfo struct {
	Duration   float64 // seconds
	SampleRate int64   // Hz
	Channels   int64
}

// readFLACInfo parses the STREAMINFO metadata block of a FLAC file. The block
// layout is fixed by the spec — 34 bytes after the 4-byte block header,
// containing sample_rate, channels, and total_samples (among other fields).
//
// References: https://xiph.org/flac/format.html#metadata_block_streaminfo
func readFLACInfo(r io.ReadSeeker) (audioInfo, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return audioInfo{}, err
	}
	if string(magic[:]) != "fLaC" {
		return audioInfo{}, errors.New("not a flac file")
	}

	// Block header: 1 byte (last-flag + type) + 3-byte length.
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return audioInfo{}, err
	}
	blockType := hdr[0] & 0x7F
	if blockType != 0 {
		return audioInfo{}, fmt.Errorf("first FLAC block is type %d, want STREAMINFO (0)", blockType)
	}
	blockLen := uint32(hdr[1])<<16 | uint32(hdr[2])<<8 | uint32(hdr[3])
	if blockLen < 18 {
		return audioInfo{}, fmt.Errorf("STREAMINFO block too short: %d bytes", blockLen)
	}

	// STREAMINFO body. We only need bytes 10-17 (sample_rate / channels /
	// total_samples); skip the first 10 (min/max block + frame sizes).
	var body [18]byte
	if _, err := io.ReadFull(r, body[:]); err != nil {
		return audioInfo{}, err
	}

	// Bytes 10-12: 20 bits sample_rate, 3 bits channels-1, 5 bits bits-per-sample-1
	// Big-endian, packed across the byte boundary.
	sampleRate := uint32(body[10])<<12 | uint32(body[11])<<4 | uint32(body[12])>>4
	channels := int64((body[12]>>1)&0x07) + 1

	// Bytes 13-17: 4 high bits of bps-1 + 36 bits total_samples.
	// We don't need bps for our shape; total_samples is the bottom 36 bits
	// of body[13..17] (5 bytes, big-endian, with the top 4 bits being the
	// remaining bits of bits-per-sample-1).
	hi := uint64(body[13]) & 0x0F
	mid := uint64(binary.BigEndian.Uint32(body[14:18]))
	totalSamples := (hi << 32) | mid

	if sampleRate == 0 || totalSamples == 0 {
		// Block is structurally valid but doesn't carry duration info.
		return audioInfo{SampleRate: int64(sampleRate), Channels: channels}, nil
	}
	return audioInfo{
		Duration:   float64(totalSamples) / float64(sampleRate),
		SampleRate: int64(sampleRate),
		Channels:   channels,
	}, nil
}
