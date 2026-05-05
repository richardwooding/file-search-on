package content

import (
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// extractReplayGain pulls ReplayGain track and album gain values (in dB)
// from the dhowden/tag Raw() map. Two storage paths are covered:
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
//
// **Not yet covered**: M4A iTunes-style "----" atoms inside
// moov/udta/meta/ilst (mean=com.apple.iTunes, name=replaygain_track_gain).
// Tracked for a follow-up — most Apple Music libraries use Sound Check
// (the iTunNORM atom) rather than ReplayGain anyway.
//
// Returns 0 for any value that's missing or unparseable.
func extractReplayGain(raw map[string]any) (track, album float64) {
	// Vorbis comments path — direct uppercase string keys.
	if v, ok := raw["REPLAYGAIN_TRACK_GAIN"].(string); ok {
		track = parseGainDB(v)
	}
	if v, ok := raw["REPLAYGAIN_ALBUM_GAIN"].(string); ok {
		album = parseGainDB(v)
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
