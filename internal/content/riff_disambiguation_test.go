package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetect_RIFFFamilyDisambiguation is the regression for issue #322:
// WebP, AVI, and WAV all start with the bare "RIFF" prefix, so the
// magic-byte pass used to return whichever registered first (WebP) for
// every RIFF file — a .wav detected as image/webp. The form-type at
// bytes 8..11 ("WEBP"/"AVI "/"WAVE") now disambiguates them, even for
// extensionless / mis-extensioned files.
func TestDetect_RIFFFamilyDisambiguation(t *testing.T) {
	dir := t.TempDir()
	// 4-byte RIFF size is arbitrary for detection; form-type at 8..11 matters.
	cases := []struct {
		file string
		body []byte
		want string
	}{
		// Extensionless → forces the magic-byte pass.
		{"riff_webp", append([]byte("RIFF\x00\x10\x00\x00WEBP"), []byte("VP8 ....")...), "image/webp"},
		{"riff_wave", append([]byte("RIFF\x00\x10\x00\x00WAVE"), []byte("fmt ....")...), "audio/wav"},
		{"riff_avi", append([]byte("RIFF\x00\x10\x00\x00AVI "), []byte("LIST....")...), "video/x-msvideo"},
		// Correct extension also resolves (extension pass wins).
		{"song.wav", append([]byte("RIFF\x00\x10\x00\x00WAVE"), []byte("fmt ....")...), "audio/wav"},
	}
	for _, tc := range cases {
		p := filepath.Join(dir, tc.file)
		if err := os.WriteFile(p, tc.body, 0o644); err != nil {
			t.Fatal(err)
		}
		ct := detectAt(p)
		if ct == nil {
			t.Errorf("%s: got nil, want %s", tc.file, tc.want)
			continue
		}
		if ct.Name() != tc.want {
			t.Errorf("%s: got %s, want %s", tc.file, ct.Name(), tc.want)
		}
	}
}
