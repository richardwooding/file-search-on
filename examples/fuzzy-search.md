# Recipes — Fuzzy search

The CEL environment ships four built-in functions for typo-tolerant and phonetic matching. These complement exact-match operators (`==`, `in`, `contains`) when the input data is hand-typed, scraped, or transliterated.

| Function | Returns | Use it for |
| --- | --- | --- |
| `levenshtein(a, b) -> int` | edit count | typo-tolerant equality (artist names, authors, camera makes) |
| `soundex(s) -> string` | 4-char ASCII code | phonetic matching (names that sound alike but spell differently) |
| `ngrams(s, n) -> list<string>` | character n-grams | low-level set composition with `.exists()` / `.size()` |
| `ngram_similarity(a, b, n) -> double` | Jaccard 0.0–1.0 | substring-tolerant similarity (titles, headlines) |

Run `file-search-on --list` for the canonical signatures.

## Levenshtein — typo-tolerant equality

```sh
# Within 2 edits of "Radiohead" — covers "Radiohad", "Radiohad ", "Radiohea", etc.
file-search-on 'is_audio && levenshtein(artist, "Radiohead") <= 2' -d ~/Music

# Authors with mistyped first names in markdown front-matter.
file-search-on 'is_markdown && levenshtein(author, "Jane Doe") <= 1' -d ./posts

# Camera make typos in EXIF — useful when scraping.
file-search-on 'is_image && levenshtein(camera_make, "FUJIFILM") <= 2' -d ~/Photos
```

Levenshtein is rune-aware: `café` vs `cafe` is one edit, not three.

## Soundex — phonetic matching

```sh
# Matches "Smith", "Smyth", "Smithe", "Smit" — all encode to S530.
file-search-on 'is_markdown && soundex(author) == soundex("Smith")' -d ./posts

# EXIF camera-make matches across capitalisation and minor typos.
file-search-on 'is_image && soundex(camera_make) == soundex("Nikon")' -d ~/Photos

# Audio-artist phonetic match — "Johnson" / "Jonson" / "Jansen" all collide on J525.
file-search-on 'is_audio && soundex(artist) == soundex("Johnson")' -d ~/Music
```

Soundex is the NARA standard: vowels separate same-code consonants, but H and W are transparent. `Ashcraft` and `Ashcroft` both encode to `A261`.

## N-gram similarity — substring-tolerant similarity

```sh
# Titles within 60% n-gram overlap of "kubernetes" — catches "Kuburnates",
# "Kubernates", and other transliterations.
file-search-on 'is_markdown && ngram_similarity(title, "kubernetes", 2) > 0.6' -d ./posts

# Audio albums with fuzzy similarity to a target — useful when tag spelling is messy.
file-search-on 'is_audio && ngram_similarity(album, "OK Computer", 2) > 0.7' -d ~/Music

# Filename match — survives reorderings ("file-search-on" vs "search-file-on" still high).
file-search-on 'ngram_similarity(name, "file-search-on", 3) > 0.5'
```

Pick `n` based on the length of your target string: `n=2` for short tokens (≤8 chars), `n=3` for typical words, `n=4+` for long phrases. Smaller `n` is more forgiving but matches more false positives.

## Composing the n-gram builder

The raw `ngrams(s, n)` builder is occasionally useful when you want set-membership over n-grams rather than the Jaccard score:

```sh
# Files whose title contains the trigram "kub" anywhere.
file-search-on 'is_markdown && "kub" in ngrams(title, 3)' -d ./posts

# Long-enough title (more than 5 unique trigrams).
file-search-on 'is_markdown && ngrams(title, 3).size() > 5' -d ./posts
```

## Combining fuzzy with exact

Fuzzy operators compose with the rest of CEL:

```sh
# Photos shot with a Nikon body that I tagged "portrait" in front-matter
# (assuming a sidecar markdown for each image).
file-search-on '
  is_image &&
  soundex(camera_make) == soundex("Nikon") &&
  iso < 800 &&
  f_stop < 2.8
' -d ~/Photos

# Markdown drafts mentioning anything Kubernetes-shaped in the title.
file-search-on '
  is_markdown && draft &&
  ngram_similarity(title, "kubernetes", 3) > 0.5
' -d ./drafts
```

## Performance note

Levenshtein is O(n × m) per call. N-gram similarity is O(n + m) for set construction plus O(min(|A|, |B|)) for the intersection. Both are fast on the short strings these queries usually touch (artist names, titles, camera makes). For long bodies — e.g. running `ngram_similarity` against PDF abstracts — query throughput can drop noticeably. Filter to a content-type subset first (`is_markdown && ...`) to keep the hot path narrow.
