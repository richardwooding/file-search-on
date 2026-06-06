# Repo hygiene before going public

## Contents

- [The before-going-public pre-flight](#the-before-going-public-pre-flight)
- [Secret scanning](#secret-scanning)
- [Branch protection](#branch-protection)
- [Dependabot](#dependabot)
- [Releases and tags](#releases-and-tags)
- [Repo metadata](#repo-metadata)

## The before-going-public pre-flight

Things that are easy to do privately, painful publicly. Do these **before** flipping visibility:

1. **Audit git history for secrets.** The audit script greps for common secret-file paths. If anything turns up, scrub with [`git filter-repo`](https://github.com/newren/git-filter-repo) (better than `git filter-branch`) before going public. Once it's pushed and forked, it's pushed and forked.
2. **Search the diff for `TODO/FIXME/XXX` with internal context.** Comments like `// TODO: ask Bob about the API key` aren't secrets, but they signal carelessness.
3. **Check the dependency tree.** Are any of your transitive deps GPL'd in a way that conflicts with your license? `go-licenses csv ./...` (Go) / `license-checker --production` (npm) / `cargo-deny check` (Rust).
4. **Test from a clean clone.** `git clone <repo> /tmp/clean && cd /tmp/clean && <build commands>`. If your README's install steps don't work in a fresh checkout, contributors won't make it past the first try.
5. **Set repo description, homepage, and topics** before flipping visibility. They're searchable on GitHub, and the first impression matters.
6. **Configure branch protection** on `main` (require PR review, require status checks to pass, dismiss stale reviews). The `repo-create-time` defaults are insufficient.
7. **Enable private vulnerability reporting** if available (Settings → Code security → Private vulnerability reporting → Enabled). Lets researchers report securely without using public issues.

## Secret scanning

The audit script greps git history for common secret-bearing filenames. Patterns it covers:

- `.env`, `.env.local`, `.env.production`
- `*.pem`, `*.key`, `*.p12`, `*.pfx`
- `id_rsa`, `id_ed25519`, `.netrc`
- `credentials.json`, `secrets.yml`, `google-services.json`
- `firebase-adminsdk*.json`

It does **not** grep file contents for high-entropy strings (API keys, tokens). For that, use:

- [`gitleaks`](https://github.com/gitleaks/gitleaks) — best-of-class entropy and pattern-based scanner.
- [`trufflehog`](https://github.com/trufflesecurity/trufflehog) — verifies live keys against the issuing service.
- GitHub's [secret scanning](https://docs.github.com/en/code-security/secret-scanning/about-secret-scanning) — automatic on public repos.

If the audit flags real secrets:

1. **Rotate the secret first.** Even if you scrub history, assume it's compromised.
2. **Then scrub history** with `git filter-repo --path <leaked-file> --invert-paths`.
3. **Force-push** to the remote (only safe pre-public, before anyone has cloned).
4. **Document the rotation** in CHANGELOG.md if it affected released artefacts.

## Branch protection

Sensible defaults for `main` on a public repo:

- ✅ Require a pull request before merging.
- ✅ Require approvals (1+ for solo / small projects, 2+ for funded).
- ✅ Dismiss stale pull request approvals when new commits are pushed.
- ✅ Require status checks to pass before merging — pin specific CI jobs (build, test, lint).
- ✅ Require branches to be up to date before merging.
- ✅ Require conversation resolution before merging.
- ✅ Do not allow force pushes.
- ✅ Do not allow deletions.
- ⚠️ "Include administrators" — flip on once you trust the process; off until then so emergencies are unblocked.

Configure via the GitHub UI (Settings → Branches → Add rule) or programmatically via `gh api`.

## Dependabot

Drop a `.github/dependabot.yml` to auto-PR dependency updates. Minimum useful config (covered by the template):

- One package-ecosystem entry per ecosystem your repo uses (`gomod`, `github-actions`, `npm`, `cargo`, `pip`).
- Weekly schedule (`daily` is noisy for most projects; `monthly` lags behind real CVEs).
- Reviewers: yourself, or the maintainer team.
- Group minor + patch updates together to reduce PR volume.

Dependabot also sends security alerts (CVEs in your dependencies) automatically once enabled in repo settings.

## Releases and tags

Before flipping public:

- Decide on a versioning scheme. **Semver** is the default (`vX.Y.Z`).
- Pre-1.0 (`v0.x.y`) means "no API stability promise" — common for early projects.
- Tag format: `v<major>.<minor>.<patch>`, no prefix beyond `v`.
- For a CLI / binary distribution, set up an automated release pipeline (GoReleaser / release-please / semantic-release / cargo-release) before announcing — manual tarballs from a maintainer's laptop are a maintenance trap.

If the repo already has tags from private development, decide whether to:
- Keep them (history reveals the project's age — usually fine).
- Reset to `v0.1.0` and start fresh (cleaner public timeline; surprise-deletes anyone who depended on prior tags privately).

## Repo metadata

Set via `gh repo edit` or the GitHub UI:

- **Description** — one sentence that answers "what is this". Searchable.
- **Homepage URL** — docs site, package registry page (pkg.go.dev, npmjs.com), or your own URL. Sets the link icon next to the repo name.
- **Topics** — 3–6 lowercase tags (`golang`, `cli`, `mcp`, `metadata`, `cel`). Drives the GitHub Topics pages. **High** discovery value.
- **Issues / Discussions** — issues on (default); discussions on if you want a Q&A space separate from issues.

Example:

```sh
gh repo edit \
  --description "Search files by content-type metadata using CEL expressions" \
  --homepage "https://pkg.go.dev/github.com/<you>/<repo>" \
  --add-topic golang --add-topic cli --add-topic mcp \
  --enable-issues --enable-discussions
```
