package content_test

import (
	"testing"
	"testing/fstest"

	"github.com/richardwooding/file-search-on/internal/content"
)

// minimalELF64 is the smallest valid ELF64 file that debug/elf will
// parse without error: a 64-byte header pointing at zero program
// headers and zero section headers. e_type = ET_DYN (3), e_machine =
// EM_AARCH64 (0xB7), little-endian. No symbol table → is_stripped is
// reported as true (Symbols() returns ErrNoSymbols).
//
// Hand-written so the test exercises the synthetic-bytes path without
// pulling in a real binary fixture.
func minimalELF64() []byte {
	b := make([]byte, 64)
	// e_ident
	b[0], b[1], b[2], b[3] = 0x7F, 'E', 'L', 'F' // ELF magic
	b[4] = 2                                     // EI_CLASS = ELFCLASS64
	b[5] = 1                                     // EI_DATA = ELFDATA2LSB
	b[6] = 1                                     // EI_VERSION = 1
	// b[7..15] OS/ABI + pad — all zero.
	// e_type = ET_DYN (3), little-endian uint16 at offset 16.
	b[16] = 0x03
	b[17] = 0x00
	// e_machine = EM_AARCH64 (0xB7), little-endian uint16 at offset 18.
	b[18] = 0xB7
	b[19] = 0x00
	// e_version = 1, uint32 at offset 20.
	b[20] = 0x01
	// e_entry / e_phoff / e_shoff = 0 → already-zero slots at 24..47.
	// e_ehsize = 64, uint16 at offset 52.
	b[52] = 64
	// e_phentsize, e_phnum, e_shentsize, e_shnum, e_shstrndx = 0.
	return b
}

// TestBinaryELF_SyntheticDetect verifies the registry detects a
// hand-crafted minimal ELF64 by magic bytes (no extension match) and
// that the parser surfaces architecture, bitness, format, and type.
func TestBinaryELF_SyntheticDetect(t *testing.T) {
	fsys := fstest.MapFS{"a.bin": {Data: minimalELF64()}}
	ct := content.DefaultRegistry().Detect(fsys, "a.bin")
	if ct == nil {
		t.Fatalf("Detect returned nil for synthetic ELF")
	}
	if ct.Name() != "binary/elf" {
		t.Fatalf("Detect.Name() = %q; want binary/elf", ct.Name())
	}
	a, err := ct.Attributes(t.Context(), fsys, "a.bin")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if archs, _ := a["architectures"].([]string); len(archs) != 1 || archs[0] != "arm64" {
		t.Errorf("architectures = %v; want [arm64]", archs)
	}
	if b, _ := a["bitness"].(int64); b != 64 {
		t.Errorf("bitness = %v; want 64", a["bitness"])
	}
	if f, _ := a["binary_format"].(string); f != "elf" {
		t.Errorf("binary_format = %q; want elf", a["binary_format"])
	}
	// ET_DYN with no PT_INTERP segment ⇒ classified as shared_library.
	if bt, _ := a["binary_type"].(string); bt != "shared_library" {
		t.Errorf("binary_type = %q; want shared_library", a["binary_type"])
	}
	// No PT_INTERP / PT_DYNAMIC segments declared → not dynamic.
	if dyn, _ := a["is_dynamically_linked"].(bool); dyn {
		t.Errorf("is_dynamically_linked = true; want false")
	}
	// No symbol table → stripped.
	if s, _ := a["is_stripped"].(bool); !s {
		t.Errorf("is_stripped = false; want true")
	}
}

// TestBinaryDetect_RegistryByExtension verifies extension-based
// detection: a `.exe` file with no PE magic should still resolve to
// binary/pe via the extension table (the parser will then fail to read
// it as a real PE and degrade to empty attrs — that's by design).
func TestBinaryDetect_RegistryByExtension(t *testing.T) {
	cases := []struct {
		name string
		ext  string
		want string
	}{
		{"PE by .exe extension", "a.exe", "binary/pe"},
		{"PE by .dll extension", "a.dll", "binary/pe"},
		{"Mach-O by .dylib extension", "a.dylib", "binary/mach-o"},
		{"ELF by .elf extension", "a.elf", "binary/elf"},
		{"ELF by .so extension", "a.so", "binary/elf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{c.ext: {Data: []byte{0x00, 0x00, 0x00, 0x00}}}
			ct := content.DefaultRegistry().Detect(fsys, c.ext)
			if ct == nil {
				t.Fatalf("Detect = nil for %q", c.ext)
			}
			if ct.Name() != c.want {
				t.Errorf("Detect.Name() = %q; want %q", ct.Name(), c.want)
			}
		})
	}
}

// TestBinaryDetect_MachoMagicVariants verifies all four thin Mach-O
// magic byte sequences route to binary/mach-o via magic-byte sniffing
// (no extension on the filenames). The fat magic 0xCAFEBABE is
// deliberately NOT registered — see binarytype.go for the Java .class
// collision rationale.
func TestBinaryDetect_MachoMagicVariants(t *testing.T) {
	cases := []struct {
		name  string
		magic []byte
	}{
		{"32-bit big-endian (FEEDFACE)", []byte{0xFE, 0xED, 0xFA, 0xCE}},
		{"64-bit big-endian (FEEDFACF)", []byte{0xFE, 0xED, 0xFA, 0xCF}},
		{"32-bit little-endian (CEFAEDFE)", []byte{0xCE, 0xFA, 0xED, 0xFE}},
		{"64-bit little-endian (CFFAEDFE)", []byte{0xCF, 0xFA, 0xED, 0xFE}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{"a.bin": {Data: c.magic}}
			ct := content.DefaultRegistry().Detect(fsys, "a.bin")
			if ct == nil {
				t.Fatalf("Detect returned nil")
			}
			if ct.Name() != "binary/mach-o" {
				t.Errorf("Detect.Name() = %q; want binary/mach-o", ct.Name())
			}
		})
	}
}

// TestBinaryDetect_ELFMagic verifies the canonical ELF magic at offset
// 0 routes to binary/elf without an extension hint.
func TestBinaryDetect_ELFMagic(t *testing.T) {
	fsys := fstest.MapFS{"a.bin": {Data: []byte{0x7F, 0x45, 0x4C, 0x46}}}
	ct := content.DefaultRegistry().Detect(fsys, "a.bin")
	if ct == nil {
		t.Fatalf("Detect returned nil")
	}
	if ct.Name() != "binary/elf" {
		t.Errorf("Detect.Name() = %q; want binary/elf", ct.Name())
	}
}
