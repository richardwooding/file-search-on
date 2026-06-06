---
name: open-source-prep
description: Audits a repository for open-source readiness — checks for LICENSE, README, CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, CHANGELOG, `.github/` issue and PR templates, dependabot config, CI workflow, repo metadata (description / homepage / topics via `gh repo view`), and likely-leaked secrets in git history — then emits a prioritised punch-list. Pairs with templates for each community file (Contributor Covenant 2.1 boilerplate, MIT/Apache LICENSE pickers, README skeleton, CONTRIBUTING, SECURITY) and a license-choice decision guide. Use when preparing a repo for its first public release, taking a private repo public, reviewing a fork before contributing back, or hardening an already-open-source project.
---

# Open Source Prep

Walks a repository and reports what's missing before going public. Deterministic checks live in the audit script — the agent doesn't reinvent a checklist each time.

## Quick start

```sh
# 1. Audit (read-only). Prints markdown punch-list.
python .claude/skills/open-source-prep/scripts/audit.py <repo-root>

# 2. For each gap, copy the template and customise.
cp .claude/skills/open-source-prep/templates/CONTRIBUTING.md.tmpl <repo-root>/CONTRIBUTING.md

# 3. For ambiguous decisions (which license? which CoC version?), open the
#    matching reference file.
```

## What the audit checks

| Area | Check |
| --- | --- |
| **Legal** | `LICENSE` / `LICENSE.md` present + SPDX detection |
| **Discoverability** | `README.md` exists, has install + usage sections, has license / build / version badges |
| **Community** | `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `CHANGELOG.md` |
| **GitHub UX** | `.github/ISSUE_TEMPLATE/`, `.github/PULL_REQUEST_TEMPLATE.md`, `.github/dependabot.yml`, `.github/FUNDING.yml`, `.github/workflows/*.yml` |
| **Hygiene** | `.gitignore`, `.editorconfig`, `.gitattributes` |
| **Repo metadata** | `gh repo view` — description, homepage, topics, archived, has_issues |
| **Secret scan** | git history grep for `.env`, `*.pem`, `*.key`, `credentials.json`, `id_rsa`, common API-key patterns |

The audit is **non-destructive** — it never writes to the repo. It produces a markdown report and exits.

## What the audit does NOT check

- **Branch protection rules** — requires admin perms on the repo. The audit notes that protection should be enabled for `main` (require PR review, require status checks, no force-push) but doesn't query for it.
- **Code quality / test coverage** — out of scope; the project's existing CI handles this.
- **License compatibility of dependencies** — dedicated tools (`go-licenses`, `license-checker`, `cargo-deny`) do this better.
- **Vulnerability scan of dependencies** — Dependabot / Snyk / Renovate do this in the repo. The audit just checks that Dependabot is configured.

## Workflow

1. **Run the audit.** Read the markdown punch-list. Prioritise: **Legal → Community → Discoverability → Hygiene**.
2. **Pick a license** if missing. See [references/license-choice.md](references/license-choice.md) for a one-page decision tree (MIT, Apache 2.0, BSD-3, MPL-2.0, GPL-3.0, AGPL-3.0).
3. **Drop in templates** for missing community files. Each template has clearly marked `<placeholders>` for repo-specific bits — fill them in, don't ship them as-is.
4. **Set repo metadata** via `gh repo edit` — description, homepage, topics. The audit script prints exact commands.
5. **Re-run the audit** until the punch-list is empty (or all remaining items are intentional).

## Scripts

- **Run** `python scripts/audit.py <repo-root>` — emits a markdown readiness report. Requires `gh` CLI for repo metadata; degrades gracefully if `gh` is missing or the repo has no GitHub remote. Exits 0 on success regardless of findings (CI gating is a separate concern).

## References

- [references/license-choice.md](references/license-choice.md) — short decision tree: when to pick MIT vs Apache 2.0 vs BSD vs MPL vs GPL/AGPL. Includes the patent-grant question.
- [references/community-files.md](references/community-files.md) — what CONTRIBUTING / CODE_OF_CONDUCT / SECURITY / CHANGELOG should each contain, and the pitfalls (e.g. CoC missing a contact email).
- [references/repo-hygiene.md](references/repo-hygiene.md) — secret-scan patterns, dependabot config, branch protection settings, releases / tags strategy, and the "before-going-public" pre-flight.

## Templates

- [templates/README.md.tmpl](templates/README.md.tmpl) — minimal README skeleton with badges, install, usage, contributing, license sections.
- [templates/CONTRIBUTING.md.tmpl](templates/CONTRIBUTING.md.tmpl) — dev setup, branch / PR conventions, commit message style, code-of-conduct pointer.
- [templates/CODE_OF_CONDUCT.md.tmpl](templates/CODE_OF_CONDUCT.md.tmpl) — Contributor Covenant 2.1 boilerplate with `<contact email>` placeholder.
- [templates/SECURITY.md.tmpl](templates/SECURITY.md.tmpl) — supported versions, reporting channel, response SLA placeholder.
- [templates/CHANGELOG.md.tmpl](templates/CHANGELOG.md.tmpl) — Keep a Changelog format with semver headings and the Unreleased block.
- [templates/dependabot.yml.tmpl](templates/dependabot.yml.tmpl) — weekly checks for Go modules + GitHub Actions.

## Conventions

- **Audit before edit.** Always run `audit.py` first so the agent's recommendations are grounded in the current state, not assumptions.
- **Templates are starting points, not finished files.** Every template has placeholders. Read each before pasting; don't ship a `<repo>` placeholder to production.
- **Don't auto-commit.** This skill produces files in the working tree — the user reviews, customises, and commits manually. License changes and CoC additions are deliberate, not automatable.
- **Pair with `cut-release` for the actual release.** This skill prepares the repo; the release workflow is its own concern.
