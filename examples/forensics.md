# Forensic triage with file-search-on

`file-search-on` isn't a low-level forensics suite — it doesn't do disk imaging, deleted-file recovery, registry parsing, or chain-of-custody logging. What it **does** do well is the **triage / content-discovery** layer that sits between Autopsy / EnCase / X-Ways / sleuthkit (heavyweight) and `find | grep | exiftool` chains (one-off scripts).

Once you have files extracted (mounted image, exported tree, or live filesystem), file-search-on lets you ask typed CEL queries across content type, metadata, body text, and now **forensic hashes** in one call.

## What's relevant for forensics

| Capability | How |
|---|---|
| **Magic-byte content-type detection** | A `.exe` renamed to `.txt` still detects as `binary/pe` from its magic bytes. Predicates: `is_binary`, `is_office`, `is_pdf`, `is_image`, etc. |
| **EXIF GPS / camera / timestamp** | `is_image && gps_lat > 51.5` — find photos in a geo-bbox; `taken_at > timestamp("2025-01-01T00:00:00Z")` |
| **Email headers** | `is_email && body.contains("invoice")` (with `--body`); `email_to`, `email_message_id`, `sent_at` |
| **Office document author** | `is_office && author == "Jane Doe"` from Dublin Core metadata |
| **PDF metadata + body** | `is_pdf && page_count > 10 && body.matches("(?i)\\bconfidential\\b")` |
| **Full-body regex search** | Pair `--body` with `body.matches("...")` (RE2) across PDF / DOCX / EPUB / email / source / text |
| **Byte-identical duplicates** | `duplicates` subcommand — data-staging indicator, version recovery |
| **Near-duplicates (SimHash)** | `near-duplicates` — finds drafts / template copies / regenerated docs that exact-hash misses |
| **Forensic hashes (MD5 + SHA1 + SHA256)** | `--with-hashes` populates all three in one pass — NSRL / VirusTotal / threat-intel-feed interop |
| **Symlink awareness** | `is_symlink`, `is_broken_symlink`, `target_path` |
| **Source files with suspicious markers** | `is_source && body.matches("(?i)\\b(eval|exec)\\(")` |
| **Time-bucketed activity** | `stats --group-by mtime_year` — what years did this disk see writes? |
| **Binary architecture / format** | `is_binary && bitness == 64 && binary_format == "elf"` |

## Forensic hashes

The headline forensic addition. **MD5, SHA1, and SHA256** all populate in one file read via `io.MultiWriter` — the marginal cost over SHA256-alone is ~30% CPU on the hash compute itself, dwarfed by I/O.

```sh
# Compute hashes alongside the regular search
file-search-on 'is_binary' --with-hashes -d /Volumes/Evidence -o json --index-path /tmp/evidence.db

# Filter by a known-bad MD5 (single threat-intel hash)
file-search-on 'is_binary && md5 == "5d41402abc4b2a76b9719d911017c592"' --with-hashes -d /Volumes/Evidence

# SHA256 of every PDF over 1 MB
file-search-on 'is_pdf && size > 1000000' --with-hashes -d /Volumes/Evidence -o json | jq '.[] | {path, sha256}'

# Hash all images and write to a CSV-style template
file-search-on 'is_image' --with-hashes --format '{{.Path}},{{.MD5}},{{.SHA1}},{{.SHA256}}' -d /Volumes/Evidence
```

**Caching** — pair `--with-hashes` with `--index-path`. The first run reads every match in full; subsequent runs on unchanged files (validated by `(size, mtime)`) hit the cache and surface the trio for free.

**MCP** — call `search` with `compute_hashes: true`:

```json
{
  "name": "mcp__file-search-on__search",
  "arguments": {
    "expr": "is_binary && md5 == \"5d41402abc4b2a76b9719d911017c592\"",
    "dir": "/Volumes/Evidence",
    "compute_hashes": true
  }
}
```

Or `read_attributes` for a single file:

```json
{
  "name": "mcp__file-search-on__read_attributes",
  "arguments": {
    "path": "/Volumes/Evidence/suspicious.bin",
    "compute_hashes": true
  }
}
```

## Recipes

### Find recently-modified executables that don't match their extension

The classic "planted backdoor" signal. Combines magic-byte detection with mtime:

```sh
file-search-on 'is_binary && mod_time > timestamp("2026-05-01T00:00:00Z")' --with-hashes -d /Volumes/Evidence
```

(Note: a follow-up [issue #145](https://github.com/richardwooding/file-search-on/issues/145) adds `is_disguised` for explicit extension-vs-magic mismatch detection.)

### Hash every binary and compare against a known-bad list

For now, dump hashes to JSON and post-process. [Issue #146](https://github.com/richardwooding/file-search-on/issues/146) tracks adding `--hash-denylist` / `is_known_bad` as a first-class CEL predicate.

```sh
file-search-on 'is_binary' --with-hashes -d /Volumes/Evidence -o json --index-path /tmp/evidence.db \
  | jq -r '.[].md5' \
  | grep -Ff /tmp/ioc-md5s.txt
```

### Photos from a specific GPS bbox + camera

```sh
file-search-on 'is_image && point_in_polygon(gps_lat, gps_lon, [51.5,-0.2, 51.5,-0.05, 51.55,-0.05, 51.55,-0.2]) && camera_make == "Apple"' \
  --with-hashes -d /Volumes/Evidence
```

The hash trio survives a copy-with-mtime-preserved (file-search-on's cache validates on `(size, mtime)` — different mtime = different cache entry, but same hash if bytes unchanged) so you can compare hashes across two extracted volumes.

### Email triage by recipient + body content

```sh
file-search-on 'is_email && "victim@example.com" in email_to && body.contains("password")' \
  --body --with-hashes -d /Volumes/Evidence -o json
```

### Documents authored by a specific person

```sh
file-search-on 'is_office && author == "Alice Smith"' --with-hashes -d /Volumes/Evidence
```

## Time bucketing for timeline overview

```sh
# File-modification activity by year — what years was this volume active?
file-search-on stats --group-by mtime_year -d /Volumes/Evidence

# Photo activity by month
file-search-on stats 'is_image' --group-by taken_at_month -d /Volumes/Evidence

# Email volume by year
file-search-on stats 'is_email' --group-by sent_at_year -d /Volumes/Evidence
```

For timeline reconstruction beyond mtime, file-search-on now surfaces **`created_at`** (filesystem birth time / btime) and **`metadata_changed_at`** (ctime — last permission / ownership change), plus the **`is_btime_anomaly`** predicate that fires when `created_at > mod_time` (the classic "this file claims to have been modified before it was placed here" signal — restored backup, copied across volumes, planted artefact).

```sh
# Files newly created on this volume in the last 30 days
file-search-on 'created_at > timestamp("2026-04-17T00:00:00Z")' -d /Volumes/Evidence

# Anomalous files — placed here AFTER being modified elsewhere
file-search-on 'is_btime_anomaly' -d /Volumes/Evidence -o json | jq '.[] | {path, created_at, mod_time}'

# Recent permission / ownership changes — tamper-trail
file-search-on 'metadata_changed_at > timestamp("2026-05-01T00:00:00Z")' -d /Volumes/Evidence --sort metadata_changed_at --order desc --limit 50

# Birth-time bucketing — when did this volume see new files?
file-search-on stats --group-by created_at_year -d /Volumes/Evidence
file-search-on stats --group-by created_at_month -d /Volumes/Evidence
```

`created_at` populates on every modern filesystem (ext4 / APFS / NTFS / btrfs / xfs). Empty on filesystems / OSes that don't track it; the predicate correctly stays false in that case. `atime` is deliberately **not** surfaced — modern mounts use `relatime` / `noatime` and the value is unreliable for forensic use.

## Out of scope

file-search-on does **not** do:

- Raw-disk imaging, deleted-file recovery / file carving from unallocated space
- Write-blocker semantics or atime preservation (reading a file with this tool DOES update atime on filesystems mounted with default options)
- Windows registry / Plist / browser-SQLite parsing
- Chain-of-custody logging or audit trails
- NSRL hash-allowlist filtering ([tracked in #146](https://github.com/richardwooding/file-search-on/issues/146))
- Online VirusTotal queries

For those use cases, pair file-search-on with Autopsy / sleuthkit / Volatility / X-Ways for the low-level work, and use file-search-on for the typed content-discovery layer once files are extracted.
