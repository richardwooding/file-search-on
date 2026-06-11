# Recipes — Images and EXIF

Image content types: `image/jpeg`, `image/png`, `image/gif`, `image/webp`, `image/svg+xml`, `image/tiff`, `image/bmp`, `image/heic`, plus eight RAW formats — `image/raw-cr2`, `image/raw-cr3`, `image/raw-nef`, `image/raw-arw`, `image/raw-dng`, `image/raw-raf`, `image/raw-orf`, `image/raw-rw2`. The umbrella boolean `is_image` matches any of them; `is_raw_photo` matches only the eight RAW formats.

EXIF metadata is extracted from JPEG, TIFF, HEIC, PNG (eXIf chunk), and every RAW format via [`evanoberholster/imagemeta`](https://github.com/evanoberholster/imagemeta). GIF/WebP/BMP/SVG fall back to stdlib `image.DecodeConfig` for `img_width`/`img_height` only.

## Camera and lens

Find photos shot on a specific camera:

```sh
file-search-on 'is_image && camera_make == "Canon"' -d ~/Pictures
file-search-on 'is_image && camera_model == "iPhone 15 Pro"'
```

Find shots from a specific lens (handy for prime-lens fans):

```sh
file-search-on 'is_image && lens.contains("50mm")' -d ~/Pictures
```

Photos with no EXIF camera info (web-stripped, scanned, or screenshots):

```sh
file-search-on 'is_image && camera_make == ""'
```

## Resolution

```sh
file-search-on 'is_image && img_width >= 4000'              # high-res
file-search-on 'is_image && img_width < 800'                # thumbnails
file-search-on 'is_image && img_width >= 4000 && img_height >= 3000'   # full-res photos
```

## Capture date

```sh
# 2024 photos
file-search-on 'is_image && taken_at >= timestamp("2024-01-01T00:00:00Z") && taken_at < timestamp("2025-01-01T00:00:00Z")'

# Last-week's shots (computed externally and substituted in)
file-search-on "is_image && taken_at > timestamp(\"$(date -u -v-7d +%Y-%m-%dT%H:%M:%SZ)\")"
```

`taken_at` falls back through DateTimeOriginal → CreateDate → ModifyDate, so even scanned or edited images that lack `DateTimeOriginal` will still report a date.

## GPS — geographic bounding boxes

Decimal degrees, north / east positive. To find photos taken in a region, set lat / lon ranges around the centre:

```sh
# London-ish
file-search-on 'is_image && gps_lat > 51.4 && gps_lat < 51.6 && gps_lon > -0.2 && gps_lon < 0.0'

# San Francisco
file-search-on 'is_image && gps_lat > 37.7 && gps_lat < 37.8 && gps_lon > -122.5 && gps_lon < -122.4'
```

Photos with NO GPS data (privacy review, web-stripped):

```sh
file-search-on 'is_image && gps_lat == 0.0 && gps_lon == 0.0 && camera_make != ""'
```

(The `camera_make != ""` filters out non-EXIF images like SVGs and BMPs, which would otherwise match the GPS = 0 condition.)

## Exposure settings

```sh
file-search-on 'is_image && iso > 1600'                          # high-ISO (low-light, noise)
file-search-on 'is_image && f_stop < 2.0'                        # wide-aperture / shallow DOF
file-search-on 'is_image && focal_length >= 200.0'               # telephoto
file-search-on 'is_image && exposure_time > 1.0'                 # long exposures > 1 sec
```

## Combined queries

A photographer's review query — high-ISO Sony shots from a specific camera body:

```sh
file-search-on 'is_image && camera_make == "SONY" && camera_model.contains("A7") && iso > 3200'
```

Wedding photographer's prime lens portfolio — 35mm or 85mm primes, wide aperture:

```sh
file-search-on 'is_image && (focal_length == 35.0 || focal_length == 85.0) && f_stop < 2.0'
```

iPhone photos taken in 2024 in a specific city:

```sh
file-search-on 'is_image && camera_model.contains("iPhone") && gps_lat > 51.4 && gps_lat < 51.6 && taken_at > timestamp("2024-01-01T00:00:00Z")'
```

## Orientation

EXIF orientation is 1-8. Common values:

| Value | Meaning |
| --- | --- |
| 1 | Normal |
| 3 | Rotated 180° |
| 6 | Rotated 90° CW (i.e. portrait phone capture stored as landscape) |
| 8 | Rotated 90° CCW |

```sh
file-search-on 'is_image && orientation == 6'        # phone-portraits
file-search-on 'is_image && orientation != 1 && orientation != 0'   # any non-default rotation
```

## Useful output formats

```sh
# Path + camera + ISO, tab-separated
file-search-on 'is_image && camera_make != ""' --format '{{.Path}}\t{{.CameraModel}}\t{{.ISO}}'

# JSON for jq — sort by ISO descending
file-search-on 'is_image && iso > 0' -o json | jq -s 'sort_by(-.iso) | .[].path'

# Bare paths for xargs (e.g. copy a year's photos)
file-search-on 'is_image && taken_at > timestamp("2024-01-01T00:00:00Z")' -o bare | xargs -I {} cp {} ~/photos-2024/
```

## Fuzzy matching for camera / lens names

EXIF strings vary across capitalisation and minor punctuation. Fuzzy operators normalise this without an explicit canonicalisation pass.

```sh
# Phonetic match catches "NIKON", "Nikon", "Nikkon" — all encode to the same Soundex code.
file-search-on 'is_image && soundex(camera_make) == soundex("Nikon")'

# Lens-model match within 3 edits — covers minor differences in formatting.
file-search-on 'is_image && levenshtein(lens, "70-200mm f/2.8") <= 3'
```

See [`fuzzy-search.md`](./fuzzy-search.md) for the full set of fuzzy / phonetic recipes.

## Photos inside an arbitrary region (point-in-polygon)

Bounding-box queries (`gps_lat > x && gps_lat < y && gps_lon > w && gps_lon < z`) are great for rectangles, but real geographies aren't rectangles. `point_in_polygon(lat, lon, polygon)` takes a flat `list<double>` of alternating lat,lon pairs in vertex order — wrap-around to the first point is implicit, you don't need to repeat it.

```sh
# Photos taken inside Cape Town's City Bowl (rough quadrilateral).
file-search-on '
  is_image &&
  point_in_polygon(gps_lat, gps_lon, [
    -33.96, 18.40,
    -33.91, 18.40,
    -33.91, 18.45,
    -33.96, 18.45
  ])
' -d ~/Pictures

# Concave polygons work — handy for "near the harbour but not in the water".
# Vertices defined clockwise or counter-clockwise; either order is fine.
file-search-on 'is_image && point_in_polygon(gps_lat, gps_lon, [<your-vertices>])'

# Compose with the rest of EXIF — Nikon, low ISO, wide aperture, in-region.
file-search-on '
  is_image &&
  soundex(camera_make) == soundex("Nikon") &&
  iso < 800 && f_stop < 2.8 &&
  point_in_polygon(gps_lat, gps_lon, [
    -33.96, 18.40, -33.91, 18.40, -33.91, 18.45, -33.96, 18.45
  ])
' -d ~/Pictures
```

## Apple Live Photo pairing

iPhone Live Photos pair an HEIC still with a short MOV video that share the same basename. file-search-on detects the pair via a sibling-file check and surfaces it on BOTH sides — the HEIC reports `is_live_photo` + the paired video's path and size; the MOV reports `is_live_photo_video` + the paired still's path.

```sh
# Every Live Photo (HEIC side)
file-search-on 'is_live_photo' -d ~/Pictures

# Live Photos taken in a date range with GPS — composes with all the EXIF
file-search-on 'is_live_photo && taken_at > timestamp("2026-01-01T00:00:00Z") && gps_lat != 0.0'

# Storage audit: large Live Photo videos worth converting to plain stills
file-search-on 'is_live_photo && live_photo_video_size > 5000000' --sort live_photo_video_size --order desc

# The video side of every pair (useful for batch operations on just the MOVs)
file-search-on 'is_live_photo_video' -d ~/Pictures

# Orphan MOVs — Live-Photo-shaped videos whose still got deleted (iOS sync glitches)
file-search-on 'video_codec == "h264" && duration < 5.0 && !is_live_photo_video' -d ~/Pictures
```

### Live Photo limitations

- **Pairing is by SIBLING FILE**, not by embedded `MakerNote` Live Photo metadata. Files separated across directories (e.g. `IMG.HEIC` in one folder, `IMG.MOV` in another) won't pair. Apple's canonical workflow always keeps them side-by-side.
- **Cache trade-off**: like the SQLite app-name (`#177`), plist kind (`#185`), and browser vendor (`#188`) path-based hooks, the lookup result is cached against THIS file's `(size, mtime)`. Deleting the sibling later won't invalidate the cached `is_live_photo` flag until this file itself changes. Re-run with `--no-index` or refresh the index when filesystem changes need to surface immediately.
- **Live Photo videos are always `.mov`**, never `.mp4` / `.m4v`. The `is_live_photo_video` predicate is restricted to the `.mov` extension to avoid false positives where an unrelated `IMG.mp4` happens to share a basename with an HEIC.
- **`.heif` paired with `.mov`** isn't detected by the canonical pair — Apple's exports always use `.HEIC`. If you've re-encoded an HEIC to `.heif`, the Live Photo pairing won't fire even if the MOV sibling exists.

## RAW photo queries

Eight RAW formats are recognised: Canon CR2 / CR3, Nikon NEF (+ NRW), Sony ARW (+ SRF / SR2), Adobe DNG, Fujifilm RAF, Olympus ORF (+ ORI), Panasonic RW2. The umbrella `is_raw_photo` matches all of them; per-format predicates (`is_cr2`, `is_nef`, `is_arw`, `is_dng`, …) discriminate. `raw_kind` and `raw_vendor` are stamped from the registration so they're populated even when EXIF is missing.

```sh
# Every RAW photo across a master collection
file-search-on 'is_raw_photo' -d ~/Pictures/RAW

# Only Canon RAW — discriminate via raw_vendor (covers CR2 + CR3)
file-search-on 'is_raw_photo && raw_vendor == "canon"' -d ~/Pictures

# Specific format
file-search-on 'is_dng' -d ~/Pictures
file-search-on 'is_arw' -d ~/Pictures

# RAW shot on a specific body (camera EXIF works the same as JPEG)
file-search-on 'is_raw_photo && camera_model == "Canon EOS R5"' -d ~/Pictures

# RAW with GPS — for the geographic-tagged subset of an archive
file-search-on 'is_raw_photo && gps_lat != 0.0' -d ~/Pictures

# RAW shot in 2024 at high ISO (low-light captures worth a second pass)
file-search-on 'is_raw_photo && iso >= 1600 && taken_at >= timestamp("2024-01-01T00:00:00Z")' -d ~/Pictures

# Histogram — RAW files grouped by vendor, useful as a storage audit
file-search-on stats 'is_raw_photo' --group-by raw_vendor -d ~/Pictures

# Cross-vendor: every photo by anyone, JPEG OR RAW, on a given trip date
file-search-on '(is_image) && taken_at >= timestamp("2024-07-01T00:00:00Z") && taken_at < timestamp("2024-08-01T00:00:00Z")'
```

### Known limitations

- **CRW (Canon CIFF, pre-2004)**, PEF (Pentax), SRW (Samsung, discontinued), IIQ (Phase One medium format), RWL (Leica) — out of scope. Files dispatch as generic `image/tiff` or `binary` and lose RAW discrimination.
- **`.raw` is claimed by `image/raw-rw2`** because Panasonic is the dominant `.raw` producer. A `.raw` from a different source will misattribute as Panasonic. Detection by content would need format-specific magic that most `.raw` shippers don't include.
- **The shared TIFF magic stays with `image/tiff`.** A `.cr2` renamed to `.bin` falls through to `image/tiff` rather than `image/raw-cr2`. This avoids ambiguous magic dispatch but means stripped-extension RAW files lose their per-vendor discrimination.
- **`raw_vendor` is a property of the format, not the camera.** A DNG produced by an iPhone reports `raw_vendor == "adobe"` because DNG is Adobe's container — the actual camera shows up in `camera_make`. Same for Leica / Pentax cameras that emit native DNG.

The algorithm is planar ray-casting — accurate for neighbourhoods, cities, and small countries. For continent-scale polygons or anything near the poles, project to a flat coordinate system before feeding the vertices in.

## C2PA / Content Credentials (provenance)

`file-search-on` reads the [C2PA](https://c2pa.org) provenance manifest embedded in JPEG and PNG files and surfaces what it **claims** — `is_c2pa`, `c2pa_claim_generator` (the creating/editing tool), `c2pa_title`, `c2pa_format`, and `c2pa_ai_generated`. **Unverified**: the JUMBF manifest is parsed but its cryptographic signature / trust chain is *not* validated (treat like EXIF — accurate-as-recorded, not authenticated).

```sh
# Images carrying Content Credentials
file-search-on 'is_image && is_c2pa' -d ~/Pictures

# AI-generated images (declared via a c2pa.actions digitalSourceType)
file-search-on 'is_image && c2pa_ai_generated' -d ~/Downloads

# Assets made/edited by a particular tool
file-search-on 'is_c2pa && c2pa_claim_generator.contains("Firefly")' -d ~/Assets
```

Note: absence of `c2pa_ai_generated` does **not** mean "not AI" — most files carry no manifest at all.
