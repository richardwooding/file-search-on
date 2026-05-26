package fingerprint

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"testing"

	"golang.org/x/image/draw"
)

// renderTestImage synthesises a deterministic photo-like test image:
// low-frequency structure (multi-band gradient) plus pseudo-random
// noise. Mimics a natural photo's 1/f DCT spectrum closely enough
// that the median threshold stays stable under resize / JPEG re-
// encode. Pure gradients are a pathological pHash case — DCT coeffs
// cluster near zero and any perturbation flips many bits.
func renderTestImage(width, height int) image.Image {
	// Fixed seed → deterministic; rand/v2 ChaCha8 is stable across
	// Go versions per the proposal.
	rng := rand.New(rand.NewChaCha8([32]byte{0x01, 0x02, 0x03, 0x04}))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			fx := float64(x) / float64(width)
			fy := float64(y) / float64(height)
			// Low-frequency structure → wide variance across the 8×8
			// DCT block.
			base := 0.6*math.Sin(fx*math.Pi*3) + 0.4*math.Cos(fy*math.Pi*2)
			// Texture: per-pixel noise. Provides 1/f-ish high-frequency
			// content typical of natural photos.
			noise := (rng.Float64() - 0.5) * 0.4
			v := base + noise
			level := uint8(128 + 80*v)
			img.Set(x, y, color.RGBA{
				R: level,
				G: uint8(80 + 100*fy),
				B: uint8(80 + 100*(1-fx)),
				A: 255,
			})
		}
	}
	return img
}

// renderDifferentImage produces a visually unrelated synthetic image
// (horizontal stripes, different palette) — used as the negative
// case in similarity tests.
func renderDifferentImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		stripe := (y / 20) % 2
		var c color.RGBA
		if stripe == 0 {
			c = color.RGBA{R: 255, G: 255, B: 0, A: 255}
		} else {
			c = color.RGBA{R: 0, G: 80, B: 200, A: 255}
		}
		for x := range width {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestPHashDeterminism(t *testing.T) {
	img := renderTestImage(256, 256)
	a := PHashFromImage(img)
	b := PHashFromImage(img)
	if a != b {
		t.Errorf("same image produced different hashes: %x vs %x", a, b)
	}
	if a == 0 {
		t.Errorf("hash of a non-trivial image was 0 (degenerate)")
	}
}

// loadFixture decodes the real-photo test fixture. Robustness tests
// (resize, JPEG re-encode) need natural-photo DCT statistics —
// synthetic gradients / pure noise produce degenerate coefficient
// distributions that make the median threshold unstable. The
// committed JPEG is a tiny re-encoded crop of a public-domain
// Wikimedia Commons photo (sufficient for testing, ~7 KB).
func loadFixture(t *testing.T) image.Image {
	t.Helper()
	f, err := os.Open("testdata/sample.jpg")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return img
}

func TestPHashSurvivesResize(t *testing.T) {
	// Real photo at native size, then 0.5x downscale. pHash should be
	// near-identical — on natural-photo DCT statistics this typically
	// hits distance 0-2.
	ref := loadFixture(t)
	w := ref.Bounds().Dx() / 2
	h := ref.Bounds().Dy() / 2
	resized := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(resized, resized.Bounds(), ref, ref.Bounds(), draw.Over, nil)

	hashRef := PHashFromImage(ref)
	hashSmall := PHashFromImage(resized)
	d := Distance(hashRef, hashSmall)
	// Issue acceptance: distance ≤ 9 at 0.85 similarity for 50%
	// resize. Real photos typically hit 0-4 — assert ≤ 6 for headroom.
	if d > 6 {
		t.Errorf("resize (50%%) Hamming distance %d, expected ≤ 6 (hashes: %016x vs %016x)", d, hashRef, hashSmall)
	}
	t.Logf("resize Hamming distance: %d (similarity %.3f)", d, Similarity(hashRef, hashSmall))
}

func TestPHashSurvivesJPEGReencode(t *testing.T) {
	// Real photo → JPEG quality 50 (aggressive re-encode) → hash.
	// pHash on natural-photo DCT statistics should be near-identical.
	ref := loadFixture(t)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, ref, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	hashRef := PHashFromImage(ref)
	hashJPEG := PHashFromImage(decoded)
	d := Distance(hashRef, hashJPEG)
	if d > 6 {
		t.Errorf("JPEG q=50 re-encode Hamming distance %d, expected ≤ 6", d)
	}
	t.Logf("JPEG re-encode Hamming distance: %d (similarity %.3f)", d, Similarity(hashRef, hashJPEG))
}

func TestPHashDistinguishesDifferentImages(t *testing.T) {
	a := PHashFromImage(renderTestImage(512, 512))
	b := PHashFromImage(renderDifferentImage(512, 512))
	d := Distance(a, b)
	// Different images should produce LARGE Hamming distance. 30+ is
	// the bar for "definitely different scenes."
	if d < 20 {
		t.Errorf("visually different images had Hamming distance %d, expected ≥ 20 (hashes: %016x vs %016x)", d, a, b)
	}
	t.Logf("different-image Hamming distance: %d", d)
}

func TestPHashFromReaderPNG(t *testing.T) {
	// Encode the synthetic image as PNG, feed via PHash(io.Reader).
	img := renderTestImage(256, 256)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	hash, err := PHash(&buf)
	if err != nil {
		t.Fatalf("PHash: %v", err)
	}
	if hash == 0 {
		t.Errorf("PHash of PNG returned 0")
	}
	// Should match PHashFromImage on the same pixels.
	want := PHashFromImage(img)
	if hash != want {
		t.Errorf("PHash via reader (%016x) != PHashFromImage (%016x)", hash, want)
	}
}

func TestPHashHexRoundtrip(t *testing.T) {
	cases := []uint64{
		0x0000000000000000,
		0xffffffffffffffff,
		0x1234567890abcdef,
		0xdeadbeefcafebabe,
	}
	for _, want := range cases {
		hex := PHashHex(want)
		if len(hex) != 16 {
			t.Errorf("PHashHex(%016x) = %q, expected 16 chars", want, hex)
		}
		got, err := PHashFromHex(hex)
		if err != nil {
			t.Errorf("PHashFromHex(%q) returned error: %v", hex, err)
			continue
		}
		if got != want {
			t.Errorf("roundtrip: %016x → %q → %016x", want, hex, got)
		}
	}
}

func TestPHashFromHexRejectsMalformed(t *testing.T) {
	cases := []string{"not-hex", "abc", "deadbeef", "abcdefabcdefabcdef"} // wrong-length / non-hex
	for _, s := range cases {
		_, err := PHashFromHex(s)
		if err == nil {
			t.Errorf("PHashFromHex(%q) should have errored", s)
		}
	}
}

func TestPHashBadInput(t *testing.T) {
	// Garbage bytes that don't decode as any registered image format.
	_, err := PHash(bytes.NewReader([]byte("not an image")))
	if err == nil {
		t.Error("PHash on garbage should return error")
	}
}
