package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// TestInstallPackage_FixtureAttributes asserts each hand-crafted
// install-package fixture surfaces the expected format + per-format
// extras. Byte layouts match what the Python builder wrote.
func TestInstallPackage_FixtureAttributes(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixDir := filepath.Join(wd, "testdata", "fixtures")
	fsys := os.DirFS(fixDir)

	cases := []struct {
		fixture string
		wantFmt string
		extras  map[string]any
	}{
		{
			fixture: "sample.pkg", wantFmt: "xar",
			extras: map[string]any{
				"package_kind": "macos-installer",
			},
		},
		{
			fixture: "sample.deb", wantFmt: "deb",
			extras: map[string]any{
				"package_kind": "binary",
			},
		},
		{
			fixture: "sample.rpm", wantFmt: "rpm",
			extras: map[string]any{
				// Lead name "file-search-on-0.32.0-1.el9" splits at the
				// last two dashes: name + version + release.
				"package_name":    "file-search-on",
				"package_version": "0.32.0",
				"package_release": "1.el9",
				"package_arch":    "i386",
				"package_kind":    "binary",
			},
		},
		{
			fixture: "sample.appimage", wantFmt: "appimage",
			extras: map[string]any{
				"package_kind":     "linux-portable",
				"appimage_version": int64(2),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ct := DefaultRegistry().Detect(fsys, tc.fixture)
			if ct == nil {
				t.Fatalf("Detect returned nil for %s", tc.fixture)
			}
			attrs, err := ct.Attributes(context.Background(), fsys, tc.fixture)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["package_format"]; got != tc.wantFmt {
				t.Errorf("package_format = %v, want %q", got, tc.wantFmt)
			}
			for k, want := range tc.extras {
				if got := attrs[k]; got != want {
					t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
				}
			}
		})
	}
}

// TestInstallPackage_Corrupted feeds each install-package type random
// bytes claimed as its extension. Contract: no panic, no error escape,
// minimal attrs surfaced. The pkg / deb / rpm parsers all check magic
// at offset 0 and bail to empty attrs on mismatch; appimage checks the
// offset-8 marker and bails similarly.
func TestInstallPackage_Corrupted(t *testing.T) {
	junk := []byte("not an install package, just some random bytes")
	cases := []string{"x.pkg", "x.deb", "x.rpm", "x.appimage"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{name: &fstest.MapFile{Data: junk}}
			ct := DefaultRegistry().Detect(fsys, name)
			if ct == nil {
				t.Fatalf("Detect returned nil for %s", name)
			}
			attrs, err := ct.Attributes(context.Background(), fsys, name)
			if err != nil {
				t.Errorf("err = %v, want nil for corrupted input", err)
			}
			if fmt, ok := attrs["package_format"]; ok && fmt != "" {
				t.Errorf("package_format = %v, want absent/empty for corrupted input", fmt)
			}
		})
	}
}

// TestSplitRPMName covers the RPM Lead's "name-version-release"
// decomposition. The format is convention, not contract, so we test
// the standard parse (split on last two dashes from the right) and
// degenerate cases.
func TestSplitRPMName(t *testing.T) {
	cases := []struct {
		in                       string
		wantName, wantVer, wantRel string
	}{
		{"file-search-on-0.32.0-1.el9", "file-search-on", "0.32.0", "1.el9"},
		{"openssh-clients-8.7p1-38.el9_3.4", "openssh-clients", "8.7p1", "38.el9_3.4"},
		{"gcc-c++-12.2.1-7", "gcc-c++", "12.2.1", "7"},
		{"foo-1.0", "foo", "", "1.0"},     // Only one dash — release only
		{"single", "single", "", ""},      // No dashes — name only
		{"", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			name, ver, rel := splitRPMName(c.in)
			if name != c.wantName || ver != c.wantVer || rel != c.wantRel {
				t.Errorf("splitRPMName(%q) = (%q, %q, %q), want (%q, %q, %q)",
					c.in, name, ver, rel, c.wantName, c.wantVer, c.wantRel)
			}
		})
	}
}
