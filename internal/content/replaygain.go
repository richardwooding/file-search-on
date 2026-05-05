package content

import (
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// extractReplayGain pulls ReplayGain track and album gain values (in dB)
// from the dhowden/tag Raw() map. Three storage paths are covered:
//
//   - **Vorbis comments** (FLAC + OGG): keys like REPLAYGAIN_TRACK_GAIN
//     map directly to a string value such as "-7.42 dB". Vorbis tag
//     names are case-insensitive per spec but dhowden/tag normalises
//     them to upper case, so we look for the canonical upper-case keys.
//   - **ID3v2** (MP3): each ReplayGain value lives in its own TXXX
//     user-defined-text frame. Multiple TXXX frames coexist; dhowden/tag
//     stores them in Raw() under "TXXX", "TXXX_1", "TXXX_2", … each as
//     a *tag.Comm with Description = "replaygain_track_gain" (or _album_)
//     and Text = the dB value.
//   - **M4A / MP4** (iTunes-style "----" atoms): inside
//     moov/udta/meta/ilst with mean=com.apple.iTunes, name=replaygain_*.
//     dhowden/tag's MP4 reader stores these directly under the inner
//     name atom's value (lower-case "replaygain_track_gain") as a
//     plain string.
//
// Returns 0 for any value that's missing or unparseable. The three
// paths are checked in order; the first non-zero value wins per side
// (track / album).
func extractReplayGain(raw map[string]any) (track, album float64) {
	// Vorbis-comments + M4A iTunes path — both encode the value as a
	// plain string under the canonical name. Vorbis dhowden/tag
	// upper-cases the key; M4A keeps the inner-name's case (lower).
	// Probe both shapes for each side.
	for _, key := range []string{"REPLAYGAIN_TRACK_GAIN", "replaygain_track_gain"} {
		if v, ok := raw[key].(string); ok {
			if g := parseGainDB(v); g != 0 {
				track = g
				break
			}
		}
	}
	for _, key := range []string{"REPLAYGAIN_ALBUM_GAIN", "replaygain_album_gain"} {
		if v, ok := raw[key].(string); ok {
			if g := parseGainDB(v); g != 0 {
				album = g
				break
			}
		}
	}

	// ID3v2 TXXX path — iterate looking for replaygain descriptions.
	for k, v := range raw {
		if !strings.HasPrefix(k, "TXXX") {
			continue
		}
		c, ok := v.(*tag.Comm)
		if !ok {
			continue
		}
		switch strings.ToLower(c.Description) {
		case "replaygain_track_gain":
			if track == 0 {
				track = parseGainDB(c.Text)
			}
		case "replaygain_album_gain":
			if album == 0 {
				album = parseGainDB(c.Text)
			}
		}
	}
	return track, album
}

// parseGainDB parses a ReplayGain value string. Common forms:
//
//	"-7.42 dB"
//	"-7.42dB"
//	"+1.50 dB"
//	"-7.42"
//
// Whitespace and a trailing "dB" / "DB" / "db" suffix are stripped,
// then strconv.ParseFloat is run. Returns 0 for an unparseable input.
func parseGainDB(s string) float64 {
	s = strings.TrimSpace(s)
	for _, suffix := range []string{"dB", "DB", "db"} {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSpace(s[:len(s)-len(suffix)])
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
