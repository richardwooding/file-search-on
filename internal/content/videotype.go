package content

import (
	"context"
	"errors"
	"io"
	"io/fs"
)

func init() {
	Register(&videoType{name: "video/mp4", exts: []string{".mp4", ".m4v"}, magic: nil})
	Register(&videoType{name: "video/quicktime", exts: []string{".mov", ".qt"}, magic: nil})
	Register(&videoType{name: "video/x-matroska", exts: []string{".mkv"}, magic: [][]byte{{0x1A, 0x45, 0xDF, 0xA3}}})
	Register(&videoType{name: "video/webm", exts: []string{".webm"}, magic: nil})
	Register(&videoType{name: "video/x-msvideo", exts: []string{".avi"}, magic: [][]byte{[]byte("RIFF")}})
}

type videoType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (v *videoType) Name() string         { return v.name }
func (v *videoType) Extensions() []string { return v.exts }
func (v *videoType) MagicBytes() [][]byte { return v.magic }

// Attributes dispatches to a per-format binary parser. Each parser is
// header- + tail-bounded (sub-millisecond on real files); ctx is honoured
// at entry to skip work when a walk is already cancelled.
func (v *videoType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{}

	rs, fileSize, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = closer() }()

	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return attrs, nil
	}

	info, err := readVideoInfo(v.name, rs, fileSize)
	if err != nil {
		return attrs, nil
	}

	if info.Duration > 0 {
		attrs["duration"] = info.Duration
		if br := int64(float64(fileSize) * 8 / info.Duration / 1000); br > 0 {
			attrs["bitrate"] = br
		}
	}
	if info.Width > 0 {
		attrs["video_width"] = info.Width
	}
	if info.Height > 0 {
		attrs["video_height"] = info.Height
	}
	if info.VideoCodec != "" {
		attrs["video_codec"] = info.VideoCodec
	}
	if info.AudioCodec != "" {
		attrs["audio_codec"] = info.AudioCodec
	}
	if info.FrameRate > 0 {
		attrs["frame_rate"] = info.FrameRate
	}
	return attrs, nil
}

func readVideoInfo(name string, r io.ReadSeeker, fileSize int64) (videoInfo, error) {
	switch name {
	case "video/mp4", "video/quicktime":
		return readMP4VideoInfo(r, fileSize)
	case "video/x-matroska", "video/webm":
		return readMKVInfo(r, fileSize)
	case "video/x-msvideo":
		return readAVIInfo(r, fileSize)
	}
	return videoInfo{}, errors.New("unsupported video format")
}
