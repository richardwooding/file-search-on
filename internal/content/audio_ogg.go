package content

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// readOGGInfo parses the Vorbis identification header in the first audio
// page (gives sample_rate + channels) and reads the granule_position from
// the last OggS page (gives total samples → duration).
//
// The ID header layout inside the page payload:
//
//	"\x01vorbis"      (7 bytes)
//	uint32 vorbis_version  (LE)
//	uint8  channels
//	uint32 sample_rate     (LE)
//	... bitrate_max/nom/min, blocksize, framing
func readOGGInfo(r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	// Read the start of the file — enough to span the first page header
	// and the Vorbis ID header (typically <100 bytes).
	head := make([]byte, 4096)
	n, _ := io.ReadFull(r, head)
	head = head[:n]

	idx := bytes.Index(head, []byte("\x01vorbis"))
	if idx < 0 || idx+30 > len(head) {
		return audioInfo{}, errors.New("no Vorbis identification header found")
	}

	channels := int64(head[idx+11])
	sampleRate := int64(binary.LittleEndian.Uint32(head[idx+12 : idx+16]))

	info := audioInfo{
		SampleRate: sampleRate,
		Channels:   channels,
	}
	if sampleRate <= 0 {
		return info, nil
	}

	// Find the last OggS page by reading the trailing 64 KiB and scanning
	// backwards for the magic. Granule position is at byte offset 6 in the
	// 27-byte page header (8 bytes, little-endian, signed int64).
	tailSize := min(fileSize, int64(64*1024))
	tail := make([]byte, tailSize)
	if _, err := r.Seek(fileSize-tailSize, io.SeekStart); err != nil {
		return info, nil
	}
	if _, err := io.ReadFull(r, tail); err != nil {
		return info, nil
	}
	for off := len(tail) - 27; off >= 0; off-- {
		if tail[off] == 'O' && tail[off+1] == 'g' && tail[off+2] == 'g' && tail[off+3] == 'S' {
			gran := int64(binary.LittleEndian.Uint64(tail[off+6 : off+14]))
			if gran > 0 {
				info.Duration = float64(gran) / float64(sampleRate)
			}
			break
		}
	}
	return info, nil
}
