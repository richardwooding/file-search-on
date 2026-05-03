package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func htmlAttrs(t *testing.T, path string) content.Attributes {
	t.Helper()
	for _, ct := range content.DefaultRegistry().Types() {
		if ct.Name() == "html" {
			attrs, err := ct.Attributes(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			return attrs
		}
	}
	t.Fatal("html content type not registered")
	return nil
}

func TestHTMLLanguage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"simple-en.html",
			`<!DOCTYPE html><html lang="en"><head><title>x</title></head></html>`,
			"en",
		},
		{
			"bcp47-en-GB.html",
			`<!DOCTYPE html><html lang="en-GB"><head><title>x</title></head></html>`,
			"en-GB",
		},
		{
			"single-quoted.html",
			`<!DOCTYPE html><html lang='fr'><head><title>x</title></head></html>`,
			"fr",
		},
		{
			"unquoted.html",
			`<!DOCTYPE html><html lang=de><head><title>x</title></head></html>`,
			"de",
		},
		{
			"with-other-attrs.html",
			`<!DOCTYPE html><html dir="ltr" lang="ja" class="root"><head><title>x</title></head></html>`,
			"ja",
		},
		{
			"uppercase-tag.html",
			`<!DOCTYPE html><HTML LANG="es"><head><title>x</title></head></HTML>`,
			"es",
		},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			attrs := htmlAttrs(t, path)
			if got := attrs["language"]; got != tc.want {
				t.Errorf("language = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestHTMLLanguageAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-lang.html")
	body := `<!DOCTYPE html><html><head><title>x</title></head><body>y</body></html>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	attrs := htmlAttrs(t, path)
	if v, ok := attrs["language"]; ok {
		t.Errorf("language present when absent in HTML: %v", v)
	}
}
