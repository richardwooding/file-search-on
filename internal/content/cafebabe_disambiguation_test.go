package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetect_CafebabeDisambiguation is the regression for issue #324:
// fat/universal Mach-O binaries and Java .class files both begin with
// 0xCAFEBABE. The magic pass used to return whichever registered first
// (bytecode/jvm), so every universal binary — the macOS norm —
// misdetected as Java. Detection now inspects the bytes after the magic
// (a fat header's nfat_arch + first CPU type vs a class's version).
func TestDetect_CafebabeDisambiguation(t *testing.T) {
	dir := t.TempDir()
	// Fat Mach-O: CAFEBABE + nfat_arch=2 + fat_arch[0].cputype=x86_64(0x01000007).
	machoFat := []byte{
		0xCA, 0xFE, 0xBA, 0xBE, // magic
		0x00, 0x00, 0x00, 0x02, // nfat_arch = 2
		0x01, 0x00, 0x00, 0x07, // cputype = x86_64
		0x00, 0x00, 0x00, 0x03, // cpusubtype (filler)
	}
	// Java 8 class: CAFEBABE + minor 0 + major 52 + constant_pool_count.
	javaClass := []byte{
		0xCA, 0xFE, 0xBA, 0xBE, // magic
		0x00, 0x00, // minor
		0x00, 0x34, // major = 52 (Java 8)
		0x00, 0x05, // constant_pool_count
		0x07, 0x00, // first cp entry (filler)
	}
	cases := []struct {
		file string
		body []byte
		want string
	}{
		{"fatbin", machoFat, "binary/mach-o"},     // extensionless → magic pass
		{"Klass", javaClass, "bytecode/jvm"},      // extensionless → magic pass
		{"App.class", javaClass, "bytecode/jvm"},  // extension pass
	}
	for _, tc := range cases {
		p := filepath.Join(dir, tc.file)
		if err := os.WriteFile(p, tc.body, 0o644); err != nil {
			t.Fatal(err)
		}
		ct := detectAt(p)
		if ct == nil {
			t.Errorf("%s: got nil, want %s", tc.file, tc.want)
			continue
		}
		if ct.Name() != tc.want {
			t.Errorf("%s: got %s, want %s", tc.file, ct.Name(), tc.want)
		}
	}
}
