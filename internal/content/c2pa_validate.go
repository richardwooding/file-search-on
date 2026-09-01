package content

import (
	"context"
	"crypto/x509"
	"io"
	"io/fs"

	"github.com/richardwooding/c2pa"
)

// c2paContainer maps a content type to the C2PA carrier parser, reporting
// ok=false when we don't read manifests for it. This is the single place that
// decides which files carry provenance, so both the cheap unverified read and
// the expensive verification below stay in step.
func c2paContainer(name string) (c2pa.Container, bool) {
	switch name {
	case "image/jpeg":
		return c2pa.JPEG, true
	case "image/png":
		return c2pa.PNG, true
	case "image/heic", "video/mp4", "video/quicktime":
		return c2pa.BMFF, true
	case "pdf":
		return c2pa.PDF, true
	}
	return "", false
}

// readC2PA adds the UNVERIFIED c2pa_* attributes to attrs when rs carries a
// manifest. It only parses — no COSE signature or certificate work — so it is
// cheap enough to run over every indexed file, unlike ValidateC2PA.
func readC2PA(ctx context.Context, container c2pa.Container, rs io.ReadSeeker, attrs Attributes) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return
	}
	c := c2pa.Read(ctx, container, rs)
	if !c.Present {
		return
	}
	attrs["is_c2pa"] = true
	if c.ClaimGenerator != "" {
		attrs["c2pa_claim_generator"] = c.ClaimGenerator
	}
	if c.Title != "" {
		attrs["c2pa_title"] = c.Title
	}
	if c.Format != "" {
		attrs["c2pa_format"] = c.Format
	}
	if c.AIGenerated {
		attrs["c2pa_ai_generated"] = true
	}
	if c.SignedBy != "" {
		attrs["c2pa_signed_by"] = c.SignedBy
	}
	if !c.SignedAt.IsZero() {
		attrs["c2pa_signed_at"] = c.SignedAt
	}
}

// ValidateC2PA runs the full pure-Go C2PA / Content Credentials
// verification (c2pa.Validate) over a file and returns the VERIFIED
// attributes: c2pa_valid, c2pa_verified_signer, c2pa_verified_signed_at,
// c2pa_validation_status. ok is false when the content type carries no C2PA
// container we read (see c2paContainer) or the file has no manifest — in which
// case the caller leaves the verified attributes at their zero values.
//
// This is the authenticated counterpart to the fast, unverified attributes
// the type Attributes methods surface via readC2PA. It is EXPENSIVE (COSE
// signature + certificate-chain validation against the embedded C2PA trust
// list) and its result is clock-dependent (a certificate can expire while the
// file bytes are unchanged), so callers gate it behind an opt-in flag and it
// is deliberately never written to the (size, mtime) attribute cache.
func ValidateC2PA(ctx context.Context, fsys fs.FS, path, contentType string) (Attributes, bool) {
	container, ok := c2paContainer(contentType)
	if !ok {
		return nil, false
	}
	rs, _, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = closer() }()
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}

	// Default options: embedded C2PA trust anchors, offline (no network
	// revocation), wall-clock now. WithOnlineRevocation / WithSigningTrust
	// can be threaded through later if we expose knobs for them.
	r := c2pa.Validate(ctx, container, rs)
	if !r.Info.Present {
		return nil, false
	}

	attrs := Attributes{"c2pa_valid": r.Valid}
	if signer := verifiedSignerName(r.SignerChain); signer != "" {
		attrs["c2pa_verified_signer"] = signer
	}
	if !r.SignedAt.IsZero() {
		attrs["c2pa_verified_signed_at"] = r.SignedAt
	}
	if f := r.FirstFailure(); f != nil {
		attrs["c2pa_validation_status"] = string(f.Code)
	}
	return attrs, true
}

// verifiedSignerName derives a human-readable signer identity from the
// verified COSE signer certificate chain (leaf first): the leaf's Subject
// Common Name, falling back to its first Organization. Empty when the chain
// is absent or carries neither.
func verifiedSignerName(chain []*x509.Certificate) string {
	if len(chain) == 0 || chain[0] == nil {
		return ""
	}
	leaf := chain[0]
	if leaf.Subject.CommonName != "" {
		return leaf.Subject.CommonName
	}
	if len(leaf.Subject.Organization) > 0 {
		return leaf.Subject.Organization[0]
	}
	return ""
}
