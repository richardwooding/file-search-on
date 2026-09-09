package content

import (
	"context"
	"io"
	"io/fs"
	"sort"

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
	case "image/heic", "video/mp4", "video/quicktime", "audio/mp4":
		return c2pa.BMFF, true
	case "image/webp", "audio/wav", "video/x-msvideo":
		return c2pa.RIFF, true
	// DNG is TIFF, so it carries the manifest in the same IFD tag.
	case "image/tiff", "image/raw-dng":
		return c2pa.TIFF, true
	case "image/gif":
		return c2pa.GIF, true
	case "audio/mpeg":
		return c2pa.MP3, true
	case "image/svg+xml":
		return c2pa.SVG, true
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
	attrs["c2pa_attribution"] = string(c.Attribution)
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
	// A manifest the file merely CARRIES — a PDF object-level manifest over an
	// embedded image, or one nothing associates — is not a claim about this
	// file, and its signer is not this file's signer. Indexing it as
	// c2pa_signed_by would make `c2pa_signed_by.contains("Adobe")` match a
	// document Adobe never signed, which is the one thing this attribute is
	// asked for. c2pa_attribution says why it is absent.
	if c.SignedBy != "" && c.Attribution == c2pa.AttributionAsset {
		attrs["c2pa_signed_by"] = c.SignedBy
	}
	if !c.SignedAt.IsZero() {
		attrs["c2pa_signed_at"] = c.SignedAt
	}
}

// ValidateC2PA runs the full pure-Go C2PA / Content Credentials
// verification (c2pa.Validate) over a file and returns the VERIFIED
// attributes: c2pa_valid, c2pa_verified_signer, c2pa_verified_signed_at,
// c2pa_validation_status, c2pa_bound. ok is false when the content type carries no C2PA
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
	attrs["c2pa_attribution"] = string(r.Info.Attribution)
	// VerifiedSigner is empty unless the identity was actually proven, which is
	// what this attribute has always claimed to mean. Deriving it from
	// SignerChain instead reported the signer of a manifest that failed its
	// trust check, and — before the library scoped the chain to the active
	// manifest — sometimes an ingredient's signer rather than the asset's.
	// Same rule as c2pa_signed_by above, and it matters more here: the verified
	// signer is the attribute a caller trusts. VerifiedSigner() proves who
	// signed the MANIFEST, which for an embedded or unplaced one is not who
	// signed the file.
	if signer := r.VerifiedSigner(); signer != "" && r.Info.Attribution == c2pa.AttributionAsset {
		attrs["c2pa_verified_signer"] = signer
	}
	if !r.SignedAt.IsZero() {
		attrs["c2pa_verified_signed_at"] = r.SignedAt
	}
	if f := r.FirstFailure(); f != nil {
		attrs["c2pa_validation_status"] = string(f.Code)
	}
	// What the hard binding proved about THESE bytes, which c2pa_valid does not
	// say: an object-level PDF manifest (c2pa_attribution "embedded") is valid
	// with nothing hashed against the document, and so is a fragmented video
	// indexed without its fragments. The library records the state where the
	// decision is made — it is not derivable from the status codes, since an
	// update manifest's binding statuses carry the PARENT manifest's label and
	// general.unsupported is used for several unrelated things.
	attrs["c2pa_bound"] = r.Binding.String()
	addIdentityAttrs(attrs, r)
	return attrs, true
}

// addIdentityAttrs records what the active manifest's CAWG identity assertions
// say — named actors who signed over the content with their own credentials.
//
// What is indexed is deliberately narrow: how many, whether any validated,
// whether any was PROVEN, and the roles and credential kinds. NOT the names.
// An identity's name is unproven until a trust anchor vouches for it (and this
// call configures none, so it never is), and CAWG says an identity assertion
// "shall not be construed to convey either attribution or ownership" — so
// making unproven personal names searchable would build exactly the claim the
// spec rules out. Roles and sig types carry the useful search without it:
// `c2pa_identity_trusted`, or `'cawg.creator' in c2pa_identity_roles`.
//
// Gated on AttributionAsset for the same reason c2pa_verified_signer is: an
// identity in a manifest the file merely CARRIES vouches for that carried
// resource, not for this file.
func addIdentityAttrs(attrs Attributes, r c2pa.ValidationResult) {
	if len(r.Identities) == 0 || r.Info.Attribution != c2pa.AttributionAsset {
		return
	}
	attrs["c2pa_identity_count"] = int64(len(r.Identities))

	var anyValid, anyTrusted bool
	roles := map[string]bool{}
	sigTypes := map[string]bool{}
	for _, id := range r.Identities {
		anyValid = anyValid || id.Valid
		anyTrusted = anyTrusted || id.Trusted
		for _, role := range id.Roles {
			roles[role] = true
		}
		if id.SigType != "" {
			sigTypes[id.SigType] = true
		}
	}
	attrs["c2pa_identity_valid"] = anyValid
	attrs["c2pa_identity_trusted"] = anyTrusted
	attrs["c2pa_identity_roles"] = sortedKeys(roles)
	attrs["c2pa_identity_sig_types"] = sortedKeys(sigTypes)
}

// sortedKeys returns a set's members sorted, the house shape for a list
// attribute so a query sees a stable order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
