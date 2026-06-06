#!/usr/bin/env bash
# Stage a Reddit post for r/golang on the macOS clipboard.
#
# Reddit's markdown editor accepts markdown directly, so no format conversion
# is needed. This script splits a `title:` (and optional `flair:`) out of YAML
# frontmatter and copies just the body bytes to the clipboard via pbcopy.
#
# Input format:
#
#   ---
#   title: "Show & Tell: <project> — <description>"
#   flair: Show & Tell
#   ---
#
#   <markdown body>
#
# Output:
#   - Body markdown (everything after the closing `---`) → pbcopy clipboard.
#   - Title and flair → printed to stderr for the user to read off.
#
# Usage:
#   bash scripts/copy.sh /path/to/post.md
#
# Exit codes:
#   0  clipboard updated
#   1  input file missing or argument absent

set -euo pipefail

INPUT="${1:?usage: copy.sh <path-to-markdown>}"
[[ -f "$INPUT" ]] || { echo "ERROR: $INPUT not found" >&2; exit 1; }

# Read frontmatter values with a small awk state machine. State `c` counts
# `---` separators encountered: c==1 means inside frontmatter, c>=2 means body.
title=$(awk '/^---$/{c++; next} c==1 && /^title:/{sub(/^title:[[:space:]]*/,""); gsub(/^"|"$/,""); print; exit}' "$INPUT")
flair=$(awk '/^---$/{c++; next} c==1 && /^flair:/{sub(/^flair:[[:space:]]*/,""); gsub(/^"|"$/,""); print; exit}' "$INPUT")

# Stream body to pbcopy. If the file has no frontmatter (no `---` line at all),
# the c>=2 guard never fires and the clipboard ends up empty — keep the input
# format honest, but warn rather than silently producing an empty clipboard.
body=$(awk 'BEGIN{c=0} /^---$/{c++; next} c>=2{print}' "$INPUT")
if [[ -z "$body" ]]; then
  echo "WARNING: no body found after frontmatter. Did you include the closing '---'?" >&2
fi
printf '%s' "$body" | pbcopy

echo "Body copied to clipboard. Paste into Reddit's markdown editor." >&2
[[ -n "$title" ]] && echo "Title: $title" >&2
[[ -n "$flair" ]] && echo "Flair: $flair" >&2
