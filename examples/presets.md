# Recipes — Named presets

Presets are pre-canned search recipes — each maps a friendly name to a vetted CEL filter plus sensible defaults for sort / limit / opt-in flags. Designed for agent UX: instead of an agent guessing the right CEL incantation, it calls `list_presets` to see what's available, then `query_preset` (MCP) / `file-search-on preset <name>` (CLI) to run one.

Issue #168 sub-feature B.

## Listing available presets

```sh
# CLI — table of name + description
file-search-on preset
```

```json
// MCP
{
  "name": "mcp__file-search-on__list_presets",
  "arguments": {}
}
```

## v1 catalog

| Preset | What it finds | Defaults |
|---|---|---|
| `recent_changes` | Files modified in the last 7 days | sort=mod_time desc, limit=50 |
| `recent_photos` | Images taken in the last 30 days | sort=taken_at desc, limit=50 |
| `old_drafts` | Markdown drafts not modified in the last 90 days | sort=mod_time asc |
| `large_files` | Files larger than 100 MB across all formats | sort=size desc, limit=20 |
| `large_binaries` | Compiled binaries larger than 100 MB | sort=size desc, limit=20 |
| `suspicious_files` | Disguised files (magic ≠ extension) OR btime anomalies | check_disguised auto-on |
| `failed_tests` | Source test files mentioning FAIL / FIXME / XXX in the body | include_body auto-on |
| `system_metadata` | OS leftovers — .DS_Store / Thumbs.db / Desktop.ini / .directory / .localized | — |

Each preset bakes a fresh timestamp at invocation time, so `recent_changes` always means "last 7 days from NOW".

### EPUB / ebook-library presets

EPUB metadata is Dublin Core only (`title` / `author` / `language`) with no page or word count, so these lean on `size` (the practical length proxy), `mod_time`, and the metadata fields.

| Preset | What it finds | Defaults |
|---|---|---|
| `large_ebooks` | The largest EPUBs (size proxies length) | sort=size desc, limit=20 |
| `recent_ebooks` | EPUBs added/changed in the last 30 days | sort=mod_time desc, limit=50 |
| `untagged_ebooks` | EPUBs missing a Dublin Core title or author — the "fix this book's metadata" list | sort=name asc |
| `non_english_ebooks` | EPUBs whose language is set and not `en` | sort=name asc |

## Running a preset

```sh
file-search-on preset recent_changes -d ~/Code
file-search-on preset recent_photos -d ~/Pictures --limit 10
file-search-on preset large_files -d / -o json | jq '.matches[] | .path + " " + (.size|tostring)'
file-search-on preset suspicious_files -d ~/Downloads -o verbose
file-search-on preset failed_tests -d ./src --limit 5
```

```json
// MCP
{
  "name": "mcp__file-search-on__query_preset",
  "arguments": {
    "name": "recent_photos",
    "dir": "/Users/me/Pictures",
    "limit": 10
  }
}
```

## Per-call overrides

The preset's CEL filter is fixed (the whole point — vetted recipe), but the walk-shape flags are user-overridable:

- `-d / --dir / dirs` — scope the walk
- `--limit / limit` — override the preset's default cap
- `--exclude / excludes` — additional prune globs
- `--respect-gitignore / respect_gitignore`
- `--follow-symlinks / follow_symlinks`
- `-o / output` (CLI only) — output format

## Common workflows

### Forensic triage

```sh
# Disguised files + btime anomalies under a downloads dir
file-search-on preset suspicious_files -d ~/Downloads -o verbose
```

Combine with `--with-hashes --hash-allowlist <NSRL.bbolt>` for the full triage workflow — see [forensics.md](./forensics.md).

### Disk-eater hunt

```sh
file-search-on preset large_files -d ~ -o json | \
  jq -r '.matches[] | "\(.size)\t\(.path)"' | \
  numfmt --field=1 --to=iec
```

### Code-review prompt

```sh
file-search-on preset failed_tests -d ./src --limit 20 -o verbose

# Equivalent CEL (in case you want to tweak the markers):
# file-search-on 'is_source && is_test_file && body.matches("FAIL|FIXME|XXX|TODO")' --body -d ./src
```

### Neglected notes

```sh
file-search-on preset old_drafts -d ~/notes -o verbose
```

### Recent-files quick-scan

```sh
file-search-on preset recent_changes -d ~ --limit 100 -o bare
```

### Tidy up

```sh
# Find OS metadata files for cleanup
file-search-on preset system_metadata -d /shared/dropbox -o bare | xargs -d '\n' rm -i
```

## Extending the catalog

Add a new entry to `internal/search/presets.go`'s `presets` slice:

```go
{
    Name:        "recent_pdfs",
    Description: "PDFs modified in the last 14 days, newest first.",
    Build: func() PresetOptions {
        cutoff := time.Now().Add(-14 * 24 * time.Hour).Format(time.RFC3339)
        return PresetOptions{
            Expr:  fmt.Sprintf(`is_pdf && mod_time > timestamp(%q)`, cutoff),
            Sort:  "mod_time",
            Order: "desc",
            Limit: 25,
        }
    },
},
```

`TestPresets_AllCompile` verifies every CEL expression compiles at build time. The CLI / MCP / docs pick up new presets automatically — no other edits required.

## Known limitations

- **Presets are search-shaped only.** They don't compose with `stats` / `find_matches` / `find_duplicates`. If a workflow needs aggregation or line-level grep, write the equivalent CEL directly.
- **Time-relative presets bake `time.Now()` at invocation.** Long-lived MCP server sessions get a fresh cutoff per call — there's no "session-relative" anchoring.
- **`suspicious_files` doesn't load a hash allowlist by default.** Adding `is_known_good` would require the user to supply `--hash-allowlist` — out of scope for v1 to keep presets self-contained. Pair with `--hash-allowlist <file>` if you have an NSRL / corp allowlist available.
