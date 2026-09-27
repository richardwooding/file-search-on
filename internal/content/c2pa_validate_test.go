package content

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/richardwooding/c2pa"
)

// TestValidateC2PA exercises the verified C2PA path (c2pa.Validate via
// content.ValidateC2PA). The fixture is a real C2PA-signed JPEG from
// contentauth/c2pa-rs, signed by a test certificate ("C2PA Signer") that is
// NOT in the embedded C2PA trust list — so full validation is expected to
// FAIL (untrusted signer), which is exactly the signal c2pa_valid /
// c2pa_validation_status are meant to carry. We assert the verified
// attributes are populated and internally consistent, not a specific verdict.
func TestValidateC2PA(t *testing.T) {
	attrs, ok := ValidateC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/jpeg")
	if !ok {
		t.Fatal("ValidateC2PA: ok=false for a fixture with a C2PA manifest")
	}
	valid, hasValid := attrs["c2pa_valid"].(bool)
	if !hasValid {
		t.Fatalf("c2pa_valid missing or not a bool: %v", attrs["c2pa_valid"])
	}
	status, _ := attrs["c2pa_validation_status"].(string)
	if !valid && status == "" {
		t.Error("c2pa_valid is false but c2pa_validation_status is empty — a failure code should be recorded")
	}
	if valid && status != "" {
		t.Errorf("c2pa_valid is true but a failure status is set: %q", status)
	}
	// c2pa_bound answers a different question, and on this fixture the two
	// disagree: the signer is untrusted (c2pa_valid false) while the bytes are
	// the ones the manifest signed. A filter that reads validity as "the file is
	// intact", or a mismatch as "untrusted", gets both cases wrong.
	if bound, _ := attrs["c2pa_bound"].(string); bound != "verified" {
		t.Errorf("c2pa_bound = %q, want verified (the fixture's hard binding holds)", bound)
	}
}

// TestValidateC2PA_BoundIsAlwaysPopulated pins that the attribute is present
// for every validated file, including one whose binding could not be checked —
// "" would be indistinguishable from verification being switched off.
func TestValidateC2PA_BoundIsAlwaysPopulated(t *testing.T) {
	attrs, ok := ValidateC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/jpeg")
	if !ok {
		t.Fatal("ValidateC2PA: ok=false for a fixture with a C2PA manifest")
	}
	bound, present := attrs["c2pa_bound"].(string)
	if !present || bound == "" {
		t.Fatalf("c2pa_bound missing or empty: %v", attrs["c2pa_bound"])
	}
	switch bound {
	case "verified", "failed", "unevaluated", "none":
	default:
		t.Errorf("c2pa_bound = %q, not one of the four states", bound)
	}
}

// TestValidateC2PA_NoManifest confirms a plain image (no C2PA manifest)
// yields ok=false so the verified attributes stay at their zero values.
func TestValidateC2PA_NoManifest(t *testing.T) {
	if _, ok := ValidateC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/gif"); ok {
		t.Error("ValidateC2PA: ok=true for an unsupported container (image/gif)")
	}
}

// TestValidateC2PA_VerifiedSignerNeedsProof pins what the attribute promises.
// The fixture's signer is "C2PA Signer", a test certificate absent from the
// embedded trust list, so the chain is PRESENT but does not verify. Deriving
// the name from the presented chain — as this did — published an unproven
// identity under a name that says "verified".
func TestValidateC2PA_VerifiedSignerNeedsProof(t *testing.T) {
	attrs, ok := ValidateC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/jpeg")
	if !ok {
		t.Fatal("ValidateC2PA: ok=false for a fixture with a C2PA manifest")
	}
	if valid, _ := attrs["c2pa_valid"].(bool); valid {
		t.Skip("fixture now validates against the embedded trust list; this case no longer applies")
	}
	if signer, present := attrs["c2pa_verified_signer"]; present {
		t.Errorf("c2pa_verified_signer = %q for a chain that did not validate; it must be absent", signer)
	}
	if claimed, _ := attrs["c2pa_signed_by"]; claimed != nil {
		t.Errorf("the unverified claim belongs on the Read path, not here: %v", claimed)
	}
}

// CAWG identity attributes: who vouched for the file.
//
// addIdentityAttrs is a pure function of a validation result, so these build
// results directly rather than vendoring a 140 KB signed fixture to re-test the
// library's parser. What is ours to get right is which facts become searchable
// and which deliberately do not.

func identityResult(attribution c2pa.Attribution, ids ...c2pa.Identity) c2pa.ValidationResult {
	return c2pa.ValidationResult{
		Info:       c2pa.Info{Present: true, Attribution: attribution},
		Identities: ids,
	}
}

func TestAddIdentityAttrs(t *testing.T) {
	attrs := Attributes{}
	addIdentityAttrs(attrs, identityResult(c2pa.AttributionAsset,
		c2pa.Identity{SigType: "cawg.x509.cose", Valid: true, Roles: []string{"cawg.editor", "cawg.creator"}},
		c2pa.Identity{SigType: "cawg.identity_claims_aggregation", Valid: true, Roles: []string{"cawg.creator"}},
	))

	if got := attrs["c2pa_identity_count"]; got != int64(2) {
		t.Errorf("c2pa_identity_count = %v, want 2", got)
	}
	if got := attrs["c2pa_identity_valid"]; got != true {
		t.Errorf("c2pa_identity_valid = %v, want true", got)
	}
	// Nothing anchored either actor, so nobody is proven — the normal state,
	// since no identity trust list exists to configure.
	if got := attrs["c2pa_identity_trusted"]; got != false {
		t.Errorf("c2pa_identity_trusted = %v, want false", got)
	}
	// Sorted and deduplicated across every identity, the house shape for a list.
	if got, _ := attrs["c2pa_identity_roles"].([]string); !equalStrings(got, []string{"cawg.creator", "cawg.editor"}) {
		t.Errorf("c2pa_identity_roles = %v", got)
	}
	if got, _ := attrs["c2pa_identity_sig_types"].([]string); !equalStrings(got, []string{"cawg.identity_claims_aggregation", "cawg.x509.cose"}) {
		t.Errorf("c2pa_identity_sig_types = %v", got)
	}
}

// TestAddIdentityAttrs_TrustedIsAnyOf: one proven actor is enough for a file to
// answer the question "did anyone provably vouch for this".
func TestAddIdentityAttrs_TrustedIsAnyOf(t *testing.T) {
	attrs := Attributes{}
	addIdentityAttrs(attrs, identityResult(c2pa.AttributionAsset,
		c2pa.Identity{SigType: "cawg.x509.cose", Valid: true},
		c2pa.Identity{SigType: "cawg.x509.cose", Valid: true, Trusted: true},
	))
	if attrs["c2pa_identity_trusted"] != true || attrs["c2pa_identity_valid"] != true {
		t.Errorf("attrs = %v", attrs)
	}
}

// TestAddIdentityAttrs_NotThisFile is the attribution gate, the same rule
// c2pa_signed_by and c2pa_verified_signer follow: an identity inside a manifest
// the file merely CARRIES vouches for that carried resource. Indexing it would
// make a query match a document nobody vouched for.
func TestAddIdentityAttrs_NotThisFile(t *testing.T) {
	for _, attribution := range []c2pa.Attribution{c2pa.AttributionEmbedded, c2pa.AttributionUnknown} {
		attrs := Attributes{}
		addIdentityAttrs(attrs, identityResult(attribution,
			c2pa.Identity{SigType: "cawg.x509.cose", Valid: true, Roles: []string{"cawg.creator"}}))
		if len(attrs) != 0 {
			t.Errorf("attribution %q indexed %v", attribution, attrs)
		}
	}
}

// TestAddIdentityAttrs_None: a manifest nobody vouched for gets no attributes
// at all, so the zero defaults answer instead of a misleading explicit false.
func TestAddIdentityAttrs_None(t *testing.T) {
	attrs := Attributes{}
	addIdentityAttrs(attrs, identityResult(c2pa.AttributionAsset))
	if len(attrs) != 0 {
		t.Errorf("no identities but got %v", attrs)
	}
}

// TestAddIdentityAttrs_IndexesNoNames is the decision this feature turns on.
// An actor's name is unproven until an anchor vouches for it, and CAWG says an
// identity assertion conveys neither attribution nor ownership — so no
// attribute may carry a personal name, a username or an aggregator's DID.
func TestAddIdentityAttrs_IndexesNoNames(t *testing.T) {
	attrs := Attributes{}
	addIdentityAttrs(attrs, identityResult(c2pa.AttributionAsset, c2pa.Identity{
		SigType: "cawg.identity_claims_aggregation",
		Valid:   true,
		Issuer:  "did:jwk:eyJrdHkiOiJPS1AifQ",
		VerifiedIdentities: []c2pa.VerifiedIdentity{{
			Type: "cawg.social_media", Name: "Alice Example", Username: "alice",
			Provider: c2pa.IdentityProvider{Name: "Social Example"},
		}},
	}))
	for key, val := range attrs {
		for _, secret := range []string{"Alice Example", "alice", "did:jwk", "Social Example"} {
			if strings.Contains(fmt.Sprint(val), secret) {
				t.Errorf("attribute %s leaks %q: %v", key, secret, val)
			}
		}
	}
}

// TestValidateC2PA_NoIdentityAttributesWithoutOne: the real fixture carries no
// CAWG identity, so none of the attributes should appear for it.
func TestValidateC2PA_NoIdentityAttributesWithoutOne(t *testing.T) {
	attrs, ok := ValidateC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/jpeg")
	if !ok {
		t.Fatal("ValidateC2PA: ok=false")
	}
	for _, key := range []string{"c2pa_identity_count", "c2pa_identity_valid", "c2pa_identity_trusted", "c2pa_identity_roles", "c2pa_identity_sig_types"} {
		if _, present := attrs[key]; present {
			t.Errorf("%s set for a file with no identity assertion", key)
		}
	}
}

// equalStrings reports whether two string slices match element for element.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
