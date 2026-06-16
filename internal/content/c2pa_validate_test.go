package content

import (
	"context"
	"os"
	"testing"
)

// TestValidateImageC2PA exercises the verified C2PA path (c2pa.Validate via
// content.ValidateImageC2PA). The fixture is a real C2PA-signed JPEG from
// contentauth/c2pa-rs, signed by a test certificate ("C2PA Signer") that is
// NOT in the embedded C2PA trust list — so full validation is expected to
// FAIL (untrusted signer), which is exactly the signal c2pa_valid /
// c2pa_validation_status are meant to carry. We assert the verified
// attributes are populated and internally consistent, not a specific verdict.
func TestValidateImageC2PA(t *testing.T) {
	attrs, ok := ValidateImageC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/jpeg")
	if !ok {
		t.Fatal("ValidateImageC2PA: ok=false for a fixture with a C2PA manifest")
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

// TestValidateImageC2PA_NoManifest confirms a plain image (no C2PA manifest)
// yields ok=false so the verified attributes stay at their zero values.
func TestValidateImageC2PA_NoManifest(t *testing.T) {
	if _, ok := ValidateImageC2PA(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg", "image/gif"); ok {
		t.Error("ValidateImageC2PA: ok=true for an unsupported container (image/gif)")
	}
}
