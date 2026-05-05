package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTkhdContent returns a tkhd body (without the 8-byte box header)
// containing the given 16.16 fixed-point matrix entries (a, b, c, d) at
// the right offsets for v0 (matrix at offset 40). The remaining 5
// entries (u, v, x, y, w) are filled with the identity defaults
// (u=v=0, x=y=0, w=0x40000000 = 1.0 in 30.2 fixed). The fields before
// the matrix are zeros except for version (byte 0).
func buildTkhdContent(version byte, a, b, c, d int32) []byte {
	// Only v0 is exercised here (matrix at offset 40). v1 padding
	// would shift the matrix to offset 52; the decoder reads buf[0]
	// to switch between the two.
	const matOff = 40
	buf := make([]byte, matOff+36)
	buf[0] = version
	put := func(off int, v int32) {
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(v))
	}
	// a, b, u, c, d, v, x, y, w
	put(matOff+0, a)
	put(matOff+4, b)
	put(matOff+8, 0)               // u
	put(matOff+12, c)
	put(matOff+16, d)
	put(matOff+20, 0)              // v
	put(matOff+24, 0)              // x
	put(matOff+28, 0)              // y
	put(matOff+32, 0x40000000)     // w = 1.0 in 2.30 fixed
	return buf
}

func TestDecodeTkhdRotation(t *testing.T) {
	const fp1 = int32(0x00010000)
	const fpN = -fp1

	cases := []struct {
		name        string
		a, b, c, d  int32
		wantRotation int64
	}{
		{"identity (0°)", fp1, 0, 0, fp1, 0},
		{"90° clockwise", 0, fp1, fpN, 0, 90},
		{"180°", fpN, 0, 0, fpN, 180},
		{"270° / -90°", 0, fpN, fp1, 0, 270},
		// Non-pure-rotation matrices return 0 (skew, mirror, projective).
		{"horizontal mirror", fpN, 0, 0, fp1, 0},
		{"skew", fp1, fp1, 0, fp1, 0},
		{"all zeros", 0, 0, 0, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := buildTkhdContent(0, tc.a, tc.b, tc.c, tc.d)
			r := bytes.NewReader(body)
			got := decodeTkhdRotation(r, int64(len(body)))
			if got != tc.wantRotation {
				t.Errorf("rotation = %d, want %d", got, tc.wantRotation)
			}
		})
	}
}

func TestDecodeTkhdRotationTruncated(t *testing.T) {
	// Truncated tkhd (less than the matrix offset + 36 bytes) must
	// return 0 rather than panic.
	short := make([]byte, 30)
	r := bytes.NewReader(short)
	if got := decodeTkhdRotation(r, int64(len(short))); got != 0 {
		t.Errorf("truncated tkhd returned %d, want 0", got)
	}
}
