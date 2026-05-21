package content

import (
	"testing"
)

// FuzzParseCDFHeader targets the CDR + GDR binary parsers. Both are
// fixed-offset header walkers with bounds-checked field reads —
// exactly the territory where bounds-check bugs hide. Fuzz body
// asserts no panic plus sane (non-negative) variable_count /
// attribute_count surfaces.
func FuzzParseCDFHeader(f *testing.F) {
	// Seed 1: complete valid CDR + GDR.
	f.Add(buildCDFFile(3, 8, 0, 1, cdfFlagMajorityRow, 5, 10, 3))

	// Seed 2: column-major encoding=6.
	f.Add(buildCDFFile(3, 0, 0, 6, 0, 0, 0, 0))

	// Seed 3: minimum valid magic only (no CDR).
	f.Add([]byte{0xCD, 0xF3, 0x00, 0x01})

	// Seed 4: empty buffer.
	f.Add([]byte{})

	// Seed 5: 4 bytes of zero (wrong magic, but exercises the
	// length-vs-magic check).
	f.Add([]byte{0, 0, 0, 0})

	// Seed 6: all 0xFF noise — wrong magic, should return empty.
	bad := make([]byte, 256)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	// Seed 7: valid magic but adversarial NrVars / NzVars / NumAttr
	// values (all int32 min — must clamp without panic).
	body := buildCDFFile(3, 8, 0, 1, 0, -2147483648, -2147483648, -2147483648)
	f.Add(body)

	// Seed 8: valid magic + corrupted CDR (wrong RecordType byte).
	corrupted := buildCDFFile(3, 8, 0, 1, 0, 0, 0, 0)
	corrupted[12] = 0xFF
	corrupted[13] = 0xFF
	corrupted[14] = 0xFF
	corrupted[15] = 0xFF
	f.Add(corrupted)

	f.Fuzz(func(t *testing.T, data []byte) {
		attrs, gdrOffset := parseCDFCDR(data)
		if attrs == nil {
			return
		}
		// gdrOffset must be non-negative (we clamp negatives to 0).
		if gdrOffset < 0 {
			t.Fatalf("parseCDFCDR returned negative gdrOffset: %d", gdrOffset)
		}
		// If gdrOffset is reachable inside `data`, exercise the GDR
		// merge path as well.
		if gdrOffset > 0 && gdrOffset < int64(len(data)) {
			if int64(len(data))-gdrOffset >= 64 {
				mergeCDFGDR(attrs, data[gdrOffset:])
			}
		}
		// variable_count and attribute_count must never be negative.
		if vc, ok := attrs["variable_count"].(int64); ok && vc < 0 {
			t.Fatalf("variable_count went negative: %d", vc)
		}
		if ac, ok := attrs["attribute_count"].(int64); ok && ac < 0 {
			t.Fatalf("attribute_count went negative: %d", ac)
		}
	})
}
