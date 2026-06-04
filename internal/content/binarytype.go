package content

import (
	"context"
	"errors"
	"io/fs"
)

func init() {
	Register(&binaryType{
		name:  "binary/elf",
		exts:  []string{".elf", ".so", ".o"},
		magic: [][]byte{{0x7F, 0x45, 0x4C, 0x46}},
	})
	Register(&binaryType{
		name: "binary/mach-o",
		exts: []string{".dylib", ".bundle"},
		magic: [][]byte{
			{0xFE, 0xED, 0xFA, 0xCE}, // 32-bit big-endian
			{0xFE, 0xED, 0xFA, 0xCF}, // 64-bit big-endian
			{0xCE, 0xFA, 0xED, 0xFE}, // 32-bit little-endian
			{0xCF, 0xFA, 0xED, 0xFE}, // 64-bit little-endian
		},
	})
	Register(&binaryType{
		name:  "binary/pe",
		exts:  []string{".exe", ".dll", ".sys"},
		magic: [][]byte{{0x4D, 0x5A}}, // "MZ"
	})
}

type binaryType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (b *binaryType) Name() string         { return b.name }
func (b *binaryType) Extensions() []string { return b.exts }
func (b *binaryType) MagicBytes() [][]byte { return b.magic }

// MatchMagic claims fat/universal Mach-O binaries (magic 0xCAFEBABE),
// which share that magic with Java .class and would otherwise detect as
// bytecode/jvm — the thin Mach-O magics are registered but the fat one
// isn't expressible as a fixed byte prefix (issue #324). Other binary
// types (ELF, PE) fall back to the standard prefix match.
func (b *binaryType) MatchMagic(head []byte) bool {
	// Fat Mach-O (0xCAFEBABE + nfat_arch + a known CPU type) is a
	// STRUCTURAL check, not a fixed-offset byte pattern, so it can't use
	// matchOffsetSigs — see isMachoFatHeader.
	if b.name == "binary/mach-o" && isMachoFatHeader(head) {
		return true
	}
	return matchAnyPrefix(head, b.magic)
}

// Attributes dispatches to a per-format binary parser. All parsers
// return the same surface — architectures, bitness, binary_format,
// binary_type, is_dynamically_linked, is_stripped, entry_point — so
// callers can filter without knowing the format.
func (b *binaryType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch b.name {
	case "binary/elf":
		return readELFInfo(fsys, path)
	case "binary/mach-o":
		return readMachoInfo(ctx, fsys, path)
	case "binary/pe":
		return readPEInfo(fsys, path)
	}
	return nil, errors.New("unsupported binary type")
}

// binaryAttrs packs the common surface into a content.Attributes map.
// Keys match the seven type-specific CEL attributes registered for the
// family. Callers populate every field — zero-value strings/ints/bools
// flow through cleanly when something isn't applicable (e.g.
// entry_point on a shared library).
func binaryAttrs(architectures []string, bitness int64, format, binaryType string,
	isDynamic, isStripped bool, entryPoint int64) Attributes {
	if architectures == nil {
		architectures = []string{}
	}
	return Attributes{
		"architectures":         architectures,
		"bitness":               bitness,
		"binary_format":         format,
		"binary_type":           binaryType,
		"is_dynamically_linked": isDynamic,
		"is_stripped":           isStripped,
		"entry_point":           entryPoint,
	}
}
