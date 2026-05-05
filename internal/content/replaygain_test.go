package content

import (
	"math"
	"testing"

	"github.com/dhowden/tag"
)

func TestParseGainDB(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"-7.42 dB", -7.42},
		{"-7.42dB", -7.42},
		{"+1.50 dB", 1.50},
		{"-7.42", -7.42},
		{"  -7.42 dB  ", -7.42},
		{"-7.42 DB", -7.42},
		{"-7.42 db", -7.42},
		{"0 dB", 0},
		// Unparseable inputs.
		{"", 0},
		{"not a number", 0},
		{"dB", 0},
	}
	for _, tc := range cases {
		got := parseGainDB(tc.in)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("parseGainDB(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestExtractReplayGain_Vorbis(t *testing.T) {
	raw := map[string]any{
		"REPLAYGAIN_TRACK_GAIN": "-7.42 dB",
		"REPLAYGAIN_ALBUM_GAIN": "-6.83 dB",
		"REPLAYGAIN_TRACK_PEAK": "0.987",     // ignored — peaks not surfaced
		"OTHER":                  "some value",
	}
	track, album := extractReplayGain(raw)
	if math.Abs(track-(-7.42)) > 1e-9 {
		t.Errorf("track = %v; want -7.42", track)
	}
	if math.Abs(album-(-6.83)) > 1e-9 {
		t.Errorf("album = %v; want -6.83", album)
	}
}

func TestExtractReplayGain_ID3v2_TXXX(t *testing.T) {
	// dhowden/tag stores TXXX user-defined-text frames as *tag.Comm
	// with Description = the descriptor and Text = the value. Multiple
	// TXXX frames coexist under "TXXX", "TXXX_1", "TXXX_2", …
	raw := map[string]any{
		"TXXX":   &tag.Comm{Description: "replaygain_track_gain", Text: "-5.10 dB"},
		"TXXX_1": &tag.Comm{Description: "replaygain_album_gain", Text: "-4.20 dB"},
		"TXXX_2": &tag.Comm{Description: "replaygain_track_peak", Text: "0.98"}, // ignored
		"TXXX_3": &tag.Comm{Description: "OTHER", Text: "ignored"},
		"TIT2":   "song title",
	}
	track, album := extractReplayGain(raw)
	if math.Abs(track-(-5.10)) > 1e-9 {
		t.Errorf("track = %v; want -5.10", track)
	}
	if math.Abs(album-(-4.20)) > 1e-9 {
		t.Errorf("album = %v; want -4.20", album)
	}
}

func TestExtractReplayGain_VorbisWinsOverTXXX(t *testing.T) {
	// If both Vorbis-style and TXXX entries are present (impossible in
	// practice — a file has one tag format — but the helper should be
	// well-defined either way), the Vorbis path runs first and the
	// TXXX path only fills in zero-valued sides.
	raw := map[string]any{
		"REPLAYGAIN_TRACK_GAIN": "-1.00 dB",
		"TXXX": &tag.Comm{Description: "replaygain_track_gain", Text: "-99.99 dB"},
	}
	track, _ := extractReplayGain(raw)
	if math.Abs(track-(-1.00)) > 1e-9 {
		t.Errorf("track = %v; want -1.00 (vorbis path should win)", track)
	}
}

func TestExtractReplayGain_Empty(t *testing.T) {
	track, album := extractReplayGain(map[string]any{})
	if track != 0 || album != 0 {
		t.Errorf("empty Raw -> track=%v album=%v; want 0,0", track, album)
	}
}
