# Recipes — Near-duplicate detection

The `near-duplicates` subcommand (and the `find_near_duplicates` MCP tool) find files whose **bodies** are similar but not byte-identical. Complements [`duplicates`](./duplicates.md): `duplicates` is sha256-keyed (exact match), `near-duplicates` is SimHash-keyed (fuzzy match).

**Algorithm**: 64-bit Charikar SimHash over tokenised body text. Token = lowercased Unicode letter/digit runs, len ≥ 2. Hash each token via FNV-1a-64, accumulate per-bit (+1 for set, -1 for unset), final fingerprint bit i = 1 iff the per-bit sum > 0. Pairwise Hamming distance + union-find groups files within the threshold. Reference: [Charikar 2002](https://www.cs.princeton.edu/courses/archive/spring04/cos598B/bib/CharikarEstim.pdf).

## When to use which

| Question | Tool |
|---|---|
| "Find every byte-identical copy of this file" | [`duplicates`](./duplicates.md) |
| "Find every version of this document with minor edits" | `near-duplicates` |
| "Was this README copied from another README and slightly modified?" | `near-duplicates --threshold 0.85` |
| "Find typo / whitespace edits only" | `near-duplicates --threshold 0.95` |
| "Find documents covering substantially the same topic" | `near-duplicates --threshold 0.75` |

## Basic usage

```sh
# Default threshold 0.85 — catches single-paragraph edits across an
# otherwise-identical document.
file-search-on near-duplicates -d ~/notes

# Scope to markdown only (skips images, archives, binaries — none of
# which fingerprint anyway).
file-search-on near-duplicates 'is_markdown' -d ~/notes

# Stricter: only near-byte-identical content (whitespace, single typo
# fix). At 0.95 threshold ≈ 3-bit Hamming distance.
file-search-on near-duplicates 'is_markdown' --threshold 0.95 -d ~/notes

# Looser: significant topical overlap. At 0.75 ≈ 16-bit Hamming
# distance — different documents that share large quoted passages
# will surface here.
file-search-on near-duplicates 'is_source && language == "go"' --threshold 0.75 -d ./internal
```

## Threshold cheatsheet

The SimHash similarity score is `1 - hamming_distance / 64`. The integer-distance equivalents:

| Threshold | Hamming distance | What it catches |
|---|---|---|
| 0.97 | ≤ 2 bits | Trailing whitespace / line-ending edits only |
| 0.95 | ≤ 3 bits | Typo fixes, regenerated timestamps |
| 0.90 | ≤ 6 bits | Minor edits (single paragraph rewrite) |
| **0.85 (default)** | ≤ 9 bits | Notable edits / template fills with mostly-identical text |
| 0.80 | ≤ 12 bits | Significant overlap, different documents |
| 0.75 | ≤ 16 bits | Substantial shared passages |
| 0.50 | ≤ 32 bits | Random — uncorrelated documents average here |

## Recipes

### Audit a notes collection for redundant copies

```sh
# Two markdown files saying mostly the same thing, plus the
# byte-identical sibling that 'duplicates' also catches.
file-search-on near-duplicates 'is_markdown' -d ~/notes --threshold 0.85

# Visualise the most-copied notes — biggest groups first.
file-search-on near-duplicates 'is_markdown' -d ~/notes -o json | jq '.groups[] | {count, rep: .representative}' | head
```

### Find duplicated code with edits

```sh
# Cross-package duplication in a Go codebase.
file-search-on near-duplicates 'is_source && language == "go" && !is_test_file' -d ./internal --threshold 0.85

# Same idea for Python.
file-search-on near-duplicates 'is_source && language == "python"' -d . --threshold 0.85 --exclude .venv
```

### Find email-template variants

PDF / office / EPUB / email all fingerprint via the same body-extraction path, so finding "this invoice template shipped to 50 customers with the customer name swapped" works the same way:

```sh
# Email templates with small variations (the customer name is a
# tiny fraction of total body text → high similarity).
file-search-on near-duplicates 'is_email' -d ~/Mail/Sent --threshold 0.95
```

### Find PDF re-renders of the same source

Different LaTeX runs of the same paper, different export settings of the same Pages doc, etc. — same body text under different rendering pipelines:

```sh
file-search-on near-duplicates 'is_pdf' -d ~/Papers --threshold 0.85
```

## Caching

Fingerprints land on `index.Entry.Fingerprint` alongside the existing sha256 hash, validated by the same `(size, mtime)` tuple. With `--index-path`, repeat runs on an unchanged tree skip the body read AND the SimHash compute.

```sh
file-search-on near-duplicates 'is_markdown' -d ~/notes --index-path ~/.file-search-on.bbolt
# First run: ~10MB/s body extraction + ~50MB/s SimHash compute (CPU-bound).
# Second run on unchanged tree: cache-hit on every file, only pairwise distance compute.
```

## Output

Table format:

```
fingerprint: 0xa5c3f0e21bd784c5  (count=3)
representative: /Users/me/notes/2025-q4-roadmap.md
  1.000  /Users/me/notes/2025-q4-roadmap.md  (5,432 B)
  0.953  /Users/me/notes/2025-q4-roadmap-v2.md  (5,128 B)
  0.875  /Users/me/notes/old/2025-q4-draft.md  (4,910 B)
```

JSON format (each group has representative + fingerprint + members[] with per-file similarity to representative):

```json
{
  "groups": [
    {
      "representative": "/Users/me/notes/2025-q4-roadmap.md",
      "fingerprint": "0xa5c3f0e21bd784c5",
      "count": 3,
      "members": [
        {"path": "/Users/me/notes/2025-q4-roadmap.md", "size": 5432, "similarity": 1.0},
        {"path": "/Users/me/notes/2025-q4-roadmap-v2.md", "size": 5128, "similarity": 0.953},
        {"path": "/Users/me/notes/old/2025-q4-draft.md", "size": 4910, "similarity": 0.875}
      ]
    }
  ],
  "total_files": 234,
  "fingerprinted": 192,
  "group_count": 1,
  "threshold": 0.85
}
```

## MCP

```json
{
  "name": "mcp__file-search-on__find_near_duplicates",
  "arguments": {
    "expr": "is_markdown",
    "dir": "~/notes",
    "threshold": 0.85,
    "min_size": 1024,
    "timeout_seconds": 30
  }
}
```

Returns the same shape as the CLI's JSON output. Honours the same partial-result contract as `search` / `stats` / `find_duplicates` — on timeout `cancelled=true` with the partial groups intact.

## Caveats

- **O(N²) pairwise.** Fine for thousands of candidates; minute-scale for tens of thousands. For hundreds of thousands an LSH banding pre-prune would be needed — out of scope for v1.
- **Only text-shaped + structured-document types fingerprint.** Binary families (image / audio / video / archive / install-package / disk-image / binary) return zero fingerprints. Use [`duplicates`](./duplicates.md) for binary content.
- **Empty / near-empty bodies don't fingerprint.** The cap is "at least 2 distinct tokens"; pure-whitespace or punctuation-only files surface as `FingerPrinted = 0` and are excluded from grouping.
- **Threshold is a precision/recall knob.** Lower threshold = more matches (recall) but more false positives (precision). 0.85 is the documented sweet spot for "near-duplicate" intuition; experiment for your corpus.
- **Body cap applies.** Files larger than `--body-max-bytes` (default 1 MiB) are silently truncated at the cap; only the prefix participates in the fingerprint.
- **No semantic similarity.** SimHash captures surface-level token overlap. Two paraphrased documents (same idea, different words) will NOT be grouped. For semantic similarity you'd need embeddings — out of scope.
