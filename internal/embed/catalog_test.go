package embed

import (
	"sort"
	"testing"
)

func TestCatalog_Shape(t *testing.T) {
	if len(Catalog) < 3 {
		t.Fatalf("catalog has %d entries, want at least 3", len(Catalog))
	}
	for _, e := range Catalog {
		if e.Name == "" {
			t.Errorf("entry has empty Name: %+v", e)
		}
		if e.Description == "" {
			t.Errorf("entry %q has empty Description", e.Name)
		}
		if e.Size == "" {
			t.Errorf("entry %q has empty Size", e.Name)
		}
		if e.Dimensions <= 0 {
			t.Errorf("entry %q has non-positive Dimensions: %d", e.Name, e.Dimensions)
		}
	}
}

func TestCatalog_AlphabeticalOrder(t *testing.T) {
	names := make([]string, len(Catalog))
	for i, e := range Catalog {
		names[i] = e.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Catalog must be alphabetically sorted by Name; got %v", names)
	}
}

func TestCatalogLookup(t *testing.T) {
	if got := CatalogLookup("nomic-embed-text"); got == nil {
		t.Errorf("CatalogLookup(\"nomic-embed-text\") = nil, want non-nil")
	} else if got.Dimensions != 768 {
		t.Errorf("nomic-embed-text dims = %d, want 768", got.Dimensions)
	}
	if got := CatalogLookup("not-a-real-model"); got != nil {
		t.Errorf("CatalogLookup of unknown returned %+v, want nil", got)
	}
}

func TestBareName(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"nomic-embed-text", "nomic-embed-text"},
		{"nomic-embed-text:latest", "nomic-embed-text"},
		{"foo:v1.2.3", "foo"},
		{"", ""},
	} {
		if got := BareName(c.in); got != c.want {
			t.Errorf("BareName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
