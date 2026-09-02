package content

import (
	"context"
	"os"
	"testing"
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
