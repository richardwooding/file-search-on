# Hash allowlists and denylists

`file-search-on` integrates hash-set lookups directly into the CEL query layer. Two new boolean predicates surface membership in external hash lists:

- **`is_known_good`** — fires when the file's MD5 / SHA1 / SHA256 appears in the loaded **allowlist** (`--hash-allowlist` / `hash_allowlist_path`).
- **`is_known_bad`** — fires when the file's hash appears in the loaded **denylist** (`--hash-denylist` / `hash_denylist_path`).

Membership in ANY of the three algorithms is sufficient — NSRL ships MD5 + SHA1; threat-intel feeds tend to use MD5 or SHA1; modern tools index by SHA256. Whichever hash the list provides will match.

## When to use which

| Use case | Predicate | Source |
|---|---|---|
| Forensic triage on a disk image | `!is_known_good` | NSRL Reference Data Set (NIST) — cuts review surface by 80-95% |
| IOC scan against a threat-intel drop | `is_known_bad` | Vendor IOC feed, internal known-malware list |
| Corporate allowlist | `!is_known_good` | Internal "every binary we've shipped" hash dump |
| Investigation crosscheck | Either | Hashes recovered from a previous investigation |

## File formats

Both flags accept two formats transparently:

### 1. Newline-separated text

The simplest format. One hex-encoded hash per line; algorithm auto-detected by length (32=MD5, 40=SHA1, 64=SHA256). Mixed-algorithm files are fine.

```text
# threat-intel feed export 2026-05-18
# Comments and blank lines are ignored.

5d41402abc4b2a76b9719d911017c592
aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

Cheap to author by hand. Loads into memory at start of every search; fine up to a few hundred thousand entries.

### 2. Pre-built bbolt (`.hashset`)

Required for NSRL-scale (~50M hashes — in-memory would use multi-GB of RAM). The bbolt format stores hashes as raw bytes (half the size of hex) with O(log N) lookup; loads in milliseconds regardless of size.

Build with the `hash-set build` subcommand:

```sh
# NSRL Modern RDS CSV → bbolt (header auto-detected)
file-search-on hash-set build NSRLFile.txt --out nsrl.hashset
# built nsrl.hashset in 5m32s
#   md5:    49234567
#   sha1:   49234567
#   sha256: 0

# Plain text list → bbolt
file-search-on hash-set build allowlist.txt --out allow.hashset

# stdin → bbolt
gunzip -c nsrl.txt.gz | file-search-on hash-set build - --out nsrl.hashset

# Force a specific format (auto-detect normally suffices)
file-search-on hash-set build NSRLFile.txt --format nsrl --out nsrl.hashset
```

Inspect a hashset:

```sh
file-search-on hash-set info nsrl.hashset
# nsrl.hashset
#   md5:    49234567
#   sha1:   49234567
#   sha256: 0
#   total:  98469134
```

## Recipes

### NSRL forensic triage — cut review surface by 80-95%

The classic application. Start from a mounted forensic image; surface only the files NOT in the NSRL allowlist:

```sh
# Every file NOT in NSRL — the review surface
file-search-on '!is_known_good' --hash-allowlist nsrl.hashset -d /Volumes/Evidence

# Narrow to executables not in NSRL — the high-signal subset
file-search-on '!is_known_good && is_binary' --hash-allowlist nsrl.hashset -d /Volumes/Evidence -o json

# Same, recently modified — even higher signal
file-search-on '!is_known_good && is_binary && mod_time > timestamp("2026-04-01T00:00:00Z")' --hash-allowlist nsrl.hashset -d /Volumes/Evidence
```

### Threat-intel IOC scan

```sh
# Any file matching an IOC drop
file-search-on 'is_known_bad' --hash-denylist /tmp/ioc-md5s.txt -d /Volumes/Evidence

# Surface paths + hash for triage upload
file-search-on 'is_known_bad' --hash-denylist /tmp/ioc.txt \
  --format '{{.Path}}\t{{.MD5}}\t{{.SHA1}}\t{{.SHA256}}' \
  -d /Volumes/Evidence
```

### Cross-reference allow + deny

A file that's both in your allowlist and somebody's threat-intel feed is genuinely interesting (hash collision attempt, supply-chain compromise, repo poisoning). Pair both lists:

```sh
file-search-on 'is_known_good && is_known_bad' \
  --hash-allowlist corp-allow.hashset \
  --hash-denylist /tmp/iocs.txt \
  -d /Volumes/Evidence
```

### Combined with disguise detection (`#145`)

Stack with the disguise predicates: a file that's marked `is_known_good` BUT whose magic disagrees with its extension is suspicious — either the allowlist is poisoned or the file got tampered after listing:

```sh
file-search-on 'is_known_good && is_disguised' \
  --hash-allowlist corp-allow.hashset \
  --check-disguised \
  -d /Volumes/Evidence
```

### Combined with btime anomaly (`#144`)

```sh
# Recently planted files not in any known-good list
file-search-on '!is_known_good && is_btime_anomaly' \
  --hash-allowlist nsrl.hashset \
  -d /Volumes/Evidence
```

## MCP usage

```json
{
  "name": "mcp__file-search-on__search",
  "arguments": {
    "expr": "!is_known_good && is_binary",
    "dir": "/Volumes/Evidence",
    "hash_allowlist_path": "/var/lib/forensic/nsrl.hashset"
  }
}
```

The MCP server force-enables `compute_hashes` when `hash_allowlist_path` or `hash_denylist_path` is set, same as the CLI.

## NSRL details

The NSRL Reference Data Set ships in three flavours from NIST:

- **Modern RDS** — current. Distributed as a SQLite database AND as ZIP archives containing `NSRLFile.txt` (a quoted-CSV with `"SHA-1","MD5","CRC32","FileName","FileSize","ProductCode","OpSystemCode","SpecialCode"`).
- **Legacy RDS** — pre-2018 format. Similar CSV shape.
- **Minimal RDS** — smallest distribution; SHA-1 only.

`file-search-on hash-set build` reads `NSRLFile.txt` directly (CSV path). For the SQLite distribution, extract the hashes with `sqlite3` first:

```sh
sqlite3 RDS.db "SELECT sha1, md5 FROM FILE" -separator , > nsrl.csv
# Then build the same way
file-search-on hash-set build nsrl.csv --format nsrl --out nsrl.hashset
```

## Performance notes

- Loading a text hashset: O(N) at startup, ~50 MB/s on typical hardware. A 10MB hashset loads in ~200ms.
- Loading a bbolt hashset: O(1) — file is mmap'd, no parse. Cold-open ~10ms regardless of size.
- Lookup during search: O(1) for text-backed, O(log N) for bbolt. Either is negligible compared to hash computation (which dominates `--with-hashes` walks).
- Building NSRL bbolt: ~5-10 minutes for the full Modern RDS on consumer hardware. Build once, reuse for years.

## Out of scope (for now)

- **Online VirusTotal queries** — privacy + rate limits; use offline IOC drops instead.
- **Per-product NSRL metadata** — the build process discards `FileName`, `ProductCode`, etc. Membership is binary yes/no.
- **Bloom-filter mode** — bbolt's O(log N) is fast enough for any practical hashset size; bloom filters add false positives without measurable speedup.
- **Hash-set diffing** — compute the symmetric difference between two hashsets. File a follow-up if you need it.
