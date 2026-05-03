#!/usr/bin/env bash
# Verify a file-search-on release across all three publish targets.
#
# Usage: verify_release.sh <version>          (e.g. v0.2.0)
#
# Checks:
#   1. GitHub Release for <version> has the six expected archives + checksums.txt.
#   2. Homebrew tap (richardwooding/homebrew-tap) has a commit referencing <version>.
#   3. (Optional) The ghcr.io OCI image manifest is published. Requires docker; warns otherwise.
#
# Exits 0 if every required check passes. Exits 1 on any required failure.
# `gh` must be installed and authenticated.

set -u

readonly OWNER="richardwooding"
readonly REPO="file-search-on"
readonly TAP_REPO="richardwooding/homebrew-tap"
readonly IMAGE="ghcr.io/${OWNER}/${REPO}"

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi
readonly VERSION="$1"
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.+)?$ ]]; then
  echo "ERROR: version must look like v0.2.0 (got: $VERSION)" >&2
  exit 2
fi
readonly NUMERIC="${VERSION#v}"

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: gh CLI not installed" >&2
  exit 1
fi

errors=0
warns=0

check() {  # check <label> <command...>
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    printf "  \xe2\x9c\x93 %s\n" "$label"
    return 0
  fi
  printf "  \xe2\x9c\x97 %s\n" "$label"
  errors=$((errors + 1))
  return 1
}

warn_only() {  # warn_only <label>
  printf "  \xe2\x9a\xa0  %s\n" "$1"
  warns=$((warns + 1))
}

echo "Verifying $VERSION..."

# 1. GitHub Release
echo "GitHub Release"
release_assets="$(gh release view "$VERSION" --repo "${OWNER}/${REPO}" --json assets -q '[.assets[].name] | join("\n")' 2>/dev/null || true)"
if [[ -z "$release_assets" ]]; then
  echo "  \xe2\x9c\x97 release not found"
  errors=$((errors + 1))
else
  expected=(
    "checksums.txt"
    "${REPO}_${NUMERIC}_darwin_amd64.tar.gz"
    "${REPO}_${NUMERIC}_darwin_arm64.tar.gz"
    "${REPO}_${NUMERIC}_linux_amd64.tar.gz"
    "${REPO}_${NUMERIC}_linux_arm64.tar.gz"
    "${REPO}_${NUMERIC}_windows_amd64.zip"
    "${REPO}_${NUMERIC}_windows_arm64.zip"
  )
  for asset in "${expected[@]}"; do
    if grep -Fxq "$asset" <<<"$release_assets"; then
      printf "  \xe2\x9c\x93 %s\n" "$asset"
    else
      printf "  \xe2\x9c\x97 missing: %s\n" "$asset"
      errors=$((errors + 1))
    fi
  done
fi

# 2. Homebrew tap commit
echo "Homebrew tap (${TAP_REPO})"
top_commit="$(gh api "repos/${TAP_REPO}/commits" --jq '.[0].commit.message' 2>/dev/null || true)"
if [[ -z "$top_commit" ]]; then
  printf "  \xe2\x9c\x97 could not read tap commits (private? auth?)\n"
  errors=$((errors + 1))
elif grep -qF "$VERSION" <<<"$top_commit"; then
  printf "  \xe2\x9c\x93 top commit references %s: %s\n" "$VERSION" "$(head -1 <<<"$top_commit")"
else
  printf "  \xe2\x9c\x97 top commit does NOT reference %s: %s\n" "$VERSION" "$(head -1 <<<"$top_commit")"
  errors=$((errors + 1))
fi

# 3. OCI image (optional)
echo "OCI image (${IMAGE}:${NUMERIC})"
if ! command -v docker >/dev/null 2>&1; then
  warn_only "docker not installed; skipping image manifest check"
elif docker manifest inspect "${IMAGE}:${NUMERIC}" >/dev/null 2>&1; then
  printf "  \xe2\x9c\x93 manifest published\n"
else
  printf "  \xe2\x9c\x97 manifest not reachable for %s:%s\n" "$IMAGE" "$NUMERIC"
  errors=$((errors + 1))
fi

echo
if [[ $errors -eq 0 ]]; then
  echo "OK: $VERSION verified ($warns warning(s))"
  exit 0
fi
echo "FAIL: $errors error(s), $warns warning(s)"
exit 1
