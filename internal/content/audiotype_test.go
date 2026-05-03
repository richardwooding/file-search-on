package content_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

// id3v2Frame is one text frame: 4-byte ID, 4-byte big-endian size, 2-byte
// flags, 1-byte encoding (ISO-8859-1 = 0), then the text bytes.
func id3v2Frame(id, text string) []byte {
	var b bytes.Buffer
	b.WriteString(id)
	size := uint32(1 + len(text)) // encoding byte + text
	b.Write([]byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)})
	b.Write([]byte{0, 0}) // flags
	b.WriteByte(0)        // encoding: ISO-8859-1
	b.WriteString(text)
	return b.Bytes()
}

// buildID3v2File constructs a valid ID3v2.3 tag with the given text frames
// and returns the bytes. dhowden/tag reads ID3v2 from the start of the file
// regardless of whether subsequent MP3 audio frames are present.
func buildID3v2File(frames map[string]string) []byte {
	var framesBuf bytes.Buffer
	for id, text := range frames {
		framesBuf.Write(id3v2Frame(id, text))
	}

	var out bytes.Buffer
	out.WriteString("ID3")
	out.Write([]byte{3, 0, 0}) // version 2.3.0, no flags

	// Synchsafe-encode the frame block size (7 bits per byte).
	sz := uint32(framesBuf.Len())
	out.Write([]byte{
		byte((sz >> 21) & 0x7F),
		byte((sz >> 14) & 0x7F),
		byte((sz >> 7) & 0x7F),
		byte(sz & 0x7F),
	})
	out.Write(framesBuf.Bytes())
	return out.Bytes()
}

func TestAudioMP3Tags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	body := buildID3v2File(map[string]string{
		"TIT2": "Paranoid Android",  // title
		"TPE1": "Radiohead",         // artist
		"TALB": "OK Computer",       // album
		"TPE2": "Radiohead",         // album artist
		"TCOM": "Thom Yorke",        // composer
		"TYER": "1997",              // year (ID3v2.3)
		"TRCK": "6",                 // track
		"TCON": "Alternative Rock",  // genre
	})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "audio/mpeg" {
		t.Fatalf("Detect: got %v, want audio/mpeg", ct)
	}
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	cases := []struct {
		key  string
		want any
	}{
		{"title", "Paranoid Android"},
		{"artist", "Radiohead"},
		{"album", "OK Computer"},
		{"album_artist", "Radiohead"},
		{"composer", "Thom Yorke"},
		{"year", int64(1997)},
		{"track", int64(6)},
		{"genre", "Alternative Rock"},
	}
	for _, tc := range cases {
		if got := attrs[tc.key]; got != tc.want {
			t.Errorf("%s = %v (%T), want %v (%T)", tc.key, got, got, tc.want, tc.want)
		}
	}
}

func TestAudioMP3NoTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "untagged.mp3")
	// A few bytes of garbage — not valid ID3v2 but the dispatcher should
	// tolerate and return an empty Attributes rather than crashing.
	if err := os.WriteFile(path, []byte("garbage data"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	if ct == nil || ct.Name() != "audio/mpeg" {
		t.Fatalf("Detect: got %v, want audio/mpeg", ct)
	}
	attrs, err := ct.Attributes(t.Context(), path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if _, ok := attrs["title"]; ok {
		t.Errorf("title present on untagged file: %v", attrs["title"])
	}
	if _, ok := attrs["artist"]; ok {
		t.Errorf("artist present on untagged file: %v", attrs["artist"])
	}
}

func TestAudioTypesRegistered(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		ext  string
		want string
	}{
		{".mp3", "audio/mpeg"},
		{".m4a", "audio/mp4"},
		{".m4b", "audio/mp4"},
		{".aac", "audio/mp4"},
		{".flac", "audio/flac"},
		{".ogg", "audio/ogg"},
		{".oga", "audio/ogg"},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			path := filepath.Join(dir, "f"+tc.ext)
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			ct := content.DefaultRegistry().Detect(path)
			if ct == nil || ct.Name() != tc.want {
				t.Errorf("Detect(%s): got %v, want %s", tc.ext, ct, tc.want)
			}
		})
	}
}

func TestAudioRespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cancel.mp3")
	body := buildID3v2File(map[string]string{"TIT2": "x", "TPE1": "y"})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ct.Attributes(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Attributes(cancelled): err = %v, want context.Canceled", err)
	}
}
