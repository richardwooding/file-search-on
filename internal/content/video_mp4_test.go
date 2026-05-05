package content_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

)

// buildVideoMP4 constructs a minimal MP4 file with a video track (avc1) +
// audio track (mp4a). The video track has known width/height/timescale/
// frame-duration; the audio track is just present to confirm audio_codec.
func buildVideoMP4(timescale, duration uint32, width, height uint16, trackTimescale, frameDelta uint32) []byte {
	// Video sample entry: avc1 with VisualSampleEntry preamble (8 reserved
	// + 2 ref_index + 16 reserved + 2 width + 2 height + 50 trailing).
	avc1Body := bytes.Buffer{}
	avc1Body.Write(make([]byte, 6))                             // 6 reserved
	_ = binary.Write(&avc1Body, binary.BigEndian, uint16(1))    // ref_index
	avc1Body.Write(make([]byte, 16))                            // 16 reserved
	_ = binary.Write(&avc1Body, binary.BigEndian, width)        // width
	_ = binary.Write(&avc1Body, binary.BigEndian, height)       // height
	avc1Body.Write(make([]byte, 50))                            // remainder
	avc1 := atomBox("avc1", avc1Body.Bytes())

	// stsd containing one avc1 entry.
	stsdVideo := bytes.Buffer{}
	stsdVideo.WriteByte(0)                                          // version
	stsdVideo.Write([]byte{0, 0, 0})                                 // flags
	_ = binary.Write(&stsdVideo, binary.BigEndian, uint32(1))        // entry count
	stsdVideo.Write(avc1)

	// stts: 1 entry (sample_count=N, sample_delta=frameDelta).
	stts := bytes.Buffer{}
	stts.WriteByte(0)                                          // version
	stts.Write([]byte{0, 0, 0})                                 // flags
	_ = binary.Write(&stts, binary.BigEndian, uint32(1))        // entry count
	_ = binary.Write(&stts, binary.BigEndian, uint32(100))      // sample_count
	_ = binary.Write(&stts, binary.BigEndian, frameDelta)       // sample_delta

	stbl := bytes.Buffer{}
	stbl.Write(atomBox("stsd", stsdVideo.Bytes()))
	stbl.Write(atomBox("stts", stts.Bytes()))

	// hdlr for video.
	hdlrVideo := bytes.Buffer{}
	hdlrVideo.WriteByte(0)
	hdlrVideo.Write([]byte{0, 0, 0})
	hdlrVideo.Write(make([]byte, 4))         // pre_defined
	hdlrVideo.WriteString("vide")            // handler_type
	hdlrVideo.Write(make([]byte, 12))        // reserved
	hdlrVideo.WriteString("VideoHandler\x00") // name (null-terminated string)

	// mdhd with track timescale.
	mdhd := bytes.Buffer{}
	mdhd.WriteByte(0)                                     // version
	mdhd.Write([]byte{0, 0, 0})                            // flags
	_ = binary.Write(&mdhd, binary.BigEndian, uint32(0))   // creation_time
	_ = binary.Write(&mdhd, binary.BigEndian, uint32(0))   // modification_time
	_ = binary.Write(&mdhd, binary.BigEndian, trackTimescale)
	_ = binary.Write(&mdhd, binary.BigEndian, uint32(0))   // duration
	mdhd.Write(make([]byte, 4))                            // language + pre-def

	mdiaVideo := bytes.Buffer{}
	mdiaVideo.Write(atomBox("mdhd", mdhd.Bytes()))
	mdiaVideo.Write(atomBox("hdlr", hdlrVideo.Bytes()))
	mdiaVideo.Write(atomBox("minf", atomBox("stbl", stbl.Bytes())))

	trakVideo := atomBox("mdia", mdiaVideo.Bytes())

	// Audio track: hdlr 'soun' + stsd with a single mp4a entry.
	mp4aBody := buildMP4A(2, 44100)
	stsdAudio := bytes.Buffer{}
	stsdAudio.WriteByte(0)
	stsdAudio.Write([]byte{0, 0, 0})
	_ = binary.Write(&stsdAudio, binary.BigEndian, uint32(1))
	stsdAudio.Write(atomBox("mp4a", mp4aBody))

	hdlrAudio := bytes.Buffer{}
	hdlrAudio.WriteByte(0)
	hdlrAudio.Write([]byte{0, 0, 0})
	hdlrAudio.Write(make([]byte, 4))
	hdlrAudio.WriteString("soun")
	hdlrAudio.Write(make([]byte, 12))
	hdlrAudio.WriteString("SoundHandler\x00")

	mdiaAudio := bytes.Buffer{}
	mdiaAudio.Write(atomBox("mdhd", mdhd.Bytes())) // reuse mdhd
	mdiaAudio.Write(atomBox("hdlr", hdlrAudio.Bytes()))
	mdiaAudio.Write(atomBox("minf", atomBox("stbl", atomBox("stsd", stsdAudio.Bytes()))))
	trakAudio := atomBox("mdia", mdiaAudio.Bytes())

	moovBody := bytes.Buffer{}
	moovBody.Write(atomBox("mvhd", buildMVHD(timescale, duration)))
	moovBody.Write(atomBox("trak", trakVideo))
	moovBody.Write(atomBox("trak", trakAudio))

	var out bytes.Buffer
	out.Write(atomBox("ftyp", []byte("isomiso2avc1mp41")))
	out.Write(atomBox("moov", moovBody.Bytes()))
	return out.Bytes()
}

func TestVideoMP4Info(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mp4")
	// Movie timescale 1000, duration 100_000 = 100 seconds.
	// Width 1920x1080, track timescale 30000, frame_delta 1000 → 30 fps.
	if err := os.WriteFile(path, buildVideoMP4(1000, 100_000, 1920, 1080, 30000, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "video/mp4" {
		t.Fatalf("Detect: got %v, want video/mp4", ct)
	}
	attrs, err := attributesAt(t.Context(), ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["duration"]; got != float64(100) {
		t.Errorf("duration = %v, want 100", got)
	}
	if got := attrs["video_width"]; got != int64(1920) {
		t.Errorf("video_width = %v, want 1920", got)
	}
	if got := attrs["video_height"]; got != int64(1080) {
		t.Errorf("video_height = %v, want 1080", got)
	}
	if got := attrs["video_codec"]; got != "h264" {
		t.Errorf("video_codec = %v, want h264", got)
	}
	if got := attrs["audio_codec"]; got != "aac" {
		t.Errorf("audio_codec = %v, want aac", got)
	}
	if got, _ := attrs["frame_rate"].(float64); got < 29.9 || got > 30.1 {
		t.Errorf("frame_rate = %v, want ≈30", got)
	}
}
