package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// TestDiskImage_FixtureAttributes asserts each hand-crafted fixture
// surfaces the expected disk_image_format + virtual_size and any
// per-format extras. The fixture-byte layouts (and the values we
// expect to read back) are documented in the corresponding
// disk_*.go parser comments.
func TestDiskImage_FixtureAttributes(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixDir := filepath.Join(wd, "testdata", "fixtures")
	fsys := os.DirFS(fixDir)

	cases := []struct {
		fixture string
		wantFmt string
		wantVS  int64
		// Per-format extras to check; only the listed keys are
		// asserted, others may or may not be present.
		extras map[string]any
	}{
		{
			fixture: "sample.dmg", wantFmt: "udif",
			// 2048 sectors × 512 = 1 MiB, per the Python builder.
			wantVS: 1024 * 1024,
		},
		{
			fixture: "sample.iso", wantFmt: "iso9660",
			// 1024 logical blocks × 2048 = 2 MiB.
			wantVS: 2 * 1024 * 1024,
			extras: map[string]any{
				"volume_label": "FILESEARCH",
				// disk_image_created_at is checked for non-zero only —
				// exact tz reconstruction is brittle and the fixture
				// pins it at 2026-05-14T12:00:00Z.
			},
		},
		{
			fixture: "sample.vhd", wantFmt: "vhd-fixed",
			wantVS: 4 * 1024 * 1024,
			extras: map[string]any{
				"disk_type": "fixed",
			},
		},
		{
			fixture: "sample.vhdx", wantFmt: "vhdx",
			// Fixture is magic-only — region tables absent, so the
			// best-effort VirtualDiskSize walk returns 0.
			wantVS: 0,
		},
		{
			fixture: "sample.vmdk", wantFmt: "vmdk-sparse",
			// 8192 sectors × 512 = 4 MiB.
			wantVS: 4 * 1024 * 1024,
			extras: map[string]any{
				"disk_type": "sparse",
			},
		},
		{
			fixture: "sample.qcow2", wantFmt: "qcow2",
			wantVS: 16 * 1024 * 1024,
			extras: map[string]any{
				"cluster_bits": int64(16),
				"is_encrypted": true,
			},
		},
		{
			fixture: "sample.wim", wantFmt: "wim",
			// WIM doesn't surface a meaningful virtual_size.
			wantVS: 0,
			extras: map[string]any{
				"image_count": int64(3),
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
			if got := attrs["disk_image_format"]; got != tc.wantFmt {
				t.Errorf("disk_image_format = %v, want %q", got, tc.wantFmt)
			}
			if got, _ := attrs["virtual_size"].(int64); got != tc.wantVS {
				t.Errorf("virtual_size = %v, want %d", attrs["virtual_size"], tc.wantVS)
			}
			for k, want := range tc.extras {
				got := attrs[k]
				if got != want {
					t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
				}
			}
			// ISO fixture also has a disk_image_created_at; just
			// assert it's populated and from 2026.
			if tc.fixture == "sample.iso" {
				ts, ok := attrs["disk_image_created_at"].(time.Time)
				if !ok || ts.IsZero() {
					t.Errorf("disk_image_created_at not populated for sample.iso: %v", attrs["disk_image_created_at"])
				} else if ts.Year() != 2026 {
					t.Errorf("disk_image_created_at year = %d, want 2026", ts.Year())
				}
			}
		})
	}
}

// TestDiskImage_Corrupted feeds each disk-image content type random
// bytes claimed as the right extension. Contract: empty attrs (or a
// partial attribute map with disk_image_format = "" if magic check
// fails), no error, no panic. Matches the "broken file doesn't fail
// the walk" pattern used by every other family.
func TestDiskImage_Corrupted(t *testing.T) {
	junk := []byte("this is not actually a disk image")
	cases := []string{
		"x.dmg", "x.iso", "x.vhd", "x.vhdx", "x.vmdk", "x.qcow2", "x.wim",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{name: &fstest.MapFile{Data: junk}}
			ct := DefaultRegistry().Detect(fsys, name)
			if ct == nil {
				// Detection by extension should still hit for .dmg
				// / .iso / .vhd (magic-less) and .vhdx / .vmdk /
				// .qcow2 / .wim (magic-then-extension fallback).
				t.Fatalf("Detect returned nil for %s", name)
			}
			attrs, err := ct.Attributes(context.Background(), fsys, name)
			if err != nil {
				t.Errorf("err = %v, want nil for corrupted input", err)
			}
			// For the magic-checking parsers the magic mismatch
			// returns an empty map; for the footer-checking ones the
			// 33-byte input is below the footer-size threshold and we
			// return an empty map. Either way, no disk_image_format
			// key should be set.
			if fmt, ok := attrs["disk_image_format"]; ok && fmt != "" {
				t.Errorf("disk_image_format = %v, want absent/empty for corrupted input", fmt)
			}
		})
	}
}
