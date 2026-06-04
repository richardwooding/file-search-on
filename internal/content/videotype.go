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

// MatchMagic disambiguates AVI from the other RIFF-container formats
// (WebP image, WAV audio) sharing the bare "RIFF" prefix: a real AVI
// carries "AVI " at bytes 8..11 (issue #322). Other video types fall
// back to the standard prefix match.
func (v *videoType) MatchMagic(head []byte) bool {
	if v.name == "video/x-msvideo" {
		return matchOffsetSigs(head, offsetSig{0, []byte("RIFF")}, offsetSig{8, []byte("AVI ")})
	}
	return matchAnyPrefix(head, v.magic)
}

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

	info, err := readVideoInfo(ctx, v.name, rs, fileSize)
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
	// Audio-track sample rate / channels (first audio track wins). The
	// existing CEL `sample_rate` and `channels` attributes are reused —
	// no schema change.
	if info.AudioSampleRate > 0 {
		attrs["sample_rate"] = info.AudioSampleRate
	}
	if info.AudioChannels > 0 {
		attrs["channels"] = info.AudioChannels
	}
	if info.Rotation > 0 {
		attrs["rotation"] = info.Rotation
	}
	if info.NominalBitrate > 0 {
		attrs["nominal_bitrate"] = info.NominalBitrate
	}
	if info.ColourPrimaries != "" {
		attrs["color_primaries"] = info.ColourPrimaries
	}
	if info.ColourTransfer != "" {
		attrs["color_transfer"] = info.ColourTransfer
	}
	if info.IsHDR {
		attrs["is_hdr"] = true
	}
	if info.Subtitles {
		attrs["subtitles"] = true
	}
	if len(info.SubtitleLanguages) > 0 {
		attrs["subtitle_languages"] = info.SubtitleLanguages
	}
	return attrs, nil
}

func readVideoInfo(ctx context.Context, name string, r io.ReadSeeker, fileSize int64) (videoInfo, error) {
	switch name {
	case "video/mp4", "video/quicktime":
		return readMP4VideoInfo(ctx, r, fileSize)
	case "video/x-matroska", "video/webm":
		return readMKVInfo(ctx, r, fileSize)
	case "video/x-msvideo":
		return readAVIInfo(ctx, r, fileSize)
	}
	return videoInfo{}, errors.New("unsupported video format")
}
