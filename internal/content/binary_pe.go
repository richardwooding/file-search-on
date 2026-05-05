package content

import (
	"debug/pe"
	"io/fs"
)

// readPEInfo parses a PE binary's headers and returns the unified
// binary attribute surface. debug/pe handles the DOS-stub-to-PE
// signature navigation internally; callers see the COFF file header
// and either a 32 or 64-bit OptionalHeader directly.
func readPEInfo(fsys fs.FS, path string) (Attributes, error) {
	ra, _, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	f, err := pe.NewFile(ra)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr // graceful degradation: malformed PE returns empty attrs
	}
	defer func() { _ = f.Close() }()

	arch := peArchName(f.Machine)
	bitness, entryPoint := peBitnessAndEntry(f)

	binType := "executable"
	if f.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		binType = "shared_library"
	} else if f.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 {
		binType = "object"
	}

	isDynamic := peHasImports(f)

	// PE: combine the explicit DEBUG_STRIPPED bit with an empty COFF
	// symbol table. Modern toolchains emit empty Symbols + the bit;
	// older binaries may have one signal without the other.
	isStripped := f.Characteristics&pe.IMAGE_FILE_DEBUG_STRIPPED != 0 ||
		len(f.Symbols) == 0

	if binType == "shared_library" || binType == "object" {
		entryPoint = 0
	}

	return binaryAttrs([]string{arch}, bitness, "pe", binType, isDynamic, isStripped, entryPoint), nil
}

// peBitnessAndEntry inspects the OptionalHeader (which is either a 32
// or 64-bit variant) and returns the bitness plus the entry-point VA.
// PE32 has a 32-bit AddressOfEntryPoint; PE32+ has the same field but
// in a 64-bit struct (the address itself is still 32-bit, but the
// surrounding ImageBase widens to 64-bit).
func peBitnessAndEntry(f *pe.File) (int64, int64) {
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		return 32, int64(oh.AddressOfEntryPoint)
	case *pe.OptionalHeader64:
		return 64, int64(oh.AddressOfEntryPoint)
	}
	return 0, 0
}

// peHasImports checks the import directory entry — if its size is
// non-zero, the binary depends on at least one DLL. The directory
// entry sits in OptionalHeader.DataDirectory[1].
func peHasImports(f *pe.File) bool {
	const importDirIndex = 1
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if int(importDirIndex) >= len(oh.DataDirectory) {
			return false
		}
		return oh.DataDirectory[importDirIndex].Size > 0
	case *pe.OptionalHeader64:
		if int(importDirIndex) >= len(oh.DataDirectory) {
			return false
		}
		return oh.DataDirectory[importDirIndex].Size > 0
	}
	return false
}

// peArchName maps PE Machine constants to canonical arch strings.
func peArchName(m uint16) string {
	switch m {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x86_64"
	case pe.IMAGE_FILE_MACHINE_I386:
		return "i386"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64"
	case pe.IMAGE_FILE_MACHINE_ARM, pe.IMAGE_FILE_MACHINE_ARMNT:
		return "arm"
	case pe.IMAGE_FILE_MACHINE_RISCV64:
		return "riscv64"
	}
	return "unknown"
}
