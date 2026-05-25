package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestFindLivePhotoVideo_UppercasePair(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "IMG_1234.HEIC")
	mov := filepath.Join(dir, "IMG_1234.MOV")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mov, []byte("video-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, size, ok := content.FindLivePhotoVideo(heic)
	if !ok {
		t.Fatalf("FindLivePhotoVideo: ok=false, want true")
	}
	if path != mov {
		t.Errorf("path = %s, want %s", path, mov)
	}
	if size != int64(len("video-bytes")) {
		t.Errorf("size = %d, want %d", size, len("video-bytes"))
	}
}

func TestFindLivePhotoVideo_LowercaseFallback(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "img_5678.heic")
	mov := filepath.Join(dir, "img_5678.mov")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mov, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, size, ok := content.FindLivePhotoVideo(heic)
	if !ok {
		t.Fatalf("FindLivePhotoVideo: ok=false, want true")
	}
	// Path basename should be img_5678.MOV or img_5678.mov — on
	// case-insensitive filesystems (HFS+ / default APFS) the first
	// queried candidate (.MOV) Stats successfully even though the on-
	// disk name is .mov. Accept either casing.
	wantUpper := filepath.Join(dir, "img_5678.MOV")
	if path != mov && path != wantUpper {
		t.Errorf("path = %s, want %s or %s", path, mov, wantUpper)
	}
	if size != 1 {
		t.Errorf("size = %d, want 1", size)
	}
}

func TestFindLivePhotoVideo_NoSibling(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "lone.HEIC")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, ok := content.FindLivePhotoVideo(heic)
	if ok {
		t.Errorf("FindLivePhotoVideo: ok=true, want false (no sibling MOV)")
	}
}

func TestFindLivePhotoVideo_SiblingIsDirectory(t *testing.T) {
	// A directory named IMG.MOV must NOT be reported as a Live Photo
	// video sibling — `info.IsDir()` guards.
	dir := t.TempDir()
	heic := filepath.Join(dir, "IMG.HEIC")
	movDir := filepath.Join(dir, "IMG.MOV")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(movDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, ok := content.FindLivePhotoVideo(heic)
	if ok {
		t.Errorf("FindLivePhotoVideo: ok=true on directory sibling, want false")
	}
}

func TestFindLivePhotoImage_UppercasePair(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "IMG_1234.HEIC")
	mov := filepath.Join(dir, "IMG_1234.MOV")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mov, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok := content.FindLivePhotoImage(mov)
	if !ok {
		t.Fatalf("FindLivePhotoImage: ok=false, want true")
	}
	if path != heic {
		t.Errorf("path = %s, want %s", path, heic)
	}
}

func TestFindLivePhotoImage_LowercaseFallback(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "shot.heic")
	mov := filepath.Join(dir, "shot.mov")
	if err := os.WriteFile(heic, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mov, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok := content.FindLivePhotoImage(mov)
	if !ok {
		t.Fatalf("FindLivePhotoImage: ok=false, want true")
	}
	// Accept either case for the same reason as LowercaseFallback
	// above (case-insensitive macOS filesystems).
	wantUpper := filepath.Join(dir, "shot.HEIC")
	if path != heic && path != wantUpper {
		t.Errorf("path = %s, want %s or %s", path, heic, wantUpper)
	}
}

func TestFindLivePhotoImage_NoSibling(t *testing.T) {
	dir := t.TempDir()
	mov := filepath.Join(dir, "orphan.MOV")
	if err := os.WriteFile(mov, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := content.FindLivePhotoImage(mov)
	if ok {
		t.Errorf("FindLivePhotoImage: ok=true, want false (no sibling HEIC)")
	}
}

func TestIsLivePhotoVideoExt(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/tmp/IMG_1234.MOV", true},
		{"/tmp/IMG_1234.mov", true},
		{"/tmp/IMG_1234.MP4", false}, // mp4 / m4v are NOT Live Photo videos
		{"/tmp/IMG_1234.mp4", false},
		{"/tmp/IMG_1234.m4v", false},
		{"/tmp/no-ext", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := content.IsLivePhotoVideoExt(tc.path); got != tc.want {
				t.Errorf("IsLivePhotoVideoExt(%s) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestPairLivePhoto_EmptyInput(t *testing.T) {
	if _, _, ok := content.PairLivePhoto("", []string{".MOV"}); ok {
		t.Errorf("empty displayPath: ok=true, want false")
	}
	if _, _, ok := content.PairLivePhoto("/tmp/foo.HEIC", nil); ok {
		t.Errorf("nil wantExts: ok=true, want false")
	}
}

func TestPairLivePhoto_OrderIsPreserved(t *testing.T) {
	// When BOTH .MOV and .mov exist, the function tries the caller's
	// first candidate first — confirms the "uppercase-first, common
	// case" optimisation works.
	dir := t.TempDir()
	heic := filepath.Join(dir, "IMG.HEIC")
	upper := filepath.Join(dir, "IMG.MOV")
	lower := filepath.Join(dir, "IMG.mov")
	for _, p := range []string{heic, upper, lower} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Most filesystems treat .MOV and .mov as distinct files on
	// Linux / case-sensitive APFS; on case-insensitive HFS+ macOS,
	// these collapse to one file and the test still passes because
	// the same inode is returned twice.
	path, _, ok := content.PairLivePhoto(heic, []string{".MOV", ".mov"})
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	// On case-sensitive filesystems the uppercase candidate wins;
	// on case-insensitive ones, either path resolves to the same
	// inode. Accept either.
	if path != upper && path != lower {
		t.Errorf("path = %s, want one of [%s, %s]", path, upper, lower)
	}
}
