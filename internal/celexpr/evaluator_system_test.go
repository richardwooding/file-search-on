package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestBuildAttributesWith_SystemMetadata exercises the four-place
// invariant for every new OS-metadata type. For each canonical file:
//   - asserts the per-type IsX bool fires on FileAttributes,
//   - asserts the OS-specific family bool fires (IsMacOSMetadata /
//     IsWindowsMetadata / IsLinuxMetadata),
//   - asserts the cross-OS IsSystemMetadata fires,
//   - confirms the OTHER OS-family bools stay false (no cross-OS
//     bleed via the refactored prefix-`if` chain),
//   - compiles + evaluates the per-type, OS-family, and cross-OS CEL
//     predicates and asserts all three return true.
func TestBuildAttributesWith_SystemMetadata(t *testing.T) {
	cases := []struct {
		filename     string
		wantType     string
		perTypeCEL   string
		osFamily     string
		// expected booleans on FileAttributes
		wantMacOS, wantWindows, wantLinux bool
	}{
		{".DS_Store", "system/macos-ds-store", "is_ds_store", "is_macos_metadata", true, false, false},
		{".localized", "system/macos-localized", "is_localized", "is_macos_metadata", true, false, false},
		{"Thumbs.db", "system/windows-thumbs-db", "is_thumbs_db", "is_windows_metadata", false, true, false},
		{"Desktop.ini", "system/windows-desktop-ini", "is_desktop_ini", "is_windows_metadata", false, true, false},
		{".directory", "system/linux-directory", "is_kde_directory", "is_linux_metadata", false, false, true},
	}

	ctx := t.Context()
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			base := filepath.Base(abs)
			parent := filepath.Dir(abs)

			attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
			if err != nil {
				t.Fatalf("BuildAttributesWith: %v", err)
			}
			if attrs.ContentType != tc.wantType {
				t.Errorf("ContentType=%q want %q", attrs.ContentType, tc.wantType)
			}

			// OS-specific family flags — exactly one fires.
			if attrs.IsMacOSMetadata != tc.wantMacOS {
				t.Errorf("IsMacOSMetadata=%v want %v", attrs.IsMacOSMetadata, tc.wantMacOS)
			}
			if attrs.IsWindowsMetadata != tc.wantWindows {
				t.Errorf("IsWindowsMetadata=%v want %v", attrs.IsWindowsMetadata, tc.wantWindows)
			}
			if attrs.IsLinuxMetadata != tc.wantLinux {
				t.Errorf("IsLinuxMetadata=%v want %v", attrs.IsLinuxMetadata, tc.wantLinux)
			}
			// Cross-OS family always fires.
			if !attrs.IsSystemMetadata {
				t.Errorf("IsSystemMetadata=false want true")
			}

			// CEL: per-type, OS-family, and cross-OS predicates all
			// evaluate true.
			for _, expr := range []string{tc.perTypeCEL, tc.osFamily, "is_system_metadata"} {
				ev, err := celexpr.New(expr)
				if err != nil {
					t.Fatalf("celexpr.New(%q): %v", expr, err)
				}
				match, err := ev.Evaluate(attrs)
				if err != nil {
					t.Fatalf("Evaluate(%q): %v", expr, err)
				}
				if !match {
					t.Errorf("CEL %q returned false; want true", expr)
				}
			}
		})
	}
}

// TestBuildAttributesWith_SystemMetadataNegative verifies a regular
// non-system file leaves every new flag at its zero default.
func TestBuildAttributesWith_SystemMetadataNegative(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}

	// Per-type flags off.
	if attrs.IsDSStore || attrs.IsLocalized || attrs.IsThumbsDB || attrs.IsDesktopIni || attrs.IsKDEDirectory {
		t.Errorf("per-type flag fired on regular.txt: %+v", attrs)
	}
	// Family flags off.
	if attrs.IsMacOSMetadata || attrs.IsWindowsMetadata || attrs.IsLinuxMetadata || attrs.IsSystemMetadata {
		t.Errorf("family flag fired on regular.txt: %+v", attrs)
	}

	// CEL: is_system_metadata false.
	ev, err := celexpr.New("is_system_metadata")
	if err != nil {
		t.Fatalf("celexpr.New: %v", err)
	}
	match, err := ev.Evaluate(attrs)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if match {
		t.Errorf("is_system_metadata=true on regular.txt; want false")
	}
}
