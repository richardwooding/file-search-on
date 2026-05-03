---
name: cut-release
description: Cuts a tag-driven release of file-search-on (GoReleaser v2 + ko + Homebrew tap), watches the GitHub Actions Release workflow, and verifies the three publish targets — GitHub Release archives, ghcr.io OCI image, and the Homebrew tap commit at richardwooding/homebrew-tap — then surfaces the rollback procedure when something goes wrong. Use when the user asks to cut, tag, ship, publish, release, or roll back a version of file-search-on, including phrases like "release v0.3.0" or "the cask is broken, undo the release".
---

# Cut Release

A release publishes to three independent targets in one shot. Pushing a tag matching `v*` to `origin` triggers `.github/workflows/release.yml`, which runs **GoReleaser v2** and produces:

1. **GitHub Release** — six archives (linux/darwin/windows × amd64/arm64) + `checksums.txt`.
2. **OCI image** — `ghcr.io/richardwooding/file-search-on:<version>` (and `:latest` for non-prerelease).
3. **Homebrew cask** — auto-commit on `richardwooding/homebrew-tap`, installable via `brew install richardwooding/tap/file-search-on`.

This skill encodes the pre-flight, the tag-and-push, the verification, and the rollback. Tag pushes are public and hard to reverse — confirm version with the user before pushing if there's any ambiguity.

## Quick start

```sh
# 1. Pre-flight (all of these must be clean)
git checkout main
git pull --ff-only origin main
git status                          # clean working tree
gh run list --branch main --limit 1 # latest CI run on main is green
git tag --sort=-v:refname | head -1 # current latest tag
goreleaser check                    # validates .goreleaser.yaml

# 2. Pick the new version using semver
#    feat: → minor bump (v0.X.0)
#    fix:/chore: → patch bump (v0.X.Y+1)
#    breaking change → major bump (vX+1.0.0) — until 1.0.0, breaking ⇒ minor

# 3. Tag and push
git tag -a v0.X.Y -m "v0.X.Y — <one-line summary>"
git push origin v0.X.Y

# 4. Watch the workflow
gh run watch "$(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status

# 5. Verify all three publish targets
bash .claude/skills/cut-release/scripts/verify_release.sh v0.X.Y
```

## Pre-flight checklist (in order)

Skip any step at your peril — tag pushes are not free to undo.

- [ ] On `main`, fast-forwarded to `origin/main`, working tree clean (`git status`).
- [ ] `gh run list --branch main --limit 1` shows the latest CI run as `success`.
- [ ] `goreleaser check` validates `.goreleaser.yaml` with no errors.
- [ ] `git tag --sort=-v:refname | head -1` to read the current latest tag.
- [ ] Pick the new version from semver: feat → minor, fix/chore → patch, breaking → minor (pre-1.0).
- [ ] If anything other than feat/fix/chore is in the diff since the last tag (`git log <last-tag>..HEAD --oneline`), pause and confirm the bump kind with the user.

Optional but recommended for risky releases:

```sh
goreleaser release --snapshot --clean --skip=publish --skip=ko
```

This builds `dist/` locally without pushing. `--skip=ko` avoids the OCI image step, which fails locally without Docker. `dist/` is gitignored.

## Tag and push

```sh
git tag -a v0.X.Y -m "v0.X.Y — <one-line summary>"
git push origin v0.X.Y
```

Use `git tag -a` (annotated), not lightweight tags. The release workflow does not care, but the annotation lands in `git show v0.X.Y` for future reference.

## Watch the workflow

```sh
gh run watch <run-id> --exit-status
```

To find the run-id of the just-triggered release run:

```sh
gh run list --workflow=release.yml --limit 1 --json databaseId,status -q '.[0]'
```

Typical duration: 2–3 minutes. If it fails, the tag is already pushed — see the rollback section.

## Verify

**Run** `bash .claude/skills/cut-release/scripts/verify_release.sh v0.X.Y` from the repo root — checks the GitHub Release has the six expected archives + checksums.txt, the Homebrew tap got an auto-commit referencing the version, and (if Docker is installed) the OCI image manifest is published. Exits non-zero on any failure.

If a target fails verification, jump to rollback before the user reports it.

## Rollback

Short version, in order. Detailed commands in [references/rollback.md](references/rollback.md).

1. **Delete the tag remotely and locally.**
2. **Delete the GitHub Release** in the UI or via `gh release delete`.
3. **Untag the ghcr.io image** (`gh api -X DELETE` against the package version).
4. **Revert the Homebrew tap commit** in the separate `richardwooding/homebrew-tap` repo.

If the change shipped is non-destructive (binaries usable, image works, cask resolves), it is **almost always cheaper to cut `vX.Y.Z+1` with the fix** than to roll back. Roll back only when something is broken enough that users shouldn't install it.

## Scripts

- **Run** `bash scripts/verify_release.sh <version>` — verifies all three publish targets for the given version (e.g. `v0.2.0`). Exits non-zero on any failure. Requires `gh`; `docker` is optional (skipped with a warning if missing).

## References

- [references/rollback.md](references/rollback.md) — exact commands for unwinding a botched release across all three targets.

## Conventions

- **Tag format is `v<major>.<minor>.<patch>`.** No prefix beyond `v`. The release workflow is gated on `tags: ['v*']`.
- **Annotated tags only** (`git tag -a`). The annotation is a one-line summary of the release.
- **Never force-push a tag.** If a tag was pushed wrong, delete it and cut the next version. Force-pushing tags can confuse caches (Homebrew, ghcr) and is hard to recover from.
- **Pre-1.0, breaking changes bump the minor.** This is the SemVer convention for `0.x.y` — major stays at 0 until the project commits to API stability.
- **Don't tag from a feature branch.** Always from `main`, after the PRs that contributed to the release have been merged.
