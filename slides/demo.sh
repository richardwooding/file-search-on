#!/usr/bin/env bash
# Demo script for the file-search-on talk.
# Each block matches a slide; run them in order, NOT in one shot.
# Suggested workflow: keep this file open in a second pane and
# copy-paste each block as the talk progresses. If you want a "type
# for me" presenter tool, doitlive or demo-magic.sh both work.

set -eu

# ---------------------------------------------------------------------------
# Demo 1, command 1 — attrs on the repo's go.mod (manifest/gomod content
# type — surfaces module name + go_version).
# ---------------------------------------------------------------------------
file-search-on attrs ~/Code/Personal/file-search-on/go.mod

# ---------------------------------------------------------------------------
# Demo 1, command 2 — top-5 Go source files by LOC across the repo.
# ---------------------------------------------------------------------------
file-search-on search -d ~/Code/Personal/file-search-on \
  'is_source && language == "go" && loc > 300' \
  --sort loc --order desc --limit 5

# ---------------------------------------------------------------------------
# Demo 1, command 3 — top-20 highest-churn source files via the
# `hot_files` repo-aware preset (#300). The preset bakes
#   is_source && is_git_tracked && git_commit_count > 0
# with a `git_commit_count desc` sort. The CEL reference to a `git_*`
# attribute auto-enables with_git via celexpr.NeedsGit — no flag needed.
# First call pays the `git log` cost (~500ms on this repo); subsequent
# calls hit the gitmeta.Pool cache and return in single-digit ms.
# Refactor / review prioritisation story.
# ---------------------------------------------------------------------------
file-search-on preset hot_files -d ~/Code/Personal/file-search-on

# ---------------------------------------------------------------------------
# Demo 2, prep — build the semantic-search corpus once (12 markdown notes):
#   ./scripts/build_semantic_corpus.sh
# Requires Ollama running locally with the nomic-embed-text model pulled:
#   ollama pull nomic-embed-text
#
# Wipe the embedding cache between rehearsals to keep the cold/warm
# story honest:
#   rm -f /tmp/semantic.db
# ---------------------------------------------------------------------------

# Demo 2, command 1 — natural-language query. The matching file
# (k8s-scheduling.md) contains none of the query words. ~1.5s cold.
file-search-on search -d ~/Demo/semantic-demo \
  --semantic-query "container orchestration deployment strategies" \
  --embedding-model nomic-embed-text \
  --similarity-threshold 0.50 --limit 5 \
  --index-path /tmp/semantic.db

# Demo 2, punchline — confirm with grep that NONE of those words appear
# in any file. Use BSD grep (default on macOS). Expected output: empty.
grep -rli 'orchestration\|container\|deployment strateg' ~/Demo/semantic-demo

# Demo 2, command 2 — second semantic query, warm (~90ms). Top hit is
# transaction-isolation.md, found by meaning not keyword.
file-search-on search -d ~/Demo/semantic-demo \
  --semantic-query "what happens when two writers update the same record" \
  --embedding-model nomic-embed-text \
  --similarity-threshold 0.50 --limit 5 \
  --index-path /tmp/semantic.db

# ---------------------------------------------------------------------------
# Demo 3, command 1 — photos by camera_make
# ---------------------------------------------------------------------------
file-search-on stats -d ~/Demo/south-africa-holiday \
  'is_image' --group-by camera_make

# ---------------------------------------------------------------------------
# Demo 3, command 2 — photos inside a Cape Town bounding box (rectangle)
# Expected hits: ~5 on the curated corpus (central CT only).
# ---------------------------------------------------------------------------
file-search-on -d ~/Demo/south-africa-holiday \
  'is_image && gps_lat > -33.96 && gps_lat < -33.7 && gps_lon > 18.3 && gps_lon < 18.7'

# ---------------------------------------------------------------------------
# Demo 3, command 3 — photos inside the Cape Peninsula polygon
# (the narrow finger pointing south from Cape Town through Cape Point).
# Polygon vertices (clockwise from NW): Atlantic Seaboard, Table Bay,
# False Bay coast, Cape Point, Cape of Good Hope. Expected hits: ~12,
# including the 7 Cape Point / Boulders Beach shots the bbox misses.
# ---------------------------------------------------------------------------
file-search-on -d ~/Demo/south-africa-holiday \
  'is_image && point_in_polygon(gps_lat, gps_lon,
       [-33.85, 18.30,  -33.85, 18.55,
        -34.15, 18.55,  -34.40, 18.50,
        -34.40, 18.32])'

# ---------------------------------------------------------------------------
# Demo 4, prep — build the OCR corpus once (synthetic JPGs with text):
#   ./scripts/build_ocr_corpus.sh
# This drops 12 images in ~/Demo/ocr-demo/. Run any time the corpus
# gets nuked; rebuilds are idempotent.
#
# Wipe the default on-disk index between rehearsals to keep the
# cold/warm story honest. The index lives at the OS cache location
# (e.g. ~/Library/Caches/file-search-on on macOS) since #243.
# ---------------------------------------------------------------------------

# Demo 4, command 1 — COLD pass: OCR runs over all 12 images. ~2.5s.
# Expected hits: 2 (error_terminal.jpg, log_entry.jpg).
# The footer line "index: 0 hits, 12 misses, 12 stored" proves OCR ran.
# No --index-path flag needed — the on-disk index defaults on now.
file-search-on search --ocr -d ~/Demo/ocr-demo \
  'is_image && body.contains("ERROR")'

# Demo 4, command 2 — WARM pass: cache hit. <50ms.
# Expected hits: 3 (receipt.jpg, invoice.jpg, printed_email.jpg — the
# email mentions "invoice 2026-0042" so it's a legit hit, not noise).
file-search-on search --ocr -d ~/Demo/ocr-demo \
  'is_image && body.matches("(?i)\b(invoice|total)\b")'

# Demo 4, command 3 — WARM pass: another sub-second query against the
# same cached body strings. Expected hits: 1 (meeting_notes.jpg).
file-search-on search --ocr -d ~/Demo/ocr-demo \
  'is_image && body.contains("Athena")'

# ---------------------------------------------------------------------------
# Demo 5 — visual similarity by perceptual hash.
# `image_similar_to` auto-enables --with-phash, so the flag is optional.
# Threshold 0.60 was tuned against the 66-photo corpus: returns the
# reference photo + ~7 visually-similar wide-frame outdoor shots.
# Cold pass ~2.5s (phash compute for all 66 images); cached for subsequent
# runs (if you reuse --index-path).
# ---------------------------------------------------------------------------
file-search-on search \
  -d ~/Demo/south-africa-holiday \
  "is_image && image_similar_to(phash, \
     '$HOME/Demo/south-africa-holiday/cape-of-good-hope_Cape_Point_Cape_Town_IMG_20180717_174658.jpg', \
     0.60)"

# ---------------------------------------------------------------------------
# Demo 6 — the MCP server. Launch in one terminal; talk to it from another.
# In Claude Desktop / Code, register this as a stdio MCP server.
# ---------------------------------------------------------------------------
# file-search-on mcp
#
# Question to paste into the agent UI:
#   "I love this Cape Point shot:
#    ~/Demo/south-africa-holiday/cape-of-good-hope_Cape_Point_Cape_Town_IMG_20180717_174658.jpg
#    Find me other photos in ~/Demo/south-africa-holiday that are
#    visually in the same style — coastal scenes, similar composition.
#    Use a loose threshold; I want scene resemblance, not byte-identical
#    duplicates."
#
# Expected agent move: a single `search` call with `with_phash: true` and an
# `image_similar_to(phash, "<cape-point-path>", ~0.60)` CEL filter, sorted
# by similarity. Same surface Demo 5 just ran on the CLI — the punchline
# is that the agent picked it from natural language, not from a prompt
# template.
#
# Why a moderate threshold: at 0.85+ (the tool description's default) you
# get near-duplicates only, which this 66-photo corpus doesn't really have.
# At 0.60 you get ~8 coastal / landscape shots that visually cluster.
# Anything below 0.50 returns most of the corpus. The "loose threshold"
# wording in the question nudges the agent away from the 0.85 default.

# ---------------------------------------------------------------------------
# Demo 7 — the live monitor dashboard.
# Assumes the MCP server from Demo 6 (or a `file-search-on watch` process)
# is still running in another terminal — both modes auto-start the
# dashboard since v0.65 (#245).
# ---------------------------------------------------------------------------

# Demo 7, command 1 — list every running file-search-on dashboard.
# Reads the shared peer-registry file at the OS cache location.
# Pretty-printed output by default; pass `-o bare` for one URL per line.
file-search-on monitors

# Demo 7, command 2 — open the first dashboard URL in the browser
# (macOS; use xdg-open on Linux, start on Windows).
file-search-on monitors -o bare | head -1 | xargs open

# Live tour while on screen (click through these tabs):
#   - Overview       — pid, working dir, MCP version, capabilities
#   - Cache          — attr / body / embedding hit-miss-stale (live SSE)
#   - Activity       — every tool call streamed as it happens
#   - Cache browse   — inspect any entry; evict / clear / warm in place
#   - Peers          — switch between concurrent file-search-on instances
#
# Optional flourish: trigger a fresh search from a third terminal so the
# Activity tab lights up live in the browser.
#   file-search-on -d ~/Demo/south-africa-holiday 'is_image' --limit 5
