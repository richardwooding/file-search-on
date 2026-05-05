package content

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// boxBytes builds a 4-byte-size + 4-byte-name + payload box.
func boxBytes(name string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], name)
	copy(out[8:], payload)
	return out
}

// buildColrNclx constructs an nclx-form colr box payload with the given
// H.273 enum values for primaries / transfer / matrix.
func buildColrNclx(primaries, transfer, matrix uint16, fullRange bool) []byte {
	body := make([]byte, 4+2+2+2+1)
	copy(body[0:4], "nclx")
	binary.BigEndian.PutUint16(body[4:6], primaries)
	binary.BigEndian.PutUint16(body[6:8], transfer)
	binary.BigEndian.PutUint16(body[8:10], matrix)
	if fullRange {
		body[10] = 0x80
	}
	return body
}

func TestReadVisualSampleEntryChildren_ColrNclx(t *testing.T) {
	cases := []struct {
		name           string
		primaries      uint16
		transfer       uint16
		wantPrimaries  string
		wantTransfer   string
		wantHDR        bool
	}{
		{"BT.709 SDR", 1, 1, "bt709", "bt709", false},
		{"BT.2020 PQ (HDR10)", 9, 16, "bt2020", "pq", true},
		{"BT.2020 HLG", 9, 18, "bt2020", "hlg", true},
		{"DCI-P3 SDR", 11, 1, "p3", "bt709", false},
		{"Display P3 SDR", 12, 1, "p3", "bt709", false},
		// Unknown enum values fall through to "" / false.
		{"unknown primaries", 99, 99, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := buildColrNclx(tc.primaries, tc.transfer, 1, false)
			children := boxBytes("colr", payload)
			r := bytes.NewReader(children)
			var info videoInfo
			readVisualSampleEntryChildren(io.ReadSeeker(r), int64(len(children)), &info)
			if info.ColourPrimaries != tc.wantPrimaries {
				t.Errorf("ColourPrimaries = %q; want %q", info.ColourPrimaries, tc.wantPrimaries)
			}
			if info.ColourTransfer != tc.wantTransfer {
				t.Errorf("ColourTransfer = %q; want %q", info.ColourTransfer, tc.wantTransfer)
			}
			if info.IsHDR != tc.wantHDR {
				t.Errorf("IsHDR = %v; want %v", info.IsHDR, tc.wantHDR)
			}
		})
	}
}

func TestReadVisualSampleEntryChildren_NonNclxColrSkipped(t *testing.T) {
	// A colr box with type "rICC" (restricted ICC profile) should not
	// populate any of the H.273 fields.
	payload := append([]byte("rICC"), 0x00, 0x01, 0x02, 0x03)
	children := boxBytes("colr", payload)
	r := bytes.NewReader(children)
	var info videoInfo
	readVisualSampleEntryChildren(r, int64(len(children)), &info)
	if info.ColourPrimaries != "" || info.ColourTransfer != "" || info.IsHDR {
		t.Errorf("rICC colr leaked fields: primaries=%q transfer=%q hdr=%v",
			info.ColourPrimaries, info.ColourTransfer, info.IsHDR)
	}
}
