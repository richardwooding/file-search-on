package content

import (
	"context"
	"os"

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
func (a *audioType) Attributes(ctx context.Context, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{}

	f, err := os.Open(path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = f.Close() }()

	m, err := tag.ReadFrom(f)
	if err != nil {
		// File is detected as audio but has no tags / bad container header.
		// Return what we have so far (the empty map) — the file still flows
		// through the search; users just can't filter on tag fields.
		return attrs, nil
	}

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
	return attrs, nil
}
