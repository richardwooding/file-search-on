package content

import (
	"encoding/binary"
	"errors"
	"io"
)

// readWAVInfo parses a RIFF/WAVE file's `fmt ` and `data` chunks to
// recover sample rate, channels, bit depth, and (from the data-chunk
// size / byte rate) duration. Bounded: it reads fixed-size chunk
// headers and seeks over chunk bodies, with a hard cap on the number of
// chunks walked so a malformed file can't loop forever.
//
// WAV layout: "RIFF"<u32 size>"WAVE" then a sequence of chunks, each
// 4-byte id + little-endian u32 size + body (padded to an even length).
// The `fmt ` body (PCM): audioFormat u16, channels u16, sampleRate u32,
// byteRate u32, blockAlign u16, bitsPerSample u16.
func readWAVInfo(r io.ReadSeeker) (audioInfo, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return audioInfo{}, err
	}
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return audioInfo{}, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return audioInfo{}, errors.New("not a RIFF/WAVE file")
	}

	var (
		info       audioInfo
		byteRate   int64
		haveFmt    bool
		dataSize   int64
		haveData   bool
	)
	const maxChunks = 4096
	var chunkHdr [8]byte
chunkLoop:
	for range maxChunks {
		if _, err := io.ReadFull(r, chunkHdr[:]); err != nil {
			break // EOF / truncated — return whatever we gathered
		}
		id := string(chunkHdr[0:4])
		size := int64(binary.LittleEndian.Uint32(chunkHdr[4:8]))
		switch id {
		case "fmt ":
			body := make([]byte, min(size, 64))
			if _, err := io.ReadFull(r, body); err != nil {
				break chunkLoop
			}
			if len(body) >= 16 {
				info.Channels = int64(binary.LittleEndian.Uint16(body[2:4]))
				info.SampleRate = int64(binary.LittleEndian.Uint32(body[4:8]))
				byteRate = int64(binary.LittleEndian.Uint32(body[8:12]))
				info.BitDepth = int64(binary.LittleEndian.Uint16(body[14:16]))
				haveFmt = true
			}
			// Skip any remaining fmt bytes (+ pad) we didn't read.
			if rem := size - int64(len(body)); rem > 0 {
				if _, err := r.Seek(rem+(size&1), io.SeekCurrent); err != nil {
					break chunkLoop
				}
			} else if size&1 == 1 {
				if _, err := r.Seek(1, io.SeekCurrent); err != nil {
					break chunkLoop
				}
			}
		case "data":
			dataSize = size
			haveData = true
			// Don't read the (potentially huge) sample body; seek past it.
			if _, err := r.Seek(size+(size&1), io.SeekCurrent); err != nil {
				break chunkLoop
			}
		default:
			if _, err := r.Seek(size+(size&1), io.SeekCurrent); err != nil {
				break chunkLoop
			}
		}
	}

	if !haveFmt {
		return audioInfo{}, errors.New("no fmt chunk")
	}
	// Prefer the codec-stored byte rate; fall back to deriving it.
	if byteRate == 0 && info.SampleRate > 0 && info.Channels > 0 && info.BitDepth > 0 {
		byteRate = info.SampleRate * info.Channels * info.BitDepth / 8
	}
	if haveData && byteRate > 0 {
		info.Duration = float64(dataSize) / float64(byteRate)
	}
	if byteRate > 0 {
		info.NominalBitrate = byteRate * 8 / 1000
	}
	return info, nil
}
