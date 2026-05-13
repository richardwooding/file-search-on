# Security Policy

## Supported versions

Only the latest minor release receives security fixes. The project is pre-1.0; backporting fixes to older minor versions
isn't sustainable for a small team. If you need a fix on an older version, please pin to that version and apply the
patch locally, or upgrade.

| Version          | Supported |
|------------------|-----------|
| Latest `v0.26.0` | ✅        |
| Older `v0.25.0`  | ❌        |

## Reporting a vulnerability

**Please do NOT open a public GitHub issue for security problems.**

Email <richard.wooding@gmail.com> with:

- A description of the vulnerability.
- Steps to reproduce, or a proof-of-concept input.
- The version (`file-search-on --version`).
- The platform (OS, architecture).
- Whether you've shared this with anyone else.

You should expect an acknowledgement within **3 working days** and, where the report is valid, a patch + coordinated
disclosure within **30 days**. Larger windows happen — if I'm slow, please email again; it's not on purpose.

You'll be credited in the release notes unless you ask not to be.

## Scope

In scope:

- Crashes / hangs / OOMs in parsers (markdown frontmatter, MP3, MP4, MKV, OGG, FLAC, image EXIF, archives, binaries,
  email, gob index decoder) given adversarial input — the fuzz suite covers the known surface; new findings are welcome.
- Crashes / hangs in the CEL evaluator given crafted expressions.
- Crashes / hangs in the MCP server given crafted protocol input.
- Path traversal, directory escape, or other filesystem boundary violations in the walker.
- Memory exhaustion via crafted inputs (e.g. malformed archive headers, gob length prefixes, MP4 box sizes —
  see [PR #100](https://github.com/richardwooding/file-search-on/pull/100) for the kind of issues we want reported).

Out of scope:

- Vulnerabilities in dependencies — please report upstream (Dependabot tracks these for us).
- Denial-of-service via legitimately huge filesystems (we provide `--timeout` and `--workers` flags; tune them).
- Social engineering, physical access, or supply-chain attacks on the build pipeline (those go to GitHub Security
  directly).
