package content

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildESDS returns an esds box body with the given avg / max bitrates
// in bps. avgFlag controls whether the avgBitrate slot is populated
// (false → 0 in the slot, exercising the maxBitrate fallback).
//
// Layout written:
//
//	4 bytes  version + flags (zero)
//	1 byte   tag 0x03 (ES_DescrTag)
//	4 bytes  long-form size (0x80 0x80 0x80 NN)
//	2 bytes  ES_ID (0x0001)
//	1 byte   flags (zero — no optional fields follow)
//	1 byte   tag 0x04 (DecoderConfigDescrTag)
//	4 bytes  long-form size
//	1 byte   objectTypeIndication (0x40 = AAC LC)
//	1 byte   streamType byte (0x15 = audio + reserved)
//	3 bytes  bufferSizeDB (zero)
//	4 bytes  maxBitrate
//	4 bytes  avgBitrate
func buildESDS(maxBitrate, avgBitrate uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x00, 0x00, 0x00, 0x00})         // version + flags
	b.WriteByte(0x03)                                // ES_DescrTag
	b.Write([]byte{0x80, 0x80, 0x80, 0x19})         // size (long form, 25 bytes follow)
	b.Write([]byte{0x00, 0x01})                      // ES_ID
	b.WriteByte(0x00)                                // ES_Descriptor flags
	b.WriteByte(0x04)                                // DecoderConfigDescrTag
	b.Write([]byte{0x80, 0x80, 0x80, 0x0D})         // size (long form, 13 bytes follow)
	b.WriteByte(0x40)                                // objectTypeIndication (AAC LC)
	b.WriteByte(0x15)                                // streamType + flags
	b.Write([]byte{0x00, 0x00, 0x00})                // bufferSizeDB
	var bb [4]byte
	binary.BigEndian.PutUint32(bb[:], maxBitrate)
	b.Write(bb[:])
	binary.BigEndian.PutUint32(bb[:], avgBitrate)
	b.Write(bb[:])
	return b.Bytes()
}

func TestReadESDSBitrates(t *testing.T) {
	cases := []struct {
		name      string
		maxBitrate uint32
		avgBitrate uint32
		wantOk    bool
		wantAvg   uint32
		wantMax   uint32
	}{
		{"avg + max both populated", 192_000, 184_000, true, 184_000, 192_000},
		{"only max populated (avg = 0)", 64_000, 0, true, 0, 64_000},
		{"only avg populated (max = 0)", 0, 128_000, true, 128_000, 0},
		{"both zero — descriptor present but uninformative", 0, 0, true, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := buildESDS(tc.maxBitrate, tc.avgBitrate)
			avg, maxBR, ok := readESDSBitrates(body)
			if ok != tc.wantOk {
				t.Errorf("ok = %v; want %v", ok, tc.wantOk)
			}
			if avg != tc.wantAvg {
				t.Errorf("avg = %d; want %d", avg, tc.wantAvg)
			}
			if maxBR != tc.wantMax {
				t.Errorf("max = %d; want %d", maxBR, tc.wantMax)
			}
		})
	}
}

func TestReadESDSBitratesMalformed(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"too short for v+f", []byte{0, 0, 0}},
		{"missing ES_DescrTag", []byte{0, 0, 0, 0, 0xFF}},
		{"truncated after ES_DescrTag", []byte{0, 0, 0, 0, 0x03}},
		{"missing DecoderConfigDescrTag", []byte{
			0x00, 0x00, 0x00, 0x00, // v+f
			0x03, 0x80, 0x80, 0x80, 0x10, // ES_DescrTag + size 16
			0x00, 0x01, 0x00, // ES_ID + flags
			0xFF, // wrong tag (expected 0x04)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ok := readESDSBitrates(tc.body)
			if ok {
				t.Errorf("ok = true; want false (malformed input %q should not parse)", tc.name)
			}
		})
	}
}

func TestReadDescriptorSize(t *testing.T) {
	cases := []struct {
		name        string
		buf         []byte
		wantSize    uint32
		wantConsumed int
	}{
		{"single byte (no continuation)", []byte{0x25}, 0x25, 1},
		{"long form (4 bytes, ffmpeg style)", []byte{0x80, 0x80, 0x80, 0x25}, 0x25, 4},
		{"two-byte form", []byte{0x81, 0x40}, 0xC0, 2}, // (0x01 << 7) | 0x40 = 0xC0
		// Spec caps at 4 bytes; longer streams of continuation bytes return 0,0.
		{"5+ continuation bytes — malformed", []byte{0x80, 0x80, 0x80, 0x80, 0x25}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSize, gotConsumed := readDescriptorSize(tc.buf)
			if gotSize != tc.wantSize {
				t.Errorf("size = %d; want %d", gotSize, tc.wantSize)
			}
			if gotConsumed != tc.wantConsumed {
				t.Errorf("consumed = %d; want %d", gotConsumed, tc.wantConsumed)
			}
		})
	}
}
