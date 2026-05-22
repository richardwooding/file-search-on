# Timeouts and partial results

Walks over large trees can take a while. Both the CLI and the MCP server expose timeouts so callers don't wait forever, and **both surface partial results when a deadline fires**: whatever was collected before cancellation is still returned, with a clear "this was incomplete" signal.

## CLI

```sh
file-search-on 'is_pdf' -d ~/Documents --timeout 30s
file-search-on 'is_video && duration > 1800' -d ~/Movies --timeout 2m -o json
file-search-on 'is_audio' -d ~/Music --timeout 1m --index-path ~/.cache/fso/music.db
```

`--timeout` accepts any Go duration (`30s`, `2m`, `500ms`, `1h`). Default is **no timeout** — back-compatible.

When the deadline fires:

- The partial result set is sorted and printed to stdout.
- The footer `<N> file(s) found` reflects the partial count.
- A warning lands on stderr: `search timed out after 30s; results above may be incomplete`.
- **Agent-actionable suggestions** follow on stderr — heuristics over the walk options point at concrete next steps (issue #168 sub-feature C):

  ```
  Suggestions:
    • Walk hit the timeout at 0.0s. Pass --timeout 1s (≈2x current) for a longer run.
    • No --exclude or --respect-gitignore is set. Common build / cache dirs (node_modules, .git, target, __pycache__, vendor) may dominate the walk; pass --exclude or --respect-gitignore to prune them.
    • The CEL filter is empty or 'true' — every file is walked. Add a type predicate (is_pdf, is_image, is_source, …) to limit candidates.
  ```

  Five heuristics fire on cancellation: bump-timeout, hot-directory (longest common prefix of matches — MCP only; the CLI doesn't carry the match list at the cancellation site), `--body` warning (body reads dominate naive walks), missing-prunes (no excludes / gitignore), lax-filter (empty CEL or `true`).
- The process **exits 124** (matches GNU `timeout(1)` convention).

Other exit codes:

| Code | Meaning |
| --- | --- |
| 0 | Walk completed (whether or not any files matched). |
| 1 | Hard error: bad CEL expression, missing directory, parse failure, etc. |
| 124 | `--timeout` fired. |
| 130 | Ctrl-C / SIGINT. |

Shell-pipeline pattern: pipe results through `jq` / `xargs` and check the exit code separately to handle partial results explicitly:

```sh
results=$(file-search-on 'is_pdf' -d ~/Documents --timeout 10s -o json)
case $? in
  0)   echo "$results" | jq '.path' ;;
  124) echo "$results" | jq '.path' ; echo "(partial — timed out)" ;;
  *)   echo "search failed" >&2 ; exit 1 ;;
esac
```

## MCP

Every tool call is bounded by a server-default timeout, set when the MCP server starts:

```sh
file-search-on mcp                          # default: 60 seconds per call
file-search-on mcp --timeout 2m             # raise the default for all calls
file-search-on mcp --timeout 0              # disable the default (NOT RECOMMENDED — see below)
```

The `search` tool also accepts `timeout_seconds` on input to override per-call:

```json
{
  "expr": "is_image && iso > 1600",
  "dir": "/Users/me/Pictures",
  "timeout_seconds": 10
}
```

| `timeout_seconds` value | Effect |
| --- | --- |
| omitted | Use the server default. |
| positive number | Override; deadline fires after that many seconds. |
| `0` | No timeout for this call (parent context still applies). |

On expiry the search tool **does not return an error** — it returns the partial match set with these fields populated:

```json
{
  "matches": [ /* whatever was collected */ ],
  "count": 47,
  "cancelled": true,
  "cancellation_reason": "timeout",
  "elapsed_seconds": 10.003,
  "suggestions": [
    "Walk hit the timeout at 10.0s. Pass --timeout 21s (≈2x current) for a longer run.",
    "All 47 matches live under /Users/me/Documents. Consider narrowing with -d /Users/me/Documents (or --exclude 'Documents' for the inverse).",
    "No --exclude or --respect-gitignore is set. Common build / cache dirs (node_modules, .git, target, __pycache__, vendor) may dominate the walk; pass --exclude or --respect-gitignore to prune them."
  ]
}
```

`cancellation_reason` is one of:

- `"timeout"` — our own deadline fired.
- `"client_cancel"` — the parent context was cancelled (transport closed, MCP client gave up, server shutting down, …).

`suggestions` (issue #168 sub-feature C) carries agent-actionable hints generated from the observed walk state. Five heuristics:

- **Bump timeout** (`timeout` reason only) — concrete `--timeout` value at ≈2× current elapsed.
- **Hot directory** — when ≥2 matches share a common parent directory, suggest `-d <dir>` to scope OR `--exclude <basename>` to prune.
- **`--body` warning** — body reads dominate naive walks; suggest a tighter type predicate.
- **Missing prunes** — fires when neither `excludes` nor `respect_gitignore` is set.
- **Lax filter** — fires when `expr` is empty or `"true"`.

The same five suggestions surface on `stats` (without hot-directory; the histogram itself answers "what's in this tree?") and on `find_matches`.

Always inspect `cancelled` before treating the result as exhaustive. A common pattern for an agent:

1. Issue a `search` with a tight timeout.
2. If `cancelled` is true, read `suggestions[]` — pick the most relevant hint, apply it, retry.
3. If `cancelled` is true and the partial set is too small to draw conclusions, retry with a larger `timeout_seconds`.
4. If `cancelled` is false, the result is the full set.

`read_attributes` is bounded by the same server default but **returns an error on cancellation** — single-file extraction has no partial-result semantics.

## Why the default is 60s for MCP

MCP clients (Claude Desktop, Claude Code, IDE plugins) have their own read deadlines. If the server walks for several minutes, the client times out at the transport layer, the agent loses both the data and the connection, and you typically have to restart the conversation.

A 60-second per-call default is comfortably under the read deadline of every common MCP client we know of, and is enough time for non-trivial walks of moderately large trees on warm caches. If you routinely scan multi-hundred-thousand-file trees, raise the default at startup (`--timeout 5m`) and let agents tighten it per-call when they want quick exploratory results.

`--timeout 0` disables the default, which means a single call can wedge the server until the underlying MCP client gives up. Avoid unless you have specific reasons.

## Why the default is "no timeout" for the CLI

The CLI is typically run interactively from a shell. If the user wants to give up, they hit Ctrl-C — there's a human in the loop. Adding a default deadline would change muscle-memory for shell users without good reason. `--timeout` is opt-in for cron jobs, CI pipelines, and shell scripts that need a hard ceiling.

## Cancellation propagation

The walker, the per-file content-type parsers, the CEL evaluator, and the bbolt index writer all honour `context.Done()` — when the deadline fires, an in-flight file finishes its current scan-loop iteration and exits cleanly. Partial results that already landed in the channel are surfaced; in-flight work is dropped.

The hand-rolled binary parsers (MP4 / MKV / AVI / audio-MP4 / TAR / ZIP / Mach-O / PDF XMP) check `ctx.Err()` inside their inner `for` loops AND inside the shared box/EBML walkers, so a multi-GB Xcode `.app`'s embedded video mid-parse surrenders to a cancelled context within one box's / entry's / EBML element's worth of work — bounded ~milliseconds, not file-EOF. Before file-search-on v0.27.x this only worked at the walker level, so a tight 30s timeout against `/Applications` could blow past its budget by minutes while workers finished their in-flight Mach-O / video parses.

## The fast path for stats

When `stats` (CLI subcommand or MCP tool) is called with the default `group_by="content_type"` (also `ext`, `dir`, `mtime_year/month/day`) AND an empty or `"true"` filter expression, the walker skips the expensive per-format `ContentType.Attributes()` parse. Only `registry.Detect` (extension + magic-byte sniff) and `setTypeFlags` run per file. This cuts `/Applications`-scale stats from minutes (parsing 30K files of binary / image / archive content) to under two seconds. Any attribute-derived `group_by` (`language` / `camera_make` / etc.) or non-trivial expr falls back to the full parse.

Practical implications:

- Cancelling at any time is safe — no corrupted index, no half-written files, no leaked goroutines.
- A file that was being parsed when the deadline fired won't be in the result set even if some partial attributes were extracted; we don't surface "half-extracted" attributes.
- Repeated calls after a partial run still benefit from the index: files that **completed** before the deadline are cached; files that were mid-scan are not.
- The fast-path stats skips the index entirely (Lookup and Put both bypassed) — an entry with empty Extra would poison the cache for later calls that DO want attributes.
