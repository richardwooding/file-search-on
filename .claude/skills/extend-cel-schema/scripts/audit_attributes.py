#!/usr/bin/env python3
"""Audit file-search-on's CEL attribute schema for drift between the four call sites.

Usage: python audit_attributes.py [--repo-root .]

Exits 0 if every CEL attribute appears in every place it must, non-zero on drift.
Prints a markdown table summarising the state of each attribute.

The four places (see extend-cel-schema/SKILL.md):
  1. cel.Variable("foo", ...) declarations in celexpr.New             (-> declared)
  2. activation defaults map literal in celexpr.Evaluate              (-> defaulted)
  3. attrs.Extra switch case assignments in celexpr.Evaluate          (-> assigned)
  4. AttributeDoc{...} entries in celexpr.Schema()                    (-> documented)

Invariants:
  declared == defaulted                                          (sanity, hard error on drift)
  documented (any slice) ⊆ declared                              (hard error)
  documented (TypeSpecific ∪ Frontmatter) ⊆ assigned             (hard error)
  declared - documented                                          (warn — undocumented attribute)
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


CEL_VARIABLE_RE = re.compile(r'cel\.Variable\(\s*"([^"]+)"')
ACTIVATION_KEY_RE = re.compile(r'^\s*"([^"]+)"\s*:')
ASSIGN_RE = re.compile(r'activation\[\s*"([^"]+)"\s*\]\s*=')
DOC_ENTRY_RE = re.compile(r'\{\s*"([^"]+)"\s*,\s*"([^"]*)"\s*,\s*"[^"]*"\s*\}')


def parse_evaluator(text: str) -> tuple[set[str], set[str], set[str]]:
    """Return (declared, defaulted, assigned) sets parsed from evaluator.go."""
    declared = set(CEL_VARIABLE_RE.findall(text))

    # Locate the activation map literal: starts at `activation := map[string]any{`
    # ends at the matching closing brace at the same indentation level.
    start_marker = "activation := map[string]any{"
    start = text.find(start_marker)
    if start < 0:
        raise SystemExit("ERROR: could not find `activation := map[string]any{` in evaluator.go")
    # Track brace depth from the opening { in the marker
    open_brace = start + len(start_marker) - 1
    depth = 1
    i = open_brace + 1
    while i < len(text) and depth > 0:
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
        i += 1
    activation_block = text[open_brace + 1 : i - 1]
    defaulted = set()
    for line in activation_block.splitlines():
        m = ACTIVATION_KEY_RE.match(line)
        if m:
            defaulted.add(m.group(1))

    assigned = set(ASSIGN_RE.findall(text))
    return declared, defaulted, assigned


def parse_schema(text: str) -> dict[str, list[tuple[str, str]]]:
    """Return mapping of slice name (Common / TypeSpecific / Frontmatter) to a list
    of (attribute_name, attribute_type) tuples in source order."""
    result: dict[str, list[tuple[str, str]]] = {}
    # Find each `Common: []AttributeDoc{` / `TypeSpecific: []AttributeDoc{` / `Frontmatter: []AttributeDoc{`
    for slice_name in ("Common", "TypeSpecific", "Frontmatter"):
        marker = f"{slice_name}: []AttributeDoc{{"
        start = text.find(marker)
        if start < 0:
            raise SystemExit(f"ERROR: could not find `{marker}` in schema.go")
        # Find matching closing brace
        open_brace = start + len(marker) - 1
        depth = 1
        i = open_brace + 1
        while i < len(text) and depth > 0:
            if text[i] == "{":
                depth += 1
            elif text[i] == "}":
                depth -= 1
            i += 1
        block = text[open_brace + 1 : i - 1]
        result[slice_name] = [(m.group(1), m.group(2)) for m in DOC_ENTRY_RE.finditer(block)]
    return result


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo-root", type=Path, default=Path("."), help="Repo root (default: cwd)")
    args = ap.parse_args()

    root = args.repo_root.resolve()
    evaluator_path = root / "internal" / "celexpr" / "evaluator.go"
    schema_path = root / "internal" / "celexpr" / "schema.go"
    for p in (evaluator_path, schema_path):
        if not p.exists():
            print(f"ERROR: {p} not found (run from the repo root, or pass --repo-root)", file=sys.stderr)
            return 1

    declared, defaulted, assigned = parse_evaluator(evaluator_path.read_text(encoding="utf-8"))
    schema = parse_schema(schema_path.read_text(encoding="utf-8"))

    documented_common = {n for n, _ in schema["Common"]}
    documented_typespec = {n for n, _ in schema["TypeSpecific"]}
    documented_frontmatter = {n for n, _ in schema["Frontmatter"]}
    documented_all = documented_common | documented_typespec | documented_frontmatter
    must_be_assigned = documented_typespec | documented_frontmatter

    errors: list[str] = []
    warnings: list[str] = []

    # Invariant 1: declared == defaulted
    only_declared = declared - defaulted
    only_defaulted = defaulted - declared
    if only_declared:
        errors.append(
            f"declared but missing zero-value default in activation map: {sorted(only_declared)}"
        )
    if only_defaulted:
        errors.append(
            f"defaulted in activation map but not declared via cel.Variable: {sorted(only_defaulted)}"
        )

    # Invariant 2: documented ⊆ declared
    documented_not_declared = documented_all - declared
    if documented_not_declared:
        errors.append(
            f"documented in schema.go but not declared via cel.Variable: {sorted(documented_not_declared)}"
        )

    # Invariant 3: type-specific & front-matter docs ⊆ assigned
    must_assign_missing = must_be_assigned - assigned
    if must_assign_missing:
        errors.append(
            f"documented as type-specific/front-matter but no `activation[\"foo\"] = v` "
            f"assignment in the attrs.Extra switch: {sorted(must_assign_missing)}"
        )

    # Sanity: assignments should target declared variables (unless they are common — but
    # common attrs are populated from struct fields, never via the switch, so any switch
    # assignment is to a type-specific or front-matter slot).
    assigned_not_declared = assigned - declared
    if assigned_not_declared:
        errors.append(
            f"attrs.Extra switch assigns to undeclared CEL variable: {sorted(assigned_not_declared)}"
        )

    # Warning: declared but undocumented (the `--list` view will hide it).
    declared_not_documented = declared - documented_all
    if declared_not_documented:
        warnings.append(
            f"declared via cel.Variable but not documented in schema.go (won't show "
            f"in --list or list_attributes): {sorted(declared_not_documented)}"
        )

    # Build the markdown report.
    print("| Attribute | Declared | Defaulted | Assigned | Documented |")
    print("| --- | --- | --- | --- | --- |")
    every = sorted(declared | defaulted | assigned | documented_all)
    for name in every:
        slot = ""
        if name in documented_common:
            slot = "Common"
        elif name in documented_typespec:
            slot = "TypeSpecific"
        elif name in documented_frontmatter:
            slot = "Frontmatter"
        d = "✓" if name in declared else "—"
        f_ = "✓" if name in defaulted else "—"
        a = "✓" if name in assigned else ("·" if slot == "Common" else "—")
        doc = slot if slot else "—"
        print(f"| `{name}` | {d} | {f_} | {a} | {doc} |")
    print()
    print("Legend: ✓ = present, — = missing, · = N/A (common attrs are not assigned via the switch)")
    print()

    for w in warnings:
        print(f"WARN  {w}")
    for e in errors:
        print(f"ERROR {e}")

    print(f"\n{len(errors)} error(s), {len(warnings)} warning(s)")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
