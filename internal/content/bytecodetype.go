package content

import (
	"context"
	"errors"
	"io/fs"
	"maps"
)

func init() {
	Register(&bytecodeType{
		name:  "bytecode/jvm",
		exts:  []string{".class"},
		magic: [][]byte{{0xCA, 0xFE, 0xBA, 0xBE}}, // Java .class magic
	})
	Register(&bytecodeType{
		name: "bytecode/python",
		exts: []string{".pyc", ".pyo"},
		// Python .pyc magic is version-dependent: every CPython
		// release picks a unique 2-byte magic + the literal trailer
		// "\r\n" (which catches accidental text-mode corruption).
		// The detector matches via extension only since the magic
		// table would otherwise need a per-Python-version entry.
		magic: nil,
	})
	Register(&bytecodeType{
		name:  "bytecode/wasm",
		exts:  []string{".wasm"},
		magic: [][]byte{{0x00, 0x61, 0x73, 0x6D}}, // "\0asm"
	})
}

type bytecodeType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (b *bytecodeType) Name() string         { return b.name }
func (b *bytecodeType) Extensions() []string { return b.exts }
func (b *bytecodeType) MagicBytes() [][]byte { return b.magic }

// MatchMagic disambiguates Java .class from fat/universal Mach-O — both
// start with 0xCAFEBABE. The jvm type only claims the magic when the
// bytes that follow are NOT a fat-Mach-O header (issue #324). Other
// bytecode types fall back to the standard prefix match.
func (b *bytecodeType) MatchMagic(head []byte) bool {
	if b.name == "bytecode/jvm" {
		// Shares 0xCAFEBABE with fat Mach-O; claim it only when the
		// bytes that follow are NOT a fat-Mach-O header (#324).
		return matchAnyPrefix(head, b.magic) && !isMachoFatHeader(head)
	}
	return matchAnyPrefix(head, b.magic)
}

// Attributes dispatches to a per-format VM-bytecode parser. Each
// parser reads the format's header only — no instruction-stream
// disassembly — and surfaces the cross-format `bytecode_format` +
// `runtime_version` (agent-friendly summary) plus per-format extras.
//
// All parsers degrade gracefully: corrupt / truncated headers return
// empty attrs + nil err so the walker keeps moving. ctx is checked
// at function entry; none of the parsers loop unboundedly.
func (b *bytecodeType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch b.name {
	case "bytecode/jvm":
		return readClassInfo(fsys, path)
	case "bytecode/python":
		return readPYCInfo(fsys, path)
	case "bytecode/wasm":
		return readWASMInfo(fsys, path)
	}
	return nil, errors.New("unsupported bytecode type")
}

// bytecodeAttrs packs the cross-format surface (always present:
// bytecode_format + runtime_version) plus per-format extras into a
// content.Attributes map. Mirrors archiveAttrs / binaryAttrs /
// diskImageAttrs / installPackageAttrs.
func bytecodeAttrs(format, runtimeVersion string, extras Attributes) Attributes {
	out := Attributes{
		"bytecode_format": format,
	}
	if runtimeVersion != "" {
		out["runtime_version"] = runtimeVersion
	}
	maps.Copy(out, extras)
	return out
}
