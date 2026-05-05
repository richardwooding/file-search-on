package content

import (
	"debug/elf"
	"errors"
	"io/fs"
)

// readELFInfo parses an ELF binary's headers and returns the unified
// binary attribute surface. debug/elf reads file/program/section
// headers eagerly via NewFile, but section content stays demand-paged.
func readELFInfo(fsys fs.FS, path string) (Attributes, error) {
	ra, _, closer, err := openReaderAt(fsys, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	f, err := elf.NewFile(ra)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr // graceful degradation: malformed ELF returns empty attrs
	}
	defer func() { _ = f.Close() }()

	arch := elfArchName(f.Machine)
	bitness := int64(0)
	switch f.Class {
	case elf.ELFCLASS32:
		bitness = 32
	case elf.ELFCLASS64:
		bitness = 64
	}

	binType := "unknown"
	switch f.Type {
	case elf.ET_EXEC:
		binType = "executable"
	case elf.ET_DYN:
		// PIE executables and shared libraries both use ET_DYN.
		// Distinguishing them needs PT_INTERP scanning — DSOs don't
		// have an interpreter, PIE executables do.
		binType = "shared_library"
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP {
				binType = "executable"
				break
			}
		}
	case elf.ET_REL:
		binType = "object"
	case elf.ET_CORE:
		binType = "core"
	}

	isDynamic := false
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP || p.Type == elf.PT_DYNAMIC {
			isDynamic = true
			break
		}
	}

	isStripped := false
	if _, err := f.Symbols(); errors.Is(err, elf.ErrNoSymbols) {
		isStripped = true
	}

	entryPoint := int64(f.Entry) //nolint:gosec // Entry is a virtual address; conversion is intentional
	if binType == "shared_library" || binType == "object" {
		entryPoint = 0
	}

	return binaryAttrs([]string{arch}, bitness, "elf", binType, isDynamic, isStripped, entryPoint), nil
}

// elfArchName maps ELF e_machine constants to canonical arch strings
// shared across all three binary formats.
func elfArchName(m elf.Machine) string {
	switch m {
	case elf.EM_X86_64:
		return "x86_64"
	case elf.EM_386:
		return "i386"
	case elf.EM_AARCH64:
		return "arm64"
	case elf.EM_ARM:
		return "arm"
	case elf.EM_PPC64:
		return "ppc64"
	case elf.EM_PPC:
		return "ppc"
	case elf.EM_RISCV:
		return "riscv64"
	case elf.EM_MIPS:
		return "mips"
	case elf.EM_S390:
		return "s390x"
	}
	return "unknown"
}
