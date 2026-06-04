package content

import "testing"

func TestMatchOffsetSigs(t *testing.T) {
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("VP8 ")...)
	if !matchOffsetSigs(webp, offsetSig{0, []byte("RIFF")}, offsetSig{8, []byte("WEBP")}) {
		t.Error("webp head should match RIFF+WEBP")
	}
	wav := append([]byte("RIFF\x00\x00\x00\x00WAVE"), []byte("fmt ")...)
	if matchOffsetSigs(wav, offsetSig{0, []byte("RIFF")}, offsetSig{8, []byte("WEBP")}) {
		t.Error("wav head must NOT match WEBP at offset 8")
	}
	if matchOffsetSigs([]byte("RIFF\x00"), offsetSig{8, []byte("WEBP")}) {
		t.Error("short head must fail closed, not panic")
	}
}
