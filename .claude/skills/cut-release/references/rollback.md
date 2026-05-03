# Release rollback

Exact commands for unwinding a botched release. Read this before doing anything destructive — and consider whether cutting `vX.Y.Z+1` instead is the cheaper move.

## When to roll back vs. roll forward

**Roll forward (cut the next version):** the binaries / image / cask exist and are *usable*, but ship the wrong code. Users who already installed are fine; the next install picks up the fix. This is the default for ~90% of botched releases.

**Roll back:** something is broken enough that any user who installs is harmed — segfault on startup, broken `--help`, missing critical files, security regression. Now you actually need to remove the artifacts.

A release that fails *during* the workflow (red GitHub Actions run) often only created some artifacts. Check each target before deciding what to clean up.

## The four cleanup targets

In order — do them top-down, since downstream caches reference upstream artifacts.

### 1. Delete the git tag

```sh
# Local
git tag -d v0.X.Y

# Remote — this is the one users see
git push origin :refs/tags/v0.X.Y
```

After this, the GitHub Release UI will still show the release (it's a separate object). The Homebrew tap and ghcr image still exist too — keep going.

### 2. Delete the GitHub Release

```sh
gh release delete v0.X.Y --repo richardwooding/file-search-on --yes
```

This removes the archives and the release page. Users who downloaded the tarball already have it; you can't recall those.

### 3. Untag the ghcr.io image

The image lives at `ghcr.io/richardwooding/file-search-on:<numeric-version>` (e.g. `0.2.0`, no `v`). Delete the package version via the GitHub API:

```sh
# List versions to find the version-id
gh api -X GET /user/packages/container/file-search-on/versions \
  --jq '.[] | {id, name, tags: .metadata.container.tags}'

# Delete the specific version-id (replace VERSION_ID)
gh api -X DELETE /user/packages/container/file-search-on/versions/VERSION_ID
```

If the release was the most recent and tagged `:latest`, the new most-recent version on the registry will inherit `:latest` once the tag is gone — verify with `docker manifest inspect ghcr.io/richardwooding/file-search-on:latest` if Docker is installed.

### 4. Revert the Homebrew tap commit

The tap is a separate repo: `richardwooding/homebrew-tap`. The release workflow auto-commits a cask update there using the `HOMEBREW_TAP_GITHUB_TOKEN` secret. To revert:

```sh
# Clone if needed
git clone https://github.com/richardwooding/homebrew-tap.git
cd homebrew-tap

# Find the auto-commit (usually most recent, message: "Brew cask update for file-search-on v0.X.Y")
git log --oneline -5

# Revert it (creates a new commit; do not force-push)
git revert <commit-sha>
git push origin main
```

Revert rather than force-push — keeps history honest and avoids breaking anyone with a checkout. After the revert lands, `brew install richardwooding/tap/file-search-on` resolves to the previous version again.

## After rollback

Run the verifier on the *previous* good version to confirm the world is consistent:

```sh
bash .claude/skills/cut-release/scripts/verify_release.sh v0.X.Y-1
```

It should still pass — none of the previous version's artifacts were touched.

## Failure modes during rollback

- **`gh release delete` succeeds but the tag won't push-delete** — usually a stale local tag. Re-run `git fetch --prune --prune-tags origin` and retry.
- **ghcr.io API call returns 404** — the package may not exist (workflow failed early) or you don't have admin scope on the package. Visit the package settings page in the GitHub UI as a fallback.
- **Tap revert blocked by branch protection** — open a PR against the tap with the revert commit. The tap is small enough that a human review is fine.

## Cheaper alternative

```sh
git tag -a v0.X.Y+1 -m "v0.X.Y+1 — fix <regression>"
git push origin v0.X.Y+1
```

If the broken release shipped non-destructive code, this is one tag-push and a 3-minute workflow run, versus four cleanup steps across three repos and the public registry. The downside: the broken version still exists in the user-facing release list. That's almost always an acceptable cost.
