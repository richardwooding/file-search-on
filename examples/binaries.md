# Recipes — Binaries

Compiled-binary content types: `binary/elf` (ELF — Linux/BSD), `binary/mach-o` (Mach-O — macOS, including universal/fat), `binary/pe` (PE — Windows). Umbrella boolean `is_binary`.

Hand-rolled on top of Go's `debug/elf`, `debug/macho`, and `debug/pe` — no CGo, no external libs. Out of scope for v1: WASM, COFF object files, Java `.class`, DWARF / debuglink resolution, code signing.

## All-binaries triage

The umbrella query — every executable / library / object under a directory:

```sh
file-search-on 'is_binary' -d /usr/local/bin
```

By format:

```sh
file-search-on 'is_binary && content_type == "binary/elf"'    -d /usr/bin
file-search-on 'is_binary && content_type == "binary/mach-o"' -d /Applications
file-search-on 'is_binary && content_type == "binary/pe"'     -d /mnt/c/Windows/System32
```

Or the same via the CEL string:

```sh
file-search-on 'is_binary && binary_format == "elf"'    -d /usr/bin
file-search-on 'is_binary && binary_format == "mach-o"' -d /Applications
file-search-on 'is_binary && binary_format == "pe"'     -d /mnt/c/Windows/System32
```

## Filter by architecture

`architectures` is always a list — length 1 for thin binaries, length ≥ 2 for fat (universal) Mach-O. Use `in` for membership:

```sh
# Find all arm64-capable binaries (works for thin arm64 AND fat ones with arm64 slices).
file-search-on 'is_binary && "arm64" in architectures' -d /Applications

# Pure x86_64 — single-arch binaries that are NOT fat.
file-search-on 'is_binary && architectures.size() == 1 && architectures[0] == "x86_64"' -d /usr/local/bin

# Fat (universal) Mach-O — multi-arch.
file-search-on 'is_binary && architectures.size() > 1' -d /Applications
```

Find binaries that match the host arch on Apple Silicon — useful when triaging an Intel Homebrew bin tree migrated under Rosetta 2:

```sh
file-search-on 'is_binary && !( "arm64" in architectures )' -d /usr/local/bin
```

## Static vs dynamic linking

Find statically-linked binaries — no shared-library dependencies. Common for Go and Rust binaries published as drop-in CLI tools:

```sh
file-search-on 'is_binary && !is_dynamically_linked' -d ~/go/bin
file-search-on 'is_binary && !is_dynamically_linked' -d ~/.cargo/bin
```

Find dynamically-linked binaries — the dominant case on Linux / macOS, and the kind you can't trivially copy to another machine without dragging dependencies:

```sh
file-search-on 'is_binary && is_dynamically_linked' -d /usr/local/bin
```

## Stripped vs symboled

Stripped binaries have no symbol table — smaller, harder to debug, common for shipping releases:

```sh
file-search-on 'is_binary && is_stripped' -d /usr/local/bin
file-search-on 'is_binary && !is_stripped' -d ~/go/bin   # Go's default builds keep symbols
```

Combine: stripped Go binaries (release artifacts) under a build tree:

```sh
file-search-on 'is_binary && is_stripped && !is_dynamically_linked' -d ./dist
```

## Rogue-format hunting

Find Windows `.exe` / `.dll` files in a Unix home directory — often left over from cross-compile toolchains, Wine prefixes, or downloads that snuck in:

```sh
file-search-on 'is_binary && binary_format == "pe"' -d ~
```

The reverse — ELF binaries somewhere they shouldn't be (e.g. on a macOS machine outside `/usr/local`):

```sh
file-search-on 'is_binary && binary_format == "elf"' -d ~/Downloads
```

## Shared libraries vs executables

Filter by `binary_type`. The cross-format vocabulary is `executable`, `shared_library`, `object`, `core`, or `unknown`:

```sh
file-search-on 'is_binary && binary_type == "shared_library"' -d /usr/local/lib   # ELF .so / Mach-O .dylib / PE .dll
file-search-on 'is_binary && binary_type == "executable"'     -d /usr/local/bin
file-search-on 'is_binary && binary_type == "object"'         -d ./build           # ELF .o relocatables
```

Note: ELF distinguishes PIE executables (which are `ET_DYN` like shared libraries) by checking for a `PT_INTERP` segment. The `binary_type` attribute already does this — a Go-built PIE binary reports `executable`, not `shared_library`.

## Bitness audits

Find leftover 32-bit binaries on a 64-bit host:

```sh
file-search-on 'is_binary && bitness == 32' -d /usr/local/bin
```

The full `i386`-only filter (excludes 32-bit ARM):

```sh
file-search-on 'is_binary && bitness == 32 && "i386" in architectures' -d /usr/local/bin
```

## Useful output formats

```sh
# Path + format + arch + size, tab-separated. The default verbose output
# already covers all binary fields; this tightens it for grep / cut.
file-search-on 'is_binary' --format '{{.Path}}\t{{.BinaryFormat}}\t{{index .Architectures 0}}\t{{.Size}}'

# JSON for jq pipelines — group binaries by architecture.
file-search-on 'is_binary' -d /usr/local/bin -o json |
  jq -s 'group_by(.architectures[0]) | map({arch: .[0].architectures[0], count: length})'

# Bare paths for xargs (e.g. strip every non-stripped binary in a build tree).
file-search-on 'is_binary && !is_stripped && !is_dynamically_linked' -d ./dist -o bare |
  xargs -I {} strip {}
```
