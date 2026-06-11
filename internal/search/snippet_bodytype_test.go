package search

import "testing"

func TestIsTextBodyType(t *testing.T) {
	for _, name := range []string{"source/go", "source/rust", "markdown", "text", "json", "yaml"} {
		if !IsTextBodyType(name) {
			t.Errorf("IsTextBodyType(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"pdf", "office/docx", "epub", "email/rfc822", "image/jpeg", "binary/elf"} {
		if IsTextBodyType(name) {
			t.Errorf("IsTextBodyType(%q) = true, want false (extracted/binary body)", name)
		}
	}
}
