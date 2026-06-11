package content

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// FuzzExtractC2PA targets the full C2PA extraction pipeline (c2pa.go):
// jpegJUMBF (APP11 marker-segment reassembly) / pngJUMBF (caBX chunk
// concatenation) → walkJUMBFBoxes (recursive LBox/TBox box tree) →
// parseC2PAManifest (CBOR claim/actions decode) → c2paSignerIdentity
// (COSE_Sign1 → x509) → rfc3161GenTime (ASN.1 timestamp). Every stage
// walks attacker-controlled bytes pulled from arbitrary files.
//
// Contract: never panic, never loop forever. We don't assert on outputs —
// corrupt input legitimately yields a zero c2paInfo (Present=false).
func FuzzExtractC2PA(f *testing.F) {
	f.Add([]byte{})
	// JPEG SOI + a minimal APP11 "JP" packet (CI+En+Z headers, Z=1) wrapping
	// one empty `jumb` box — exercises jpegJUMBF reassembly + the walker.
	f.Add([]byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xEB, 0x00, 0x12, // APP11, length 18
		0x4A, 0x50, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // "JP" + En + Z=1
		0x00, 0x00, 0x00, 0x08, 'j', 'u', 'm', 'b', // empty jumb box
	})
	// PNG signature + an empty `caBX` chunk — exercises pngJUMBF.
	f.Add([]byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x00, 'c', 'a', 'B', 'X', // len 0, type caBX
		0x00, 0x00, 0x00, 0x00, // crc
	})
	// The real signed fixture — gives the mutator a valid manifest (claim +
	// actions + COSE signature + RFC 3161 timestamp) to corrupt from.
	if b, err := os.ReadFile("testdata/fixtures/c2pa_signed.jpg"); err == nil {
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = extractC2PA(context.Background(), "jpeg", bytes.NewReader(data))
		_ = extractC2PA(context.Background(), "png", bytes.NewReader(data))
	})
}

// FuzzWalkJUMBFBoxes targets the recursive JUMBF box-tree walker and
// jumdLabel directly. Boxes are length-prefixed (LBox) and `jumb`
// superboxes nest, so this is the classic spot for integer-overflow on the
// length field and stack growth on adversarial nesting — the latter guarded
// by maxJUMBFDepth.
//
// Contract: never panic, never loop forever.
func FuzzWalkJUMBFBoxes(f *testing.F) {
	f.Add([]byte{})
	// A `jumb` superbox holding a valid `jumd` description box (16-byte type
	// UUID + 1 toggle byte, no label) plus one empty `cbor` child.
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x2A, 'j', 'u', 'm', 'b', // jumb, lbox 42
		0x00, 0x00, 0x00, 0x19, 'j', 'u', 'm', 'd', // jumd, lbox 25
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // type UUID
		0x00,                                        // toggles (no label)
		0x00, 0x00, 0x00, 0x09, 'c', 'b', 'o', 'r', 0xA0, // cbor child {}
	})
	// LBox claiming far more than the buffer holds — must bail, not index OOB.
	f.Add([]byte{0x00, 0x00, 0xFF, 0xFF, 'j', 'u', 'm', 'b'})
	// Self-nesting `jumb` chain (no valid jumd at any level) — exercises the
	// depth guard rather than recursing per stripped header.
	f.Add([]byte{
		0x00, 0x00, 0x00, 0x18, 'j', 'u', 'm', 'b', // lbox 24
		0x00, 0x00, 0x00, 0x10, 'j', 'u', 'm', 'b', // lbox 16
		0x00, 0x00, 0x00, 0x08, 'j', 'u', 'm', 'b', // lbox 8 (empty)
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		walkJUMBFBoxes(context.Background(), data, "", func(string, string, []byte) {})
	})
}

// FuzzRFC3161GenTime targets the hand-rolled ASN.1 descent that walks an
// RFC 3161 timestamp (TimeStampResp → CMS SignedData → TSTInfo) down to
// genTime. Nested encoding/asn1.Unmarshal calls over attacker bytes.
//
// Contract: never panic, never loop forever; any structural surprise yields
// the zero time.
func FuzzRFC3161GenTime(f *testing.F) {
	f.Add([]byte{})
	// SEQUENCE { INTEGER 0 } — a PKIStatusInfo-shaped prefix with no token.
	f.Add([]byte{0x30, 0x03, 0x02, 0x01, 0x00})
	// A bare GeneralizedTime TLV ("20240806215337Z") — not a ContentInfo.
	f.Add([]byte{
		0x18, 0x0F,
		'2', '0', '2', '4', '0', '8', '0', '6', '2', '1', '5', '3', '3', '7', 'Z',
	})

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = rfc3161GenTime(data)
	})
}
