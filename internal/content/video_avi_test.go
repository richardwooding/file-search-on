package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

)

// avih builds a minimal AVI main header (avih chunk body, 56 bytes).
func avih(microSecPerFrame, totalFrames, width, height uint32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, microSecPerFrame)
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // maxBytesPerSec
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // padding
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // flags
	_ = binary.Write(&b, binary.LittleEndian, totalFrames)
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // initialFrames
	_ = binary.Write(&b, binary.LittleEndian, uint32(2)) // streams
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // suggestedBufferSize
	_ = binary.Write(&b, binary.LittleEndian, width)
	_ = binary.Write(&b, binary.LittleEndian, height)
	b.Write(make([]byte, 16)) // reserved[4]
	return b.Bytes()
}

// strh builds a minimal AVI stream header (56 bytes) with the given
// fccType and fccHandler. Other fields are zero.
func strh(fccType, fccHandler string) []byte {
	var b bytes.Buffer
	b.WriteString(fccType)
	b.WriteString(fccHandler)
	b.Write(make([]byte, 56-8))
	return b.Bytes()
}

// riffChunk builds a chunk: "ID  " + 4-byte LE size + data (+ pad if odd).
func riffChunk(id string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString(id)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	if len(data)%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// listChunk builds a LIST chunk with the given list type.
func listChunk(listType string, body []byte) []byte {
	inner := append([]byte(listType), body...)
	return riffChunk("LIST", inner)
}

func buildAVI(microSecPerFrame, totalFrames, width, height uint32, videoFCC string) []byte {
	avihChunk := riffChunk("avih", avih(microSecPerFrame, totalFrames, width, height))
	strlVideo := listChunk("strl", riffChunk("strh", strh("vids", videoFCC)))
	strlAudio := listChunk("strl", riffChunk("strh", strh("auds", "0\x00\x00\x00")))
	hdrl := listChunk("hdrl", append(append(avihChunk, strlVideo...), strlAudio...))

	var body bytes.Buffer
	body.WriteString("AVI ")
	body.Write(hdrl)

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestAVIInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.avi")
	// 33333 µs/frame ≈ 30 fps; 3000 frames × 33333 µs ≈ 100 seconds.
	// 1280×720, video FCC "H264".
	if err := os.WriteFile(path, buildAVI(33333, 3000, 1280, 720, "H264"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "video/x-msvideo" {
		t.Fatalf("Detect: got %v, want video/x-msvideo", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	dur, _ := attrs["duration"].(float64)
	if dur < 99 || dur > 101 {
		t.Errorf("duration = %v, want ≈100", dur)
	}
	if got := attrs["video_width"]; got != int64(1280) {
		t.Errorf("video_width = %v, want 1280", got)
	}
	if got := attrs["video_height"]; got != int64(720) {
		t.Errorf("video_height = %v, want 720", got)
	}
	if got := attrs["video_codec"]; got != "h264" {
		t.Errorf("video_codec = %v, want h264", got)
	}
	if got, _ := attrs["frame_rate"].(float64); got < 29.9 || got > 30.1 {
		t.Errorf("frame_rate = %v, want ≈30", got)
	}
}
