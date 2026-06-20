package playground

import (
	"slices"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

func res(path, ct string, size int64, md bool) search.Result {
	return search.Result{Path: path, Attrs: &celexpr.FileAttributes{
		Path: path, Name: path, ContentType: ct, Size: size, IsMarkdown: md,
	}}
}

func TestFilter(t *testing.T) {
	rs := []search.Result{
		res("a.md", "markdown", 100, true),
		res("b.md", "markdown", 9000, true),
		res("c.go", "source/go", 500, false),
	}

	t.Run("empty matches all", func(t *testing.T) {
		got, err := filter(rs, "  ")
		if err != nil || !slices.Equal(got, []int{0, 1, 2}) {
			t.Fatalf("got %v err %v, want [0 1 2]", got, err)
		}
	})

	t.Run("predicate filters", func(t *testing.T) {
		got, err := filter(rs, "is_markdown && size > 1000")
		if err != nil {
			t.Fatalf("err %v", err)
		}
		if !slices.Equal(got, []int{1}) {
			t.Errorf("got %v, want [1] (only b.md)", got)
		}
	})

	t.Run("compile error surfaces", func(t *testing.T) {
		if _, err := filter(rs, "is_markdown &&"); err == nil {
			t.Error("expected a compile error for a malformed expression")
		}
	})

	t.Run("content_type predicate", func(t *testing.T) {
		got, _ := filter(rs, `content_type == "source/go"`)
		if !slices.Equal(got, []int{2}) {
			t.Errorf("got %v, want [2]", got)
		}
	})
}
