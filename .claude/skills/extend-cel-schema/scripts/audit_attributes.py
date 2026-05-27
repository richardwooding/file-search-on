#!/usr/bin/env python3
"""Audit file-search-on's CEL attribute schema for drift between the call sites.

Usage: python audit_attributes.py [--repo-root .]

Exits 0 if every CEL attribute is declared, resolvable, and documented
consistently; non-zero on drift. Prints a markdown table summarising each
attribute.

The places a CEL attribute must appear (see extend-cel-schema/SKILL.md):
  1. cel.Variable("foo", ...) declarations in celexpr.New
     (internal/celexpr/env.go)                                        -> declared
  2. resolvable in (*fileAttrsActivation).ResolveName
     (internal/celexpr/activation.go), via EITHER:
       a. a `case "foo":` label returning a typed FileAttributes field
          (common scalars + is_* predicates + md5/sha1/...)           -> cased
       b. a key in the `zeroDefaults` map literal (type-specific attrs
          that flow through the verbatim `Extra[name]` fallthrough)    -> defaulted
  3. AttributeDoc{...} entries in celexpr.Schema()
     (internal/celexpr/schema.go)                                     -> documented

Note (2024 refactor): the activation machinery moved out of celexpr.Evaluate
into internal/celexpr/activation.go. There is no longer an
`activation["foo"] = v` rename switch — type-specific attributes resolve by
verbatim key match against FileAttributes.Extra (so the content type must
emit the final CEL name directly), falling back to zeroDefaults when a file
didn't emit them. "Resolvable" therefore means cased OR defaulted.

Invariants:
  declared == (cased ∪ defaulted)        (every declared var resolves; no orphans — hard error)
  documented ⊆ declared                  (can't document an undeclared var — hard error)
  documented (TypeSpecific ∪ Frontmatter) ⊆ defaulted
                                         (Extra-flowing docs need a zero default so files
                                          that don't emit them don't error — hard error)
  declared - documented                  (warn — undocumented, hidden from --list)
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


CEL_VARIABLE_RE = re.compile(r'cel\.Variable\(\s*"([^"]+)"')
# A `case "a":` or `case "a", "b":` label line inside ResolveName.
CASE_LINE_RE = re.compile(r'^\s*case\s+(.+?):\s*$')
QUOTED_RE = re.compile(r'"([^"]+)"')
ACTIVATION_KEY_RE = re.compile(r'^\s*"([^"]+)"\s*:')
DOC_ENTRY_RE = re.compile(r'\{\s*"([^"]+)"\s*,\s*"([^"]*)"\s*,\s*"[^"]*"\s*\}')


def find_block(text: str, start_marker: str, what: str) -> str:
    """Return the brace-balanced block body following start_marker (the
    marker must end with the opening `{`)."""
    start = text.find(start_marker)
    if start < 0:
        raise SystemExit(f"ERROR: could not find `{start_marker}` ({what})")
    open_brace = start + len(start_marker) - 1
    depth = 1
    i = open_brace + 1
    while i < len(text) and depth > 0:
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
        i += 1
    return text[open_brace + 1 : i - 1]


def parse_declared(evaluator_text: str) -> set[str]:
    """cel.Variable declarations in celexpr.New."""
    return set(CEL_VARIABLE_RE.findall(evaluator_text))


def parse_activation(activation_text: str) -> tuple[set[str], set[str]]:
    """Return (cased, defaulted) from activation.go.

    cased     — string labels of every `case "..."` in ResolveName's switch.
    defaulted — keys of the `var zeroDefaults = map[string]any{...}` literal.
    """
    # 1. ResolveName switch case labels (handles multi-label `case "a", "b":`).
    body = find_block(
        activation_text,
        "func (a *fileAttrsActivation) ResolveName(name string) (any, bool) {",
        "ResolveName in activation.go",
    )
    cased: set[str] = set()
    for line in body.splitlines():
        m = CASE_LINE_RE.match(line)
        if m:
            cased.update(QUOTED_RE.findall(m.group(1)))

    # 2. zeroDefaults map keys.
    defaults_block = find_block(
        activation_text,
        "var zeroDefaults = map[string]any{",
        "zeroDefaults in activation.go",
    )
    defaulted: set[str] = set()
    for line in defaults_block.splitlines():
        m = ACTIVATION_KEY_RE.match(line)
        if m:
            defaulted.add(m.group(1))

    return cased, defaulted


def parse_schema(text: str) -> dict[str, list[tuple[str, str]]]:
    """Map slice name (Common / TypeSpecific / Frontmatter) to (name, type) tuples."""
    result: dict[str, list[tuple[str, str]]] = {}
    for slice_name in ("Common", "TypeSpecific", "Frontmatter"):
        block = find_block(text, f"{slice_name}: []AttributeDoc{{", f"{slice_name} in schema.go")
        result[slice_name] = [(m.group(1), m.group(2)) for m in DOC_ENTRY_RE.finditer(block)]
    return result


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo-root", type=Path, default=Path("."), help="Repo root (default: cwd)")
    args = ap.parse_args()

    root = args.repo_root.resolve()
    # cel.Variable declarations live in celexpr.New, which is in env.go
    # (split out of evaluator.go for navigability).
    env_path = root / "internal" / "celexpr" / "env.go"
    activation_path = root / "internal" / "celexpr" / "activation.go"
    schema_path = root / "internal" / "celexpr" / "schema.go"
    for p in (env_path, activation_path, schema_path):
        if not p.exists():
            print(f"ERROR: {p} not found (run from the repo root, or pass --repo-root)", file=sys.stderr)
            return 1

    declared = parse_declared(env_path.read_text(encoding="utf-8"))
    cased, defaulted = parse_activation(activation_path.read_text(encoding="utf-8"))
    schema = parse_schema(schema_path.read_text(encoding="utf-8"))

    resolvable = cased | defaulted

    documented_common = {n for n, _ in schema["Common"]}
    documented_typespec = {n for n, _ in schema["TypeSpecific"]}
    documented_frontmatter = {n for n, _ in schema["Frontmatter"]}
    documented_all = documented_common | documented_typespec | documented_frontmatter
    extra_flowing_docs = documented_typespec | documented_frontmatter

    errors: list[str] = []
    warnings: list[str] = []

    # Invariant 1: declared == resolvable.
    declared_unresolvable = declared - resolvable
    if declared_unresolvable:
        errors.append(
            "declared via cel.Variable but neither a ResolveName `case` nor a "
            f"zeroDefaults key (→ runtime 'no such attribute'): {sorted(declared_unresolvable)}"
        )
    resolvable_undeclared = resolvable - declared
    if resolvable_undeclared:
        errors.append(
            "resolvable in activation.go (case or zeroDefaults) but not declared via "
            f"cel.Variable: {sorted(resolvable_undeclared)}"
        )

    # Invariant 2: documented ⊆ declared.
    documented_not_declared = documented_all - declared
    if documented_not_declared:
        errors.append(
            f"documented in schema.go but not declared via cel.Variable: {sorted(documented_not_declared)}"
        )

    # Invariant 3: Extra-flowing docs (TypeSpecific ∪ Frontmatter) need a zero
    # default — they resolve via the verbatim Extra fallthrough, so a file that
    # doesn't emit them must fall back to a default or cel-go errors. (A doc
    # that's instead handled by a typed `case` is fine — exclude those.)
    extra_flowing_missing_default = extra_flowing_docs - defaulted - cased
    if extra_flowing_missing_default:
        errors.append(
            "documented as type-specific/front-matter but no zeroDefaults entry "
            f"(and not a typed ResolveName case): {sorted(extra_flowing_missing_default)}"
        )

    # Warning: declared but undocumented (hidden from --list / list_attributes).
    declared_not_documented = declared - documented_all
    if declared_not_documented:
        warnings.append(
            "declared via cel.Variable but not documented in schema.go (won't show "
            f"in --list or list_attributes): {sorted(declared_not_documented)}"
        )

    # Markdown report.
    print("| Attribute | Declared | Cased | Defaulted | Documented |")
    print("| --- | --- | --- | --- | --- |")
    every = sorted(declared | resolvable | documented_all)
    for name in every:
        slot = ""
        if name in documented_common:
            slot = "Common"
        elif name in documented_typespec:
            slot = "TypeSpecific"
        elif name in documented_frontmatter:
            slot = "Frontmatter"
        d = "✓" if name in declared else "—"
        c = "✓" if name in cased else "—"
        f_ = "✓" if name in defaulted else "—"
        doc = slot if slot else "—"
        print(f"| `{name}` | {d} | {c} | {f_} | {doc} |")
    print()
    print("Legend: ✓ = present, — = absent. A declared attr must be Cased OR Defaulted.")
    print()

    for w in warnings:
        print(f"WARN  {w}")
    for e in errors:
        print(f"ERROR {e}")

    print(f"\n{len(errors)} error(s), {len(warnings)} warning(s)")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
