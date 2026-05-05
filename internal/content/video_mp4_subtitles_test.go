package content

import "testing"

func TestDecodeMDHDLanguage(t *testing.T) {
	pack := func(s string) uint16 {
		if len(s) != 3 {
			t.Fatalf("language must be 3 chars, got %q", s)
		}
		return uint16((s[0]-0x60)&0x1F)<<10 |
			uint16((s[1]-0x60)&0x1F)<<5 |
			uint16((s[2]-0x60)&0x1F)
	}

	cases := []struct {
		name   string
		packed uint16
		want   string
	}{
		{"english", pack("eng"), "eng"},
		{"french", pack("fre"), "fre"},
		{"spanish", pack("spa"), "spa"},
		{"japanese", pack("jpn"), "jpn"},
		{"german", pack("ger"), "ger"},
		{"undefined sentinel", 0x55C4, ""}, // "und" placeholder treated as empty
		{"all zeros", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeMDHDLanguage(tc.packed)
			if got != tc.want {
				t.Errorf("decode(%#x) = %q; want %q", tc.packed, got, tc.want)
			}
		})
	}
}

func TestIsSubtitleTrack(t *testing.T) {
	subtitles := []string{"text", "subt", "sbtl", "clcp"}
	others := []string{"vide", "soun", "hint", "meta", ""}
	for _, t1 := range subtitles {
		if !isSubtitleTrack(t1) {
			t.Errorf("isSubtitleTrack(%q) = false; want true", t1)
		}
	}
	for _, t1 := range others {
		if isSubtitleTrack(t1) {
			t.Errorf("isSubtitleTrack(%q) = true; want false", t1)
		}
	}
}
