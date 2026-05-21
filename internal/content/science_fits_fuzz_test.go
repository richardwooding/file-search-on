package content

import (
	"testing"
)

// FuzzParseFITSHeaders targets the FITS header parser + HDU walker.
// The parser handles 80-byte ASCII cards inside 2880-byte blocks with
// keyword-value records — exactly the territory where bounds-check
// bugs hide. Adversarial inputs include claimed NAXIS values that
// would overflow the data-size computation, headers without an END
// card, and giant NAXISn products. The fuzz body asserts no panic and
// sane (non-negative) values on the returned attributes.
func FuzzParseFITSHeaders(f *testing.F) {
	// Seed 1: minimal valid primary header (SIMPLE/BITPIX/NAXIS + END
	// padded to one block).
	good := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 8),
		buildCard("NAXIS", 0),
	})
	f.Add(good)

	// Seed 2: image header so the HDU walker has a data unit to
	// advance past.
	img := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", -32),
		buildCard("NAXIS", 2),
		buildCard("NAXIS1", 16),
		buildCard("NAXIS2", 16),
	})
	img = append(img, make([]byte, fitsBlockSize)...)
	f.Add(img)

	// Seed 3: truncated input — less than one card.
	f.Add([]byte("SIMPLE  =                    T"))

	// Seed 4: junk (all 0xFF) — must produce empty attrs, not crash.
	bad := make([]byte, fitsBlockSize)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	// Seed 5: adversarial NAXIS that would overflow the product if
	// the cap weren't in place. NAXIS=999 with NAXIS1..999 all set
	// to 999 → 999^999 conceptually; the data-size guard must clamp.
	overflow := buildFITSHeader([][]byte{
		buildCard("SIMPLE", true),
		buildCard("BITPIX", 64),
		buildCard("NAXIS", 999),
		buildCard("NAXIS1", 999999999),
		buildCard("NAXIS2", 999999999),
	})
	f.Add(overflow)

	// Seed 6: header with no END card — must terminate at buffer
	// boundary, not loop.
	noEnd := make([]byte, fitsBlockSize)
	copy(noEnd, buildCard("SIMPLE", true))
	copy(noEnd[fitsCardSize:], buildCard("BITPIX", 8))
	// Remaining cards are blanks — no END marker.
	for i := fitsCardSize * 2; i+fitsCardSize <= len(noEnd); i += fitsCardSize {
		copy(noEnd[i:i+fitsCardSize], buildCard("", nil))
	}
	f.Add(noEnd)

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs := parseFITSHeaders(data)
		// hdu_count is always populated when ok — must be in [1, cap].
		if hc, ok := attrs["hdu_count"].(int64); ok {
			if hc < 0 {
				t.Fatalf("hdu_count negative: %d", hc)
			}
			if hc > int64(fitsMaxHDUs) {
				t.Fatalf("hdu_count exceeds cap: %d > %d", hc, fitsMaxHDUs)
			}
		}
		// Numeric attributes must never go negative beyond what FITS
		// allows (BITPIX is signed; NAXIS / NAXIS1 / NAXIS2 are not).
		for _, key := range []string{"naxis", "naxis1", "naxis2"} {
			if v, ok := attrs[key].(int64); ok && v < 0 {
				t.Fatalf("%s went negative: %d", key, v)
			}
		}
	})
}
