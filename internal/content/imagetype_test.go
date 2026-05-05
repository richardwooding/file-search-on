package content_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

func imageAttrs(t *testing.T, ctx context.Context, path string) content.Attributes {
	t.Helper()
	ct := detectAt(path)
	if ct == nil {
		t.Fatalf("Detect: nil for %s", path)
	}
	attrs, err := attributesAt(ctx, ct, path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	return attrs
}

// TestEXIFLightJPEG covers the fixture from imagemeta with a partial EXIF
// segment: it has a DateTime (2019-09-29 15:19:54) and image dimensions but
// no Make/Model/GPS/lens. We assert what the fixture actually carries and
// that fields it doesn't carry are absent.
func TestEXIFLightJPEG(t *testing.T) {
	ctx := t.Context()
	attrs := imageAttrs(t, ctx, "testdata/exif-light.jpg")

	// Width/height present (50x50 from the fixture).
	if got := attrs["width"]; got != int64(50) {
		t.Errorf("width = %v, want 50", got)
	}
	if got := attrs["height"]; got != int64(50) {
		t.Errorf("height = %v, want 50", got)
	}

	// EXIF DateTime parsed into taken_at.
	if got, ok := attrs["taken_at"].(time.Time); !ok || got.IsZero() {
		t.Errorf("taken_at = %v, want a non-zero time.Time", attrs["taken_at"])
	} else if got.Year() != 2019 || got.Month() != time.September || got.Day() != 29 {
		t.Errorf("taken_at = %v, want 2019-09-29", got)
	}

	// Camera fields absent (fixture has none).
	for _, key := range []string{"camera_make", "camera_model", "lens", "gps_lat", "gps_lon"} {
		if v, ok := attrs[key]; ok && v != "" && v != float64(0) {
			t.Errorf("%s present when fixture has no value: %v", key, v)
		}
	}
}

// TestPNGNoEXIF covers the fallback path: PNG without an eXIf chunk should
// still get width/height via the stdlib image.DecodeConfig path, with no
// EXIF fields populated.
func TestPNGNoEXIF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.png")

	// Synthesize a 4x3 PNG.
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	attrs := imageAttrs(t, t.Context(), path)
	if got := attrs["width"]; got != int64(4) {
		t.Errorf("width = %v, want 4", got)
	}
	if got := attrs["height"]; got != int64(3) {
		t.Errorf("height = %v, want 3", got)
	}
	for _, key := range []string{"camera_make", "taken_at"} {
		v, ok := attrs[key]
		if !ok {
			continue
		}
		if tt, isTime := v.(time.Time); isTime && !tt.IsZero() {
			t.Errorf("%s present on plain PNG: %v", key, v)
		}
		if s, isStr := v.(string); isStr && s != "" {
			t.Errorf("%s present on plain PNG: %v", key, v)
		}
	}
}

// TestHEICRegistered confirms that .heic and .heif files are recognised and
// dispatched as image/heic. Real HEIC parsing is covered by imagemeta's own
// tests; we only verify the registration here.
func TestHEICRegistered(t *testing.T) {
	dir := t.TempDir()
	for _, ext := range []string{".heic", ".heif"} {
		path := filepath.Join(dir, "f"+ext)
		// Empty file is enough for extension-based detection.
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		ct := detectAt(path)
		if ct == nil || ct.Name() != "image/heic" {
			t.Errorf("Detect(%s): got %v, want image/heic", ext, ct)
		}
	}
}

// TestImageRespectsCancellation verifies the entry-point ctx check.
func TestImageRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ct := detectAt("testdata/exif-light.jpg")
	if ct == nil {
		t.Fatal("Detect: nil for testdata/exif-light.jpg")
	}
	_, err := attributesAt(ctx, ct, "testdata/exif-light.jpg")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Attributes(cancelled ctx): err = %v, want context.Canceled", err)
	}
}
