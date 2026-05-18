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

### Find disguised files — extension lies about content

The classic "planted backdoor" / "evasion" signal. `--check-disguised` runs both the name-based and magic-byte detection passes and surfaces three new attributes:

- `magic_content_type` — what the file's first 512 bytes look like under magic-byte sniffing alone
- `extension_content_type` — what the extension implies
- `is_disguised` — fires when both are non-empty AND disagree

```sh
# Disguised binaries — extension claims something benign but bytes say binary
file-search-on 'is_disguised && magic_content_type.startsWith("binary/")' --check-disguised -d /Volumes/Evidence -o json

# Disguised executables planted recently
file-search-on 'is_disguised && is_binary && mod_time > timestamp("2026-05-01T00:00:00Z")' --check-disguised --with-hashes -d /Volumes/Evidence

# Any extension-vs-bytes mismatch, sorted by recency
file-search-on 'is_disguised' --check-disguised --sort mod_time --order desc -d /Volumes/Evidence
```

**Filter tip**: a bare `is_disguised` flag also fires on legitimate name-vs-magic divergences (e.g. `package.json` matches `manifest/node` by name and `json` by magic — both are "JSON in content", which is fine). Pair with type predicates like `is_disguised && is_binary` for forensic-grade signal that excludes the manifest noise.

### Hash allowlist / denylist — `is_known_good` / `is_known_bad`

Compare every walked file's hashes against an external list (NSRL allowlist, VirusTotal IOC feed, internal known-malware drop) in one CEL query:

```sh
# NSRL: cut review surface to executables NOT in the known-good list
file-search-on '!is_known_good && is_binary' --hash-allowlist nsrl.hashset -d /Volumes/Evidence

# Threat-intel feed: surface every file whose hash matches a known-bad IOC
file-search-on 'is_known_bad' --hash-denylist /tmp/ioc-md5s.txt -d /Volumes/Evidence -o json

# Combined: known-bad files + a hash for triage upload
file-search-on 'is_known_bad' --hash-denylist /tmp/ioc.txt --format '{{.Path}}\t{{.MD5}}\t{{.SHA256}}' -d /Volumes/Evidence
```

The flag accepts two formats:

- **Newline-separated hex** (mixed md5/sha1/sha256 auto-detected by length, `#` comments allowed). Cheap to author by hand, fine for lists up to a few hundred thousand hashes.
- **Pre-built bbolt** (`.hashset`). Required for NSRL-scale (~50M hashes). Build with `file-search-on hash-set build`:

```sh
# NSRL Modern RDS CSV → bbolt (typically ~5-10 minutes on the full set)
file-search-on hash-set build NSRLFile.txt --out nsrl.hashset

# Text-list → bbolt (idempotent; rebuild any time)
file-search-on hash-set build allowlist.txt --out allow.hashset

# Sanity-check counts
file-search-on hash-set info nsrl.hashset
# nsrl.hashset
#   md5:    49234567
#   sha1:   49234567
#   sha256: 0
#   total:  98469134
```

Both `--hash-allowlist` and `--hash-denylist` force `--with-hashes` on transparently — membership lookup needs the per-file hash trio. Membership is NOT cached in the attribute index (it depends on the set, not the file), so swapping lists is free.

See [`examples/hashsets.md`](./hashsets.md) for the full hashset cookbook.

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
- Online VirusTotal queries

For those use cases, pair file-search-on with Autopsy / sleuthkit / Volatility / X-Ways for the low-level work, and use file-search-on for the typed content-discovery layer once files are extracted.
