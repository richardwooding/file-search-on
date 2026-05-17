package content

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// TestBytecode_FixtureAttributes asserts each hand-crafted fixture
// surfaces the expected bytecode_format + runtime_version plus the
// per-format extras documented in the parser comments.
func TestBytecode_FixtureAttributes(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixDir := filepath.Join(wd, "testdata", "fixtures")
	fsys := os.DirFS(fixDir)

	cases := []struct {
		fixture string
		wantFmt string
		wantVer string
		extras  map[string]any
	}{
		{
			fixture: "sample.class", wantFmt: "jvm", wantVer: "Java 17",
			extras: map[string]any{
				"class_name":   "Hello",
				"super_class":  "java/lang/Object",
				"method_count": int64(0),
				"field_count":  int64(0),
			},
		},
		{
			fixture: "sample.pyc", wantFmt: "python", wantVer: "Python 3.11",
			extras: map[string]any{
				"python_version": "3.11",
			},
		},
		{
			fixture: "sample.wasm", wantFmt: "wasm", wantVer: "WebAssembly 1.0",
			extras: map[string]any{
				"wasm_version":  int64(1),
				"section_count": int64(3),
				"import_count":  int64(1),
				"export_count":  int64(2),
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
			if got := attrs["bytecode_format"]; got != tc.wantFmt {
				t.Errorf("bytecode_format = %v, want %q", got, tc.wantFmt)
			}
			if got := attrs["runtime_version"]; got != tc.wantVer {
				t.Errorf("runtime_version = %v, want %q", got, tc.wantVer)
			}
			for k, want := range tc.extras {
				got := attrs[k]
				if got != want {
					t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, want, want)
				}
			}
		})
	}
}

// TestBytecode_ClassAccessFlags verifies the access-flags decoder
// against a known bit-pattern combination.
func TestBytecode_ClassAccessFlags(t *testing.T) {
	cases := []struct {
		flags uint16
		want  []string
	}{
		{0x0021, []string{"public", "super"}},
		{0x0411, []string{"public", "final", "abstract"}},
		{0x0200, []string{"interface"}},
		{0x4001, []string{"public", "enum"}},
		{0x0000, []string{}},
	}
	for _, c := range cases {
		got := decodeClassAccessFlags(c.flags)
		if !equalStringSlices(got, c.want) {
			t.Errorf("decodeClassAccessFlags(0x%04x) = %v, want %v", c.flags, got, c.want)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestBytecode_JavaVersion covers the .class major-version → release
// table.
func TestBytecode_JavaVersion(t *testing.T) {
	cases := []struct {
		major uint16
		want  string
	}{
		{52, "Java 8"},
		{61, "Java 17"},
		{65, "Java 21"},
		{0, ""},
		{99, "class format 99"},
	}
	for _, c := range cases {
		if got := javaVersion(c.major); got != c.want {
			t.Errorf("javaVersion(%d) = %q, want %q", c.major, got, c.want)
		}
	}
}

// TestBytecode_PYCSourceMtime verifies the source_mtime parse for a
// PEP 552 timestamp-based .pyc.
func TestBytecode_PYCSourceMtime(t *testing.T) {
	wd, _ := os.Getwd()
	fsys := os.DirFS(filepath.Join(wd, "testdata", "fixtures"))
	attrs, err := readPYCInfo(fsys, "sample.pyc")
	if err != nil {
		t.Fatalf("readPYCInfo: %v", err)
	}
	mtime, ok := attrs["source_mtime"].(time.Time)
	if !ok {
		t.Fatalf("source_mtime missing or wrong type: %v (%T)", attrs["source_mtime"], attrs["source_mtime"])
	}
	want := time.Unix(1700000000, 0).UTC()
	if !mtime.Equal(want) {
		t.Errorf("source_mtime = %v, want %v", mtime, want)
	}
}

// TestBytecode_Corrupted feeds each type random bytes claimed as the
// right extension. Contract: empty attrs (or partial with empty
// format), no error, no panic — matches the "broken file doesn't
// fail the walk" pattern.
func TestBytecode_Corrupted(t *testing.T) {
	cases := []string{"x.class", "x.pyc", "x.wasm"}
	junk := []byte("nope, not a real bytecode artefact")
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
			if got, ok := attrs["bytecode_format"]; ok && got != "" {
				// .pyc is extension-only — surfaces format on
				// corrupted input too, but with no runtime_version.
				// That's acceptable; only the absent runtime_version
				// signals the corruption.
				if name == "x.pyc" {
					return
				}
				t.Errorf("bytecode_format = %v, want absent for corrupted %s", got, name)
			}
		})
	}
}
