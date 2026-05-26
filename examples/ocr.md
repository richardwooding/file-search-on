# Recipes — Screenshot OCR

`--ocr` runs OCR over `image/*` files and populates the `body` CEL variable with the recognized text. Once `body` is populated, every existing CEL string method (`contains`, `matches`, `startsWith`, `size`) works on the OCR output the same way it does for markdown / source / PDF / SQLite FTS bodies. Compose freely with EXIF, GPS, time-bucket grouping, hashes, and any other CEL filter.

**Platform support**: macOS Vision today (built-in since macOS 10.15, runs on the Neural Engine on Apple Silicon). Linux Tesseract and Windows.Media.Ocr providers are deferred to follow-up issues — the architecture's [provider abstraction](../internal/content/ocr/ocr.go) supports them dropping in without restructure. On platforms without a registered provider, `--ocr` is a clean no-op.

**Performance**: first walk over a tree of screenshots runs the macOS Vision helper subprocess per image — typically 200ms-2s per image, bounded by `--ocr-timeout` (default 10s). Cached in the body cache (`bodies_v1` bbolt bucket) keyed on `(size, mtime)`; subsequent walks are essentially free. Pair with `--index-path` to persist the cache across CLI invocations.

## Setup

The OCR helper is bundled in the Homebrew cask and the macOS release archives. After a fresh `brew install richardwooding/tap/file-search-on`, both binaries live in `/opt/homebrew/bin/` (or `/usr/local/bin/` on Intel) and the helper is found automatically.

For local development against a `go install`-style binary, the helper must be compiled separately:

```sh
git clone https://github.com/richardwooding/file-search-on
cd file-search-on
make ocr-helper   # writes $GOBIN/file-search-on-ocr-helper
go install ./cmd/file-search-on
```

The Go binary's helper-locator searches three places: `$FILE_SEARCH_ON_OCR_HELPER` env var → sibling to `os.Executable()` → `$PATH` lookup for `file-search-on-ocr-helper`. The `make ocr-helper` target installs into `$GOBIN`, which `os.Executable()` finds when you also `go install ./cmd/file-search-on`.

## Find the screenshot you can't remember

The headline use case — "where's the screenshot of the kubernetes error from last Tuesday?":

```sh
file-search-on 'is_image && body.contains("kubernetes")' --ocr -d ~/Desktop -d ~/Downloads

# Narrow to a date range — composes with EXIF taken_at on JPEGs and
# file mtime on PNGs (screenshots usually have no EXIF DateTime).
file-search-on 'is_image && body.contains("ERR_BLOCKED_BY_CLIENT") && mtime_year == 2026' --ocr -d ~/Desktop

# Find the screenshot from a specific website / app
file-search-on 'is_image && body.contains("github.com") && body.contains("Pull request")' --ocr -d ~/Desktop
```

## Quality filtering

`ocr_confidence` is the average per-line confidence across all recognized text (0..1). Low confidence usually means handwriting, blurry photos, or non-text imagery that the recognizer was uncertain about. Filter to skip noise:

```sh
# Only screenshots where OCR is reasonably confident
file-search-on 'is_image && body.contains("invoice") && ocr_confidence > 0.8' --ocr -d ~/Downloads

# Inverse — find ambiguous OCR for manual review (handwriting, photos of whiteboards)
file-search-on 'is_image && size(body) > 0 && ocr_confidence < 0.5' --ocr -d ~/Pictures
```

## Language filtering

`ocr_language` is the BCP-47 dominant language detected by `NLLanguageRecognizer` across the recognized text. Empty when the recognizer couldn't decide (short text, ambiguous script):

```sh
# Find Japanese screenshots (manga, kanji study material, Japanese UI)
file-search-on 'is_image && ocr_language == "ja"' --ocr -d ~/Pictures

# Find screenshots in Chinese (Simplified or Traditional)
file-search-on 'is_image && (ocr_language == "zh-Hans" || ocr_language == "zh-Hant")' --ocr

# Non-English content with high confidence
file-search-on 'is_image && ocr_language != "en" && ocr_language != "" && ocr_confidence > 0.7' --ocr
```

## Compose with EXIF

Camera photos still get EXIF; OCR ADDS the body. Mix freely:

```sh
# Find iPhone photos that captured text (whiteboards, slides, documents)
file-search-on 'is_image && camera_make == "Apple" && size(body) > 50' --ocr -d ~/Pictures

# Geographic — find screenshots taken at a specific location with specific text
file-search-on 'is_image && body.contains("conference") && gps_lat > 37 && gps_lat < 38' --ocr

# Long-form text photographs (slides, posters, signage)
file-search-on 'is_image && size(body) > 500 && ocr_confidence > 0.85' --ocr -d ~/Pictures
```

## Group-by + stats

```sh
# How many screenshots in each language
file-search-on stats 'is_image' --group-by ocr_language --ocr -d ~/Pictures

# What times of day do you take text-bearing screenshots?
file-search-on stats 'is_image && size(body) > 0' --group-by mtime_year --ocr -d ~/Desktop

# Confidence distribution — useful for tuning ocr_confidence thresholds
file-search-on 'is_image' --ocr -d ~/Pictures -o json | \
  jq -r '.ocr_confidence' | sort -n | uniq -c
```

## Pair with `--index-path` for persistence

Without a persisted index, OCR re-runs on every CLI invocation. With `--index-path`, the body cache lives across runs and only NEW or CHANGED images run the helper:

```sh
# First run — full OCR pass over the tree
file-search-on 'is_image && body.contains("kubernetes")' --ocr --index-path ~/.file-search-on/idx -d ~/Desktop

# Subsequent runs — cache hits except for new screenshots since the last walk
file-search-on 'is_image && body.contains("github")' --ocr --index-path ~/.file-search-on/idx -d ~/Desktop
```

## Known limitations

- **macOS only in v1.** The architecture supports cross-platform providers — Linux Tesseract and Windows.Media.Ocr can register under the same `Provider` interface without restructuring — but only the macOS Vision provider ships today. On Linux / Windows builds, `--ocr` is a no-op.
- **No handwriting recognition.** Vision technically supports it but quality is variable; we use the default `automatic` script setting. Handwriting-specific recognition could be a future `--ocr-handwriting` flag if asked.
- **No PDF page OCR.** Text PDFs already extract via the existing PDF body path. Image-only / scanned PDFs (no text layer) surface as empty `body` today; rasterize-then-OCR for that case is a separate follow-up.
- **No video frame OCR.** Out of scope.
- **`ocr_language` is the dominant language across the whole image.** Multilingual screenshots (e.g. a Japanese app with English error messages) bucket as one language, usually the majority script.
- **Helper subprocess per image.** Each OCR call spawns the Swift helper. For trees with thousands of new images on first walk, this is the dominant cost. The body cache amortises it across walks; consider `--index-path` for repeat queries.
- **Per-file timeout caps long calls.** Default 10s via `--ocr-timeout`. A pathological image (huge resolution, dense glyphs) that takes longer gets SIGKILL'd; the walk continues with an empty body for that file.
