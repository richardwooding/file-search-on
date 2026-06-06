# Community files: what each one needs

## Contents

- [README.md](#readmemd)
- [CONTRIBUTING.md](#contributingmd)
- [CODE_OF_CONDUCT.md](#code_of_conductmd)
- [SECURITY.md](#securitymd)
- [CHANGELOG.md](#changelogmd)
- [Issue and PR templates](#issue-and-pr-templates)

## README.md

The first thing any visitor reads. The README answers, in order:

1. **What is this?** One sentence, no marketing.
2. **Why would I use it?** A concrete example or use-case.
3. **How do I install it?** Copy-pasteable commands.
4. **How do I use it?** A minimal example that actually works.
5. **Where do I go for more?** Docs, examples, godoc, etc.
6. **How do I contribute?** Pointer to CONTRIBUTING.md.
7. **What's the license?** Pointer to LICENSE.

Common mistakes:

- Burying the install command past the fold. Put it in the first 30 lines.
- "Coming soon" sections. Either it works now or it doesn't go in the README.
- Marketing voice. r/programming and HN can smell it from orbit.
- Screenshots only — copy-pasteable text always beats screenshots.

## CONTRIBUTING.md

Tells potential contributors how to be useful. Cover:

- **Dev setup** — exact commands to clone, install deps, build, test. Copy-paste.
- **Branching** — work off `main` or off a `develop` branch? feature branches off `main` is the common modern default.
- **Commit messages** — Conventional Commits (`feat:` / `fix:` / `chore:`)? plain English? whatever you've picked, say so.
- **Code style / linter** — point at the linter config; say "run `<tool> ./...` before pushing".
- **PR process** — squash-merge? merge-commit? required reviewers? required CI checks?
- **Code of conduct** — one line linking to CODE_OF_CONDUCT.md.
- **Reporting security issues** — one line saying "do NOT use issues; see SECURITY.md".

A small CONTRIBUTING.md that's accurate beats a long one full of process you don't actually follow.

## CODE_OF_CONDUCT.md

The community standard is the [Contributor Covenant](https://www.contributor-covenant.org/). The skill ships v2.1 boilerplate as a template — only customise the **enforcement contact** at the bottom (don't ship without that filled in; an unattended CoC is worse than no CoC).

Pitfalls:

- Forgetting to set the contact email — the placeholder in the template **must** be replaced before publishing.
- Linking only to an external CoC URL without including a copy in the repo. Include the file; GitHub looks for `CODE_OF_CONDUCT.md` to surface the badge.
- Adding a CoC and never enforcing it. If you're not prepared to act on reports, don't add one.

## SECURITY.md

GitHub surfaces this file in the "Security" tab and in the "Report a vulnerability" link on every issue. It needs:

- **Supported versions** — a small table saying which versions get security updates. (For very young projects: "Latest minor only.")
- **How to report a vulnerability** — preferably GitHub's [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/working-with-repository-security-advisories/configuring-private-vulnerability-reporting-for-a-repository) if enabled, **or** an email address. Don't say "open an issue" — that defeats the purpose.
- **Expected response time** — be honest. "We aim to acknowledge within 7 days" is better than promising 24 hours and missing.
- **Disclosure policy** — usually "coordinated disclosure: report privately, we'll work with you on a fix and credit you in the advisory".

Don't add SECURITY.md unless you're prepared to actually triage reports.

## CHANGELOG.md

Use the [Keep a Changelog](https://keepachangelog.com/) format. Sections per version: **Added / Changed / Deprecated / Removed / Fixed / Security**. Keep an `[Unreleased]` block at the top and move it to a versioned section on each release.

For projects using GoReleaser / release-please / semantic-release, the changelog is often auto-generated from commits. Either keep it manual *or* fully automated — don't half-do it (auto-generate then hand-edit selectively, and the file rots).

## Issue and PR templates

`.github/ISSUE_TEMPLATE/` can hold multiple templates (bug, feature, question). The minimum useful set:

- `bug_report.md` — what happened, what was expected, version, OS, repro steps.
- `feature_request.md` — what problem are you solving, what alternatives considered.

`.github/PULL_REQUEST_TEMPLATE.md` is a single file. Cover:

- A summary checklist (tests added / docs updated / changelog updated).
- A "what does this PR do" prompt.
- A "how to test" prompt.

Templates that are too long get ignored. Aim for a screen of content per template.
