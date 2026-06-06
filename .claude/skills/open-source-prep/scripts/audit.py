#!/usr/bin/env python3
"""
Audit a repository for open-source readiness.

Walks the repo at <repo-root>, checks for the standard community files,
GitHub UX files, repo metadata, and likely-leaked secrets in git history.
Emits a markdown punch-list to stdout.

Non-destructive: never writes to the repo.

Usage:
    python audit.py <repo-root>
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


# Files to check for at the repo root. (filename, label, severity)
ROOT_FILES = [
    ("LICENSE", "LICENSE", "legal"),
    ("LICENSE.md", "LICENSE.md", "legal"),
    ("LICENSE.txt", "LICENSE.txt", "legal"),
    ("COPYING", "COPYING", "legal"),
    ("README.md", "README.md", "discoverability"),
    ("README.rst", "README.rst", "discoverability"),
    ("README", "README", "discoverability"),
    ("CONTRIBUTING.md", "CONTRIBUTING.md", "community"),
    ("CODE_OF_CONDUCT.md", "CODE_OF_CONDUCT.md", "community"),
    ("SECURITY.md", "SECURITY.md", "community"),
    ("CHANGELOG.md", "CHANGELOG.md", "community"),
    (".gitignore", ".gitignore", "hygiene"),
    (".editorconfig", ".editorconfig", "hygiene"),
    (".gitattributes", ".gitattributes", "hygiene"),
]

# Files to check for under .github/.
GITHUB_FILES = [
    (".github/PULL_REQUEST_TEMPLATE.md", "PR template", "github-ux"),
    (".github/dependabot.yml", "dependabot config", "github-ux"),
    (".github/FUNDING.yml", "FUNDING.yml", "github-ux"),
]

# Directories under .github/ — at least one file inside.
GITHUB_DIRS = [
    (".github/ISSUE_TEMPLATE", "issue templates", "github-ux"),
    (".github/workflows", "GitHub Actions workflows", "github-ux"),
]

# SPDX identifiers we'll try to detect from the first 200 lines of a license file.
SPDX_PATTERNS = [
    ("MIT", re.compile(r"\bMIT License\b|Permission is hereby granted, free of charge")),
    ("Apache-2.0", re.compile(r"Apache License,?\s+Version 2\.0")),
    ("BSD-3-Clause", re.compile(r'Redistribution and use.*"AS IS"', re.DOTALL)),
    ("BSD-2-Clause", re.compile(r"BSD 2-Clause")),
    ("MPL-2.0", re.compile(r"Mozilla Public License,?\s+v?\.?\s*2\.0")),
    ("GPL-3.0", re.compile(r"GNU GENERAL PUBLIC LICENSE\s+Version 3")),
    ("AGPL-3.0", re.compile(r"GNU AFFERO GENERAL PUBLIC LICENSE\s+Version 3")),
    ("LGPL-3.0", re.compile(r"GNU LESSER GENERAL PUBLIC LICENSE\s+Version 3")),
    ("ISC", re.compile(r"ISC License")),
    ("Unlicense", re.compile(r"This is free and unencumbered software")),
]

# Likely-leaked-secret filenames to grep for in git history.
SECRET_FILE_PATTERNS = [
    r"\.env(\.|$)",
    r"\.env\.local",
    r"\.env\.production",
    r"credentials\.json",
    r"\.pem$",
    r"\.key$",
    r"\.p12$",
    r"\.pfx$",
    r"id_rsa(\.|$)",
    r"id_ed25519(\.|$)",
    r"\.netrc",
    r"secrets\.ya?ml",
    r"google-services\.json",
    r"firebase-adminsdk.*\.json",
]

# README sanity checks.
README_SECTIONS = {
    "install": re.compile(r"^#+\s*install", re.IGNORECASE | re.MULTILINE),
    "usage": re.compile(r"^#+\s*(usage|getting started|quick start)", re.IGNORECASE | re.MULTILINE),
    "license": re.compile(r"^#+\s*licen[cs]e|\bSPDX-License-Identifier\b", re.IGNORECASE | re.MULTILINE),
}
README_BADGE = re.compile(r"!\[[^\]]*\]\(https?://[^\)]+\)|<img[^>]+src=", re.IGNORECASE)


@dataclass
class Finding:
    label: str
    status: str  # "ok", "missing", "warn"
    detail: str = ""
    severity: str = "info"


def file_exists(repo: Path, name: str) -> bool:
    return (repo / name).is_file()


def dir_has_files(repo: Path, name: str) -> bool:
    p = repo / name
    return p.is_dir() and any(p.iterdir())


def detect_license(path: Path) -> str | None:
    try:
        text = path.read_text(errors="replace")
    except OSError:
        return None
    head = "\n".join(text.splitlines()[:200])
    for spdx, pattern in SPDX_PATTERNS:
        if pattern.search(head):
            return spdx
    return None


def check_legal(repo: Path) -> list[Finding]:
    candidates = ["LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"]
    for cand in candidates:
        p = repo / cand
        if p.is_file():
            spdx = detect_license(p) or "unrecognised"
            return [Finding(f"License file ({cand})", "ok",
                            detail=f"detected SPDX: {spdx}", severity="legal")]
    return [Finding("License file", "missing",
                    detail="no LICENSE / LICENSE.md / LICENSE.txt / COPYING found",
                    severity="legal")]


def check_readme(repo: Path) -> list[Finding]:
    findings: list[Finding] = []
    readmes = [p for p in ("README.md", "README.rst", "README") if (repo / p).is_file()]
    if not readmes:
        findings.append(Finding("README", "missing",
                                detail="no README.* found",
                                severity="discoverability"))
        return findings
    name = readmes[0]
    text = (repo / name).read_text(errors="replace")
    findings.append(Finding(f"{name}", "ok",
                            detail=f"{len(text)} chars", severity="discoverability"))
    # Section sanity (only for markdown).
    if name.endswith(".md"):
        for section, pattern in README_SECTIONS.items():
            if pattern.search(text):
                findings.append(Finding(f"README has '{section}' section", "ok",
                                        severity="discoverability"))
            else:
                findings.append(Finding(f"README has '{section}' section", "warn",
                                        detail=f"no '{section}' heading detected",
                                        severity="discoverability"))
        if README_BADGE.search(text):
            findings.append(Finding("README has at least one badge", "ok",
                                    severity="discoverability"))
        else:
            findings.append(Finding("README has at least one badge", "warn",
                                    detail="badges (build / license / version) help discoverability",
                                    severity="discoverability"))
    return findings


def check_root_files(repo: Path) -> list[Finding]:
    findings: list[Finding] = []
    seen_legal = False
    seen_readme = False
    for name, label, severity in ROOT_FILES:
        if severity == "legal":
            if seen_legal:
                continue
            if file_exists(repo, name):
                seen_legal = True
                continue
        elif severity == "discoverability" and label.startswith("README"):
            if seen_readme:
                continue
            if file_exists(repo, name):
                seen_readme = True
                continue
        else:
            if file_exists(repo, name):
                findings.append(Finding(label, "ok", severity=severity))
            else:
                findings.append(Finding(label, "missing", severity=severity))
    return findings


def check_github_ux(repo: Path) -> list[Finding]:
    findings: list[Finding] = []
    for path, label, severity in GITHUB_FILES:
        if file_exists(repo, path):
            findings.append(Finding(label, "ok", severity=severity))
        else:
            findings.append(Finding(label, "missing", severity=severity))
    for path, label, severity in GITHUB_DIRS:
        if dir_has_files(repo, path):
            findings.append(Finding(label, "ok", severity=severity))
        else:
            findings.append(Finding(label, "missing", severity=severity))
    return findings


def run(cmd: list[str], cwd: Path | None = None) -> tuple[int, str, str]:
    try:
        result = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, check=False)
    except FileNotFoundError:
        return 127, "", f"command not found: {cmd[0]}"
    return result.returncode, result.stdout, result.stderr


def check_repo_metadata(repo: Path) -> list[Finding]:
    findings: list[Finding] = []
    rc, _, _ = run(["gh", "auth", "status"], cwd=repo)
    if rc != 0:
        return [Finding("Repo metadata", "warn",
                        detail="`gh` CLI unavailable / not authenticated; skipping",
                        severity="metadata")]
    rc, out, err = run(
        ["gh", "repo", "view", "--json",
         "description,homepageUrl,repositoryTopics,isArchived,hasIssuesEnabled,visibility"],
        cwd=repo,
    )
    if rc != 0:
        return [Finding("Repo metadata", "warn",
                        detail=f"`gh repo view` failed: {err.strip()[:120]}",
                        severity="metadata")]
    try:
        data = json.loads(out)
    except json.JSONDecodeError:
        return [Finding("Repo metadata", "warn",
                        detail="could not parse `gh repo view` output",
                        severity="metadata")]
    desc = (data.get("description") or "").strip()
    homepage = (data.get("homepageUrl") or "").strip()
    topics = [t.get("name", "") for t in data.get("repositoryTopics") or []]
    archived = data.get("isArchived")
    issues = data.get("hasIssuesEnabled")
    visibility = data.get("visibility")

    findings.append(Finding(
        "Description", "ok" if desc else "missing",
        detail=desc if desc else "no description set; run `gh repo edit -d \"<short description>\"`",
        severity="metadata",
    ))
    findings.append(Finding(
        "Homepage URL", "ok" if homepage else "warn",
        detail=homepage if homepage else
        "no homepage; consider setting to docs site or pkg.go.dev: `gh repo edit -h <url>`",
        severity="metadata",
    ))
    findings.append(Finding(
        "Topics", "ok" if topics else "warn",
        detail=", ".join(topics) if topics else
        "no topics; helps discovery: `gh repo edit --add-topic <topic>`",
        severity="metadata",
    ))
    findings.append(Finding(
        "Issues enabled", "ok" if issues else "warn",
        detail="enabled" if issues else "disabled — open-source projects typically have issues on",
        severity="metadata",
    ))
    if archived:
        findings.append(Finding(
            "Archive status", "warn",
            detail="repo is ARCHIVED — readers see read-only banner",
            severity="metadata",
        ))
    if visibility and visibility.upper() != "PUBLIC":
        findings.append(Finding(
            "Visibility", "warn",
            detail=f"{visibility} — open-source repos must be public",
            severity="metadata",
        ))
    return findings


def secret_scan(repo: Path) -> list[Finding]:
    rc, _, _ = run(["git", "rev-parse", "--git-dir"], cwd=repo)
    if rc != 0:
        return [Finding("Git history scan", "warn",
                        detail="not a git repo; skipping secret scan",
                        severity="hygiene")]
    rc, out, _ = run(["git", "log", "--all", "--name-only", "--pretty=format:"], cwd=repo)
    if rc != 0:
        return [Finding("Git history scan", "warn",
                        detail="`git log` failed; skipping",
                        severity="hygiene")]
    seen: set[str] = set()
    hits: list[str] = []
    combined = re.compile("|".join(SECRET_FILE_PATTERNS), re.IGNORECASE)
    for line in out.splitlines():
        line = line.strip()
        if not line or line in seen:
            continue
        seen.add(line)
        if combined.search(line):
            hits.append(line)
    if hits:
        sample = ", ".join(hits[:5]) + ("…" if len(hits) > 5 else "")
        return [Finding("Likely-leaked secrets in history", "warn",
                        detail=f"{len(hits)} suspicious path(s) in git history: {sample}. "
                               "If real, see `git filter-repo` to scrub before going public.",
                        severity="hygiene")]
    return [Finding("No leaked-secret filenames in history", "ok", severity="hygiene")]


def emit_report(repo: Path, findings: list[Finding]) -> None:
    print(f"# Open-source readiness for `{repo.name}`")
    print()
    by_status = {"ok": [], "missing": [], "warn": []}
    for f in findings:
        by_status.setdefault(f.status, []).append(f)
    print("## Found")
    print()
    if not by_status["ok"]:
        print("(nothing)")
    for f in by_status["ok"]:
        d = f" — {f.detail}" if f.detail else ""
        print(f"- ✅ **{f.label}**{d}")
    print()
    print("## Missing (action required)")
    print()
    if not by_status["missing"]:
        print("(nothing)")
    for f in by_status["missing"]:
        d = f" — {f.detail}" if f.detail else ""
        print(f"- ❌ **{f.label}**{d}")
    print()
    print("## Warnings (review)")
    print()
    if not by_status["warn"]:
        print("(nothing)")
    for f in by_status["warn"]:
        d = f" — {f.detail}" if f.detail else ""
        print(f"- ⚠️  **{f.label}**{d}")
    print()
    summary = (f"{len(by_status['ok'])} ok, "
               f"{len(by_status['missing'])} missing, "
               f"{len(by_status['warn'])} warnings")
    print(f"_{summary}._")


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print(f"Usage: {argv[0]} <repo-root>", file=sys.stderr)
        return 2
    repo = Path(argv[1]).resolve()
    if not repo.is_dir():
        print(f"ERROR: {repo} is not a directory", file=sys.stderr)
        return 2
    findings: list[Finding] = []
    findings += check_legal(repo)
    findings += check_readme(repo)
    findings += check_root_files(repo)
    findings += check_github_ux(repo)
    findings += check_repo_metadata(repo)
    findings += secret_scan(repo)
    emit_report(repo, findings)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
