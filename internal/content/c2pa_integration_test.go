package content

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestExtractC2PA_Integration exercises the full imageType.Attributes path:
// the C2PA parsing itself lives in github.com/richardwooding/c2pa (and is
// tested there); this asserts file-search-on wires its Info fields onto the
// is_c2pa / c2pa_* attributes correctly. Fixture: a real C2PA-signed JPEG
// from contentauth/c2pa-rs.
func TestExtractC2PA_Integration(t *testing.T) {
	it := &imageType{name: "image/jpeg"}
	attrs, err := it.Attributes(context.Background(), os.DirFS("testdata/fixtures"), "c2pa_signed.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if attrs["is_c2pa"] != true {
		t.Errorf("is_c2pa not set: %v", attrs["is_c2pa"])
	}
	if g, _ := attrs["c2pa_claim_generator"].(string); g == "" {
		t.Errorf("c2pa_claim_generator not set")
	}
	if by, _ := attrs["c2pa_signed_by"].(string); by != "C2PA Signer" {
		t.Errorf("c2pa_signed_by=%q want %q", by, "C2PA Signer")
	}
	if at, _ := attrs["c2pa_signed_at"].(time.Time); at.IsZero() {
		t.Errorf("c2pa_signed_at not set")
	}
}
