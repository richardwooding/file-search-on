package content

import "bytes"

// This file holds the shared helpers a ContentType's MagicMatcher
// implementation uses to disambiguate formats that a plain byte-prefix
// (MagicBytes) can't tell apart — most notably containers that share a
// leading magic but carry their real form-type at a fixed later offset
// (the RIFF family: WebP / AVI / WAV all start with "RIFF" but differ at
// bytes 8..11). See the MagicMatcher interface in detector.go and the
// conflict-guard test in magic_conflict_test.go. Issue #334.

// offsetSig is a fixed byte pattern expected at a fixed offset in a
// file's head. A WebP file, for example, is {0:"RIFF", 8:"WEBP"}.
type offsetSig struct {
	at   int
	want []byte
}

// matchOffsetSigs reports whether head contains every signature at its
// declared offset. Bounds-checked: a head shorter than any signature's
// extent fails closed.
func matchOffsetSigs(head []byte, sigs ...offsetSig) bool {
	for _, s := range sigs {
		if s.at+len(s.want) > len(head) {
			return false
		}
		if !bytes.Equal(head[s.at:s.at+len(s.want)], s.want) {
			return false
		}
	}
	return true
}

// matchAnyPrefix is the default prefix match a MagicMatcher falls back
// to for the formats in its family that aren't ambiguous — equivalent
// to the detector's plain MagicBytes pass.
func matchAnyPrefix(head []byte, magics [][]byte) bool {
	for _, m := range magics {
		if bytes.HasPrefix(head, m) {
			return true
		}
	}
	return false
}
