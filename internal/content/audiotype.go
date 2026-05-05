package content

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"github.com/dhowden/tag"
)

func init() {
	Register(&audioType{name: "audio/mpeg", exts: []string{".mp3"}, magic: [][]byte{[]byte("ID3"), {0xFF, 0xFB}, {0xFF, 0xF3}, {0xFF, 0xF2}}})
	Register(&audioType{name: "audio/mp4", exts: []string{".m4a", ".m4b", ".m4p", ".aac"}, magic: nil})
	Register(&audioType{name: "audio/flac", exts: []string{".flac"}, magic: [][]byte{[]byte("fLaC")}})
	Register(&audioType{name: "audio/ogg", exts: []string{".ogg", ".oga"}, magic: [][]byte{[]byte("OggS")}})
}

type audioType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (a *audioType) Name() string         { return a.name }
func (a *audioType) Extensions() []string { return a.exts }
func (a *audioType) MagicBytes() [][]byte { return a.magic }

// Attributes reads audio tags via dhowden/tag. The library auto-detects the
// container format (ID3v1/v2 for MP3, MP4 atoms for M4A, Vorbis comments for
// FLAC/OGG) so a single ReadFrom covers all four registered types. Header-
// only reads — sub-millisecond per file. Tag-only; duration / bitrate are
// out of scope for v1 and would need format-specific decoders.
func (a *audioType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{}

	rs, fileSize, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = closer() }()

	if m, err := tag.ReadFrom(rs); err == nil {
		if v := m.Title(); v != "" {
			attrs["title"] = v
		}
		if v := m.Artist(); v != "" {
			attrs["artist"] = v
		}
		if v := m.Album(); v != "" {
			attrs["album"] = v
		}
		if v := m.AlbumArtist(); v != "" {
			attrs["album_artist"] = v
		}
		if v := m.Composer(); v != "" {
			attrs["composer"] = v
		}
		if v := m.Genre(); v != "" {
			attrs["genre"] = v
		}
		if y := m.Year(); y > 0 {
			attrs["year"] = int64(y)
		}
		if t, _ := m.Track(); t > 0 {
			attrs["track"] = int64(t)
		}
		// ReplayGain track / album gain in dB (negative = quieter,
		// positive = louder). Format-specific extraction sits in the
		// helper; populated for Vorbis comments (FLAC + OGG) and
		// ID3v2 TXXX frames (MP3). M4A iTunes ---- atoms are not yet
		// covered — out of scope for the initial #33 implementation;
		// flagged in the schema doc.
		if track, album := extractReplayGain(m.Raw()); track != 0 || album != 0 {
			if track != 0 {
				attrs["replaygain_track_gain"] = track
			}
			if album != 0 {
				attrs["replaygain_album_gain"] = album
			}
		}
	}
	// Tag-read failure isn't fatal — playback metadata still flows through
	// the per-format parsers below.

	// Playback metadata via per-format binary parsing. Each parser is bounded
	// (header + at most a 64 KiB tail for OGG); they don't take ctx because
	// the entry-point check at the top covers cancellation between files.
	_, _ = rs.Seek(0, io.SeekStart)
	if info, err := readAudioInfo(a.name, rs, fileSize); err == nil {
		if info.Duration > 0 {
			attrs["duration"] = info.Duration
			if br := int64(float64(fileSize) * 8 / info.Duration / 1000); br > 0 {
				attrs["bitrate"] = br
			}
		}
		if info.SampleRate > 0 {
			attrs["sample_rate"] = info.SampleRate
		}
		if info.Channels > 0 {
			attrs["channels"] = info.Channels
		}
		if info.BitDepth > 0 {
			attrs["bit_depth"] = info.BitDepth
		}
		if info.NominalBitrate > 0 {
			attrs["nominal_bitrate"] = info.NominalBitrate
		}
	}
	return attrs, nil
}

// readAudioInfo dispatches to the format-specific parser by content-type name.
func readAudioInfo(name string, r io.ReadSeeker, fileSize int64) (audioInfo, error) {
	switch name {
	case "audio/flac":
		return readFLACInfo(r)
	case "audio/mpeg":
		return readMP3Info(r, fileSize)
	case "audio/ogg":
		return readOGGInfo(r, fileSize)
	case "audio/mp4":
		return readMP4Info(r, fileSize)
	}
	return audioInfo{}, errors.New("unsupported audio format")
}
