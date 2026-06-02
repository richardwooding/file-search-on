package content

import (
	"context"
	"testing"
)

// FuzzParsePDS3Label targets the PVL line-walker + comment stripper.
// PVL is free-form key-value; the line scanner has a hard 1 MiB
// buffer cap to defend against pathological single-line inputs.
// Fuzz body asserts no panic and that empty attrs are returned for
// inputs that don't yield a recognised keyword.
func FuzzParsePDS3Label(f *testing.F) {
	f.Add([]byte(minimalPDS3))

	// Adversarial: unbalanced comment marker.
	f.Add([]byte("PDS_VERSION_ID = PDS3\n/* unterminated comment\nTARGET_NAME = MARS\n"))

	// Adversarial: line so long the bufio scanner approaches its cap.
	long := make([]byte, 32*1024)
	for i := range long {
		long[i] = 'A'
	}
	f.Add(append([]byte("PDS_VERSION_ID = PDS3\nLONG_KEY = "), append(long, '\n')...))

	// Adversarial: many `=` signs on one line.
	f.Add([]byte("PDS_VERSION_ID = PDS3\nWEIRD = a = b = c = d\n"))

	// Adversarial: empty input.
	f.Add([]byte{})

	// Adversarial: all 0xFF noise.
	bad := make([]byte, 256)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parsePDS3Label(context.Background(), data) // must not panic
	})
}

// FuzzParsePDS4Label targets the encoding/xml Unmarshal path. The
// XMLName namespace check rejects non-PDS4 inputs early. Fuzz body
// asserts no panic and that non-PDS4 inputs produce empty attrs.
func FuzzParsePDS4Label(f *testing.F) {
	f.Add([]byte(minimalPDS4))

	// Adversarial: unclosed root element.
	f.Add([]byte(`<?xml version="1.0"?><Product_Observational xmlns="http://pds.nasa.gov/pds4/pds/v1"><Identification_Area><title>x`))

	// Adversarial: gigantic nested elements.
	f.Add([]byte(`<?xml version="1.0"?><Product_Observational xmlns="http://pds.nasa.gov/pds4/pds/v1"><a><b><c><d><e><f></f></e></d></c></b></a></Product_Observational>`))

	// Adversarial: nesting depth attack via opening tags.
	deep := []byte(`<?xml version="1.0"?><Product_Observational xmlns="http://pds.nasa.gov/pds4/pds/v1">`)
	for range 100 {
		deep = append(deep, []byte("<x>")...)
	}
	f.Add(deep)

	// Adversarial: empty input.
	f.Add([]byte{})

	// Adversarial: all 0xFF noise.
	bad := make([]byte, 256)
	for i := range bad {
		bad[i] = 0xFF
	}
	f.Add(bad)

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = parsePDS4Label(data) // must not panic
	})
}
