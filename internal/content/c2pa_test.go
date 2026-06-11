package content

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// TestExtractC2PA_SignedJPEG parses the JUMBF manifest from a real
// C2PA-signed JPEG (contentauth/c2pa-rs test fixture).
func TestExtractC2PA_SignedJPEG(t *testing.T) {
	f, err := os.Open("testdata/fixtures/c2pa_signed.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	c := extractC2PA("jpeg", f)
	if !c.Present {
		t.Fatal("expected a C2PA manifest")
	}
	if !bytes.Contains([]byte(c.ClaimGenerator), []byte("c2pa-rs")) {
		t.Errorf("claim_generator=%q want it to mention c2pa-rs", c.ClaimGenerator)
	}
	if c.Title != "CA.jpg" {
		t.Errorf("title=%q want CA.jpg", c.Title)
	}
	if c.AIGenerated {
		t.Errorf("CA.jpg is edited, not AI-generated; want AIGenerated=false")
	}
}

// TestExtractC2PA_NoManifest returns Present=false for content with no
// C2PA manifest.
func TestExtractC2PA_NoManifest(t *testing.T) {
	if c := extractC2PA("jpeg", bytes.NewReader([]byte("\xff\xd8\xff\xe0 not a real manifest"))); c.Present {
		t.Errorf("expected no manifest, got %+v", c)
	}
}

// TestExtractC2PA_Integration exercises the full imageType.Attributes path.
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
}

// TestC2PAActionsAreAI checks the AI-generated detection on synthetic
// c2pa.actions assertions (no public AI-positive fixture available).
func TestC2PAActionsAreAI(t *testing.T) {
	ai := mustCBOR(t, map[string]any{"actions": []any{
		map[string]any{"action": "c2pa.created",
			"digitalSourceType": "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"},
	}})
	aiParam := mustCBOR(t, map[string]any{"actions": []any{
		map[string]any{"action": "c2pa.created",
			"parameters": map[string]any{"digitalSourceType": "...compositeWithTrainedAlgorithmicMedia"}},
	}})
	notAI := mustCBOR(t, map[string]any{"actions": []any{
		map[string]any{"action": "c2pa.color_adjustments"},
		map[string]any{"action": "c2pa.opened"},
	}})

	for _, tc := range []struct {
		name string
		cbor []byte
		want bool
	}{
		{"top-level digitalSourceType", ai, true},
		{"parameters digitalSourceType", aiParam, true},
		{"edit-only actions", notAI, false},
	} {
		var m map[string]any
		if err := c2paDecMode.Unmarshal(tc.cbor, &m); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := c2paActionsAreAI(m); got != tc.want {
			t.Errorf("%s: c2paActionsAreAI=%v want %v", tc.name, got, tc.want)
		}
	}
}

func mustCBOR(t *testing.T, v any) []byte {
	t.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
