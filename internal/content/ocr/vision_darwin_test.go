//go:build darwin

package ocr

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// renderTextPNG produces a white-on-black PNG with `text` rendered via
// basicfont scaled up by `scale` for Vision-friendly readability. The
// 6x13 basicfont is too small for accurate OCR at native size; scaling
// to ~50px-tall glyphs gets stable recognition on macOS Vision.
func renderTextPNG(t *testing.T, path, text string, scale int) {
	t.Helper()
	if scale < 1 {
		scale = 1
	}

	const baseFaceHeight = 13
	w := 6*scale*len(text) + 80*scale
	h := 30 * scale

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	// Render at native size into a small intermediate, then scale up
	// via nearest-neighbour copy. Vision handles the resulting blocky
	// glyphs fine — it's tuned for screen captures.
	small := image.NewRGBA(image.Rect(0, 0, w/scale, h/scale))
	draw.Draw(small, small.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	d := &font.Drawer{
		Dst:  small,
		Src:  &image.Uniform{C: color.Black},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(20, baseFaceHeight+4),
	}
	d.DrawString(text)

	// Naive nearest-neighbour upscale into img.
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, small.At(x/scale, y/scale))
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestVisionRecognizeRoundtrip is the end-to-end happy path. Requires
// the Swift helper to be installed at $FILE_SEARCH_ON_OCR_HELPER, in
// $PATH, or as a sibling to the test binary. Skips when no helper is
// available so CI can run the package on macOS without forcing every
// developer to compile the Swift helper before running `go test`.
func TestVisionRecognizeRoundtrip(t *testing.T) {
	p := &visionProvider{}
	if !p.Available() {
		t.Skip("vision-macos helper not installed; run `make ocr-helper` to enable")
	}

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "ocr-fixture.png")
	renderTextPNG(t, imgPath, "FILE SEARCH ON OCR TEST", 5)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := p.Recognize(ctx, imgPath)
	if err != nil {
		t.Fatalf("Recognize failed: %v", err)
	}
	if result.Provider != "vision-macos" {
		t.Errorf("Provider = %q, want vision-macos", result.Provider)
	}
	// Confidence should be > 0 for a successful recognition; exact
	// value depends on Vision's model + the rendered text quality.
	if result.Confidence <= 0 {
		t.Errorf("Confidence = %f, want > 0", result.Confidence)
	}
	// We don't require an exact text match — basicfont rendering can
	// produce per-character misrecognitions. Assert that at least one
	// of the substantive words round-trips. "FILE", "SEARCH", "TEST"
	// are reasonably distinctive; one of them firing means OCR is
	// fundamentally working.
	upperText := strings.ToUpper(result.Text)
	hits := 0
	for _, needle := range []string{"FILE", "SEARCH", "TEST", "OCR"} {
		if strings.Contains(upperText, needle) {
			hits++
		}
	}
	if hits == 0 {
		t.Errorf("recognized text contained none of the expected words: %q", result.Text)
	}
}

// TestVisionRecognizeMissingFile verifies the error path — passing a
// non-existent path returns a non-nil error without panicking.
func TestVisionRecognizeMissingFile(t *testing.T) {
	p := &visionProvider{}
	if !p.Available() {
		t.Skip("vision-macos helper not installed; run `make ocr-helper` to enable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := p.Recognize(ctx, "/does/not/exist/missing.png")
	if err == nil {
		t.Error("expected error on missing file, got nil")
	}
}

// TestVisionRecognizeUnavailable verifies the "no helper" path — a
// fresh provider with no helperOK returns ErrProviderUnavailable.
func TestVisionRecognizeUnavailable(t *testing.T) {
	// Force the "no helper" state by setting the env override to a
	// non-existent path AND clearing the sync.Once state by using
	// a fresh provider instance.
	t.Setenv(helperEnvOverride, "/definitely/not/a/binary/nope")

	p := &visionProvider{}
	// Defeat the os.Executable() sibling and PATH lookup paths by
	// pointing the env override at something definitely-missing —
	// resolveHelper falls through to PATH (which won't find a
	// matching binary in most test environments either).
	// If the test environment HAS the helper in PATH, this test is
	// not meaningful — skip rather than fail.
	p.resolveHelper()
	if p.helperOK {
		t.Skip("helper found in PATH; cannot test Unavailable path here")
	}

	ctx := context.Background()
	_, err := p.Recognize(ctx, "/tmp/anything.png")
	if err == nil || err.Error() != ErrProviderUnavailable.Error() {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}
