package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestYAMLAttributes(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		file     string
		body     string
		wantKind string
		wantDocs int64
	}{
		// Single-document, root mapping — the K8s manifest shape.
		{"manifest.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: foo\n", "object", 1},
		// Root sequence — top-level list.
		{"list.yaml", "- alpha\n- beta\n- gamma\n", "array", 1},
		// Plain scalar at root (legal YAML, unusual but possible).
		{"scalar.yaml", "just-a-string\n", "scalar", 1},
		// Multi-document file — three `---`-separated docs.
		{"multi.yaml", "---\nfoo: 1\n---\nfoo: 2\n---\nfoo: 3\n", "object", 3},
		// .yml extension also detected.
		{"compose.yml", "services:\n  web:\n    image: nginx\n", "object", 1},
		// Empty file decodes to nothing — kind is "unknown".
		{"empty.yaml", "", "unknown", 0},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil || ct.Name() != "yaml" {
				t.Fatalf("Detect: got %v, want yaml", ct)
			}
			attrs, err := attributesAt(t.Context(), ct, path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["yaml_kind"]; got != tc.wantKind {
				t.Errorf("yaml_kind = %v, want %q", got, tc.wantKind)
			}
			if got := attrs["yaml_document_count"]; got != tc.wantDocs {
				t.Errorf("yaml_document_count = %v, want %d", got, tc.wantDocs)
			}
		})
	}
}
