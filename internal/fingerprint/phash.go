package fingerprint

import (
	"encoding/hex"
	"image"
	_ "image/gif"  // register decoder
	_ "image/jpeg" // register decoder
	_ "image/png"  // register decoder
	"io"
	"math"

	"golang.org/x/image/draw"
)

// pHash (perceptual hash) for images. Algorithm per Niblack /
// Marr-Hildreth-style perceptual hashing — Zauner's pHash without
// the C++ baggage:
//
//  1. Decode → grayscale.
//  2. Resample to a fixed 32×32 grid (Catmull-Rom — sharper than
//     bilinear, ~free at this size).
//  3. 2D type-II DCT.
//  4. Take the low-frequency 8×8 sub-block (excluding the DC coeff at
//     [0][0] — it carries overall brightness, not structure).
//  5. Threshold against the median of the 63 remaining coefficients;
//     bit i is 1 iff coefficient i > median.
//  6. Pack 64 bits into a uint64.
//
// pHash is a perceptual locality-sensitive hash: visually-similar
// images (resized, re-encoded, slightly cropped or recoloured) produce
// hashes whose XOR has few set bits (small Hamming distance). Use
// `Distance` / `Similarity` (defined in simhash.go for the SimHash
// path) for comparison — both packages output uint64s on the same
// metric so the helpers work for either.
//
// Useful thresholds:
//
//	distance <= 8   ≈ 87% similar  (resize / JPEG re-save)
//	distance <= 12  ≈ 81% similar  (light crop or recolour)
//	distance >= 20  → likely different scenes
//
// pHash is robust to:
//   - resizing (1.5x downscale ≈ 0-2 bits change)
//   - JPEG re-saves at moderate quality
//   - mild colour / brightness shifts
//
// pHash is NOT robust to:
//   - heavy cropping (the spatial structure changes)
//   - rotation (use orientation-normalised variants if needed)
//   - mirror flips (likewise)
//
// All pure-Go via stdlib `image` + golang.org/x/image/draw.
//
// Issue #208.

// phashGridSize is the square edge length of the working grid (32×32 →
// 1024 samples that feed the DCT).
const phashGridSize = 32

// phashLowFreqSize is the edge length of the low-frequency sub-block
// extracted from the DCT output (8×8 → 64 coefficients).
const phashLowFreqSize = 8

// PHash returns the 64-bit perceptual hash of the image decoded from r.
// Returns 0 + an error when decoding fails or the image is unusable
// (smaller than the grid, animated GIF with no first frame, etc.).
//
// PHash is invariant under (file size, modification time) when the
// pixels don't change, so the cached value in index.Entry.PHash stays
// valid for as long as the entry itself validates.
func PHash(r io.Reader) (uint64, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return 0, err
	}
	return PHashFromImage(img), nil
}

// PHashFromImage is the pure-pixel entry point. Useful for callers
// that already have a decoded image.Image (tests, in-memory pipelines)
// and for the CEL reference-image cache where the reference is
// decoded once and reused across the walk.
func PHashFromImage(img image.Image) uint64 {
	// 1. Resample to 32×32 RGBA. Catmull-Rom is sharper than bilinear
	// at this aggressive downscale — preserves edge structure that
	// the DCT picks up as low-frequency signal.
	grid := image.NewRGBA(image.Rect(0, 0, phashGridSize, phashGridSize))
	draw.CatmullRom.Scale(grid, grid.Bounds(), img, img.Bounds(), draw.Over, nil)

	// 2. Build the grayscale matrix (Y' / Rec. 601 luma).
	gray := make([]float64, phashGridSize*phashGridSize)
	for y := range phashGridSize {
		for x := range phashGridSize {
			r, g, b, _ := grid.At(x, y).RGBA()
			// RGBA returns 16-bit values; rescale to 0..255. Rec. 601
			// luminance — same coefficients ImageMagick uses for its
			// default colorspace conversion.
			y8 := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
			gray[y*phashGridSize+x] = y8
		}
	}

	// 3. 2D DCT (type-II, separable). Coefficient C(u, v) is
	//    Σ Σ pixel(x,y) · cos((2x+1)uπ/2N) · cos((2y+1)vπ/2N)
	// We only need the top-left 8×8 so we don't compute the full 32×32
	// output — stop the outer loops at phashLowFreqSize.
	dct := make([]float64, phashLowFreqSize*phashLowFreqSize)
	// Pre-compute the cosine table once (32 rows × 8 cols).
	cos := make([]float64, phashGridSize*phashLowFreqSize)
	for i := range phashGridSize {
		for u := range phashLowFreqSize {
			cos[i*phashLowFreqSize+u] = math.Cos((2*float64(i) + 1) * float64(u) * math.Pi / (2.0 * phashGridSize))
		}
	}
	for u := range phashLowFreqSize {
		for v := range phashLowFreqSize {
			var sum float64
			for y := range phashGridSize {
				rowCosY := cos[y*phashLowFreqSize+v]
				row := gray[y*phashGridSize:]
				for x := range phashGridSize {
					sum += row[x] * cos[x*phashLowFreqSize+u] * rowCosY
				}
			}
			dct[u*phashLowFreqSize+v] = sum
		}
	}

	// 4. Median-threshold the 63 non-DC coefficients. The DC coeff at
	// [0][0] is the average brightness; including it would skew the
	// median toward overall image brightness and reduce discrimination.
	coeffs := make([]float64, 0, phashLowFreqSize*phashLowFreqSize-1)
	for i := range phashLowFreqSize * phashLowFreqSize {
		if i == 0 {
			continue
		}
		coeffs = append(coeffs, dct[i])
	}
	median := median(coeffs)

	// 5. Pack 64 bits. The DC coefficient sits at bit 0 — we always
	// set it to 0 (or any constant) since it carries no comparison
	// signal; downstream Hamming distance sees the same bit on both
	// sides and the DC bit contributes nothing.
	var hash uint64
	for i := 1; i < phashLowFreqSize*phashLowFreqSize; i++ {
		if dct[i] > median {
			hash |= 1 << i
		}
	}
	return hash
}

// PHashHex formats a 64-bit pHash as a 16-character lowercase hex
// string (the wire form used in CEL `phash` attribute output and in
// the index cache's JSON / gob encoding for human inspection).
func PHashHex(hash uint64) string {
	var buf [8]byte
	buf[0] = byte(hash >> 56)
	buf[1] = byte(hash >> 48)
	buf[2] = byte(hash >> 40)
	buf[3] = byte(hash >> 32)
	buf[4] = byte(hash >> 24)
	buf[5] = byte(hash >> 16)
	buf[6] = byte(hash >> 8)
	buf[7] = byte(hash)
	return hex.EncodeToString(buf[:])
}

// PHashFromHex parses a 16-character hex string back into a uint64.
// Returns 0 + an error when the string isn't a valid hex pHash.
// Used by the CEL `image_similar_to(reference_path, threshold)`
// function to compare against a reference image's pHash without
// re-decoding the reference image per file.
func PHashFromHex(s string) (uint64, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return 0, err
	}
	if len(b) != 8 {
		return 0, &PHashError{msg: "phash hex must decode to 8 bytes"}
	}
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7]), nil
}

// PHashError is the error type for phash-specific parse / decode
// failures. Wraps a static message for cheap allocation.
type PHashError struct{ msg string }

func (e *PHashError) Error() string { return e.msg }

// median returns the median of a slice of float64s. Modifies the input
// slice (sort in place) — pass a copy if order matters to the caller.
// For an even count, returns the lower-of-two-middle value (we don't
// need true mean-of-medians here; the threshold is robust either way).
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	// Simple sort via insertion since the slice is tiny (63 items).
	// Avoids a sort.Slice closure allocation on the hot path.
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
	return xs[len(xs)/2]
}
