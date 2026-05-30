# Recipes — VM bytecode

VM-bytecode content types: `bytecode/jvm` (Java `.class`), `bytecode/python` (CPython `.pyc` / `.pyo`), `bytecode/wasm` (WebAssembly `.wasm`). Umbrella boolean `is_bytecode`.

Each parser reads the format's header only — no instruction-stream disassembly, no constant-pool full decode beyond what the surface attributes need. Pure-Go stdlib, no third-party libs. JAR / WAR / EAR / AAR are NOT here — those detect as `archive/zip` and you walk their contents via `archive-contents` to surface the individual `.class` entries inside.

## All-bytecode triage

```sh
file-search-on 'is_bytecode' -d ~/Code
file-search-on 'is_bytecode' -d /opt/homebrew/share --follow-symlinks
```

By format:

```sh
file-search-on 'is_class' -d ./target/classes               # Maven build output
file-search-on 'is_pyc'   -d ./__pycache__                  # Python compile cache
file-search-on 'is_wasm'  -d ./web/dist                     # Bundled WASM modules
```

Or by the cross-format string:

```sh
file-search-on 'bytecode_format == "jvm"' -d ./target
file-search-on 'bytecode_format == "wasm"' -d .
```

## JVM `.class` recipes

### Find classes by name pattern

```sh
# Specific class
file-search-on 'is_class && class_name == "com/example/Service"' -d ./target/classes

# Pattern via CEL string methods
file-search-on 'is_class && class_name.startsWith("com/example/")' -d ./target/classes
file-search-on 'is_class && class_name.contains("Controller")' -d ./target/classes
```

### Filter by access flags

```sh
# Every public class
file-search-on 'is_class && "public" in access_flags' -d ./target/classes

# Public abstract base classes (potential refactor targets)
file-search-on 'is_class && "public" in access_flags && "abstract" in access_flags' -d ./target/classes

# Interfaces
file-search-on 'is_class && "interface" in access_flags' -d ./target/classes

# Enums
file-search-on 'is_class && "enum" in access_flags' -d ./target/classes
```

### Find big / interface-heavy classes

```sh
# Classes with > 50 methods (god-class candidates)
file-search-on 'is_class && method_count > 50' -d ./target/classes --sort-by method_count --order desc --limit 10

# Classes implementing multiple interfaces
file-search-on 'is_class && size(interfaces) >= 2' -d ./target/classes
```

### Filter by superclass

```sh
# Every class extending Spring's AbstractController
file-search-on 'is_class && super_class == "org/springframework/web/servlet/mvc/AbstractController"' -d ./target/classes

# Custom base classes via pattern
file-search-on 'is_class && super_class.startsWith("com/example/base/")' -d ./target/classes
```

### Filter by Java version

```sh
# Classes compiled for Java 17 specifically
file-search-on 'is_class && runtime_version == "Java 17"' -d ./target/classes

# Classes compiled for any of Java 17, 21
file-search-on 'is_class && (runtime_version == "Java 17" || runtime_version == "Java 21")' -d ./target/classes

# Audit: classes compiled for too-old Java
file-search-on 'is_class && runtime_version.startsWith("Java 1.")' -d ./target/classes
```

## Python `.pyc` recipes

### Filter by Python version

```sh
# All .pyc compiled by Python 3.11
file-search-on 'is_pyc && python_version == "3.11"' -d ~/Code/myproject

# Audit for mixed Python versions in __pycache__/
file-search-on stats 'is_pyc' -d . --group-by python_version
```

### Source mtime queries

```sh
# .pyc files whose source was modified recently (e.g. after 2026-01-01)
file-search-on 'is_pyc && source_mtime > timestamp("2026-01-01T00:00:00Z")' -d .

# Stale .pyc files — source older than 1 year ago
file-search-on 'is_pyc && source_mtime < timestamp("2025-05-17T00:00:00Z")' -d .
```

### Find orphaned `__pycache__`

```sh
# Every .pyc under directories that don't look like active projects
file-search-on 'is_pyc' -d ~/Code --exclude .git --exclude .venv
```

## WebAssembly `.wasm` recipes

### Module metadata

```sh
# Every WASM module under a build output
file-search-on 'is_wasm' -d ./dist

# Modules with no exports — pure libraries embedded in another module
file-search-on 'is_wasm && export_count == 0' -d ./dist

# Modules with lots of imports — heavy host-binding surface (WebGPU, WASI)
file-search-on 'is_wasm && import_count > 50' -d ./dist
```

### Section reconnaissance

```sh
# Modules with unusually many sections (custom sections, debug info)
file-search-on 'is_wasm && section_count > 20' -d ./dist --sort-by section_count --order desc

# Audit by WASM version (currently only v1 exists)
file-search-on 'is_wasm && wasm_version != 1' -d ./dist
```

## Stats

```sh
# How many bytecode artefacts per format?
file-search-on stats 'is_bytecode' -d ~/Code --group-by bytecode_format

# Java version histogram
file-search-on stats 'is_class' -d ./target/classes --group-by runtime_version

# Python version histogram
file-search-on stats 'is_pyc' -d . --group-by python_version
```

## Searching INSIDE archives

JAR / WAR / EAR are ZIP envelopes. The bytecode types fire on the individual `.class` entries inside:

```sh
# Find every class inside a JAR
file-search-on archive-contents ./target/myapp.jar --expr 'is_class' --include-attributes

# Find interfaces inside a deploy JAR
file-search-on archive-contents ./target/myapp.jar --expr 'is_class && "interface" in access_flags'

# Count Java versions inside an EAR
file-search-on archive-contents ./target/myapp.ear --expr 'is_class' --include-attributes -o json | \
  jq -r '.entries[].attributes.runtime_version' | sort | uniq -c
```

The same applies to `.pyc` files inside Python wheel (`.whl`) archives and `.wasm` modules inside web-bundle archives.

## Caveats

- **`.NET` PE assemblies** detect as `binary/pe`, not `bytecode/*`. They're PE files on disk with CLR metadata in a specific PE section — separate code path. Left for a future follow-up; no issue currently tracking.
- **JAR / WAR / EAR / AAR** detect as `archive/zip` (the existing alias list). Use `archive-contents` to walk the inner `.class` entries.
- **Python magic numbers go out of date**. Each CPython release picks a new magic; the table covers 3.7-3.14. Unknown magics return empty `runtime_version` but still detect as `bytecode/python`.
- **`.class` constant-pool walking caps at 65535 entries** (the JVMS hard limit). Larger files are truncated rather than reading unboundedly.
- **WASM section count caps at 128 per module**. Modules with more sections are partial-parsed and `section_count` reflects what was walked, not the file's true count.
- **No instruction-stream disassembly**. Method bodies, opcode counts, and the like are out-of-scope. For deeper analysis, use a dedicated tool (`javap`, `dis` for Python, `wasm-objdump`).
- **No JVM reflection**: the constant-pool entries other than `CONSTANT_Class` (e.g. `CONSTANT_Methodref`) aren't surfaced. Surfacing "the methods this class calls" would be a much bigger feature.
