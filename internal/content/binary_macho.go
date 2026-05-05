package content

import (
	"debug/macho"
	"io/fs"
)

// readMachoInfo parses a Mach-O binary's headers and returns the unified
// binary attribute surface. Tries fat first via NewFatFile (handles the
// 0xCAFEBABE universal-binary header); falls back to thin Mach-O on
// FormatError. For fat files the architectures slice carries every
// slice's CPU; bitness reflects the first slice.
func readMachoInfo(fsys fs.FS, path string) (Attributes, error) {
	ra, _, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	if ff, err := macho.NewFatFile(ra); err == nil {
		defer func() { _ = ff.Close() }()
		return machoFatAttrs(ff), nil
	}

	f, err := macho.NewFile(ra)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr // graceful degradation: malformed Mach-O returns empty attrs
	}
	defer func() { _ = f.Close() }()

	return machoThinAttrs(f), nil
}

// machoThinAttrs collects attributes from a single-arch Mach-O file.
func machoThinAttrs(f *macho.File) Attributes {
	arch := machoArchName(f.Cpu)
	bitness := machoBitness(f.Magic)
	binType := machoBinType(f.Type)

	isDynamic := false
	for _, load := range f.Loads {
		if _, ok := load.(*macho.Dylib); ok {
			isDynamic = true
			break
		}
	}

	// Mach-O signals stripped via missing LC_SYMTAB. Real toolchains
	// emit LC_SYMTAB even for fully-stripped binaries (with empty
	// string/symbol tables), so we also check for an empty Syms slice.
	isStripped := f.Symtab == nil || len(f.Symtab.Syms) == 0

	// Mach-O doesn't expose the entry point directly. LC_MAIN carries
	// EntryOff (offset, not VA) on modern binaries; LC_UNIXTHREAD held
	// the registered RIP/EIP on older ones. debug/macho has no public
	// surface for either, so we report 0. Users who need it can switch
	// on binary_format == "mach-o" and use a richer tool.
	entryPoint := int64(0)

	return binaryAttrs([]string{arch}, bitness, "mach-o", binType, isDynamic, isStripped, entryPoint)
}

// machoFatAttrs collects attributes from a fat (universal) Mach-O.
// Each slice contributes one architecture; bitness/type/dynamic/
// stripped/entry come from the first slice (which is what users
// typically run on this host).
func machoFatAttrs(ff *macho.FatFile) Attributes {
	if len(ff.Arches) == 0 || ff.Arches[0].File == nil {
		return binaryAttrs([]string{}, 0, "mach-o", "unknown", false, false, 0)
	}

	archs := make([]string, 0, len(ff.Arches))
	for _, a := range ff.Arches {
		archs = append(archs, machoArchName(a.Cpu))
	}

	first := ff.Arches[0].File
	return mergeMachoFatAttrs(archs, first)
}

// mergeMachoFatAttrs builds the unified surface from the architectures
// list plus a representative *macho.File (the first slice's file).
func mergeMachoFatAttrs(archs []string, f *macho.File) Attributes {
	bitness := machoBitness(f.Magic)
	binType := machoBinType(f.Type)

	isDynamic := false
	for _, load := range f.Loads {
		if _, ok := load.(*macho.Dylib); ok {
			isDynamic = true
			break
		}
	}

	isStripped := f.Symtab == nil || len(f.Symtab.Syms) == 0

	return binaryAttrs(archs, bitness, "mach-o", binType, isDynamic, isStripped, 0)
}

// machoArchName maps macho.Cpu constants to canonical arch strings.
func machoArchName(c macho.Cpu) string {
	switch c {
	case macho.CpuAmd64:
		return "x86_64"
	case macho.Cpu386:
		return "i386"
	case macho.CpuArm64:
		return "arm64"
	case macho.CpuArm:
		return "arm"
	case macho.CpuPpc64:
		return "ppc64"
	case macho.CpuPpc:
		return "ppc"
	}
	return "unknown"
}

// machoBitness reads the 64-bit flag out of the Mach-O magic value.
func machoBitness(magic uint32) int64 {
	switch magic {
	case macho.Magic64:
		return 64
	case macho.Magic32:
		return 32
	}
	return 0
}

// machoBinType normalises the Mach-O Type to the cross-format
// binary_type vocabulary.
func machoBinType(t macho.Type) string {
	switch t {
	case macho.TypeExec:
		return "executable"
	case macho.TypeDylib:
		return "shared_library"
	case macho.TypeBundle:
		return "shared_library"
	case macho.TypeObj:
		return "object"
	}
	return "unknown"
}
