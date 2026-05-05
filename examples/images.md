# Recipes — Images and EXIF

Image content types: `image/jpeg`, `image/png`, `image/gif`, `image/webp`, `image/svg+xml`, `image/tiff`, `image/bmp`, `image/heic`. The umbrella boolean `is_image` matches any of them.

EXIF metadata is extracted from JPEG, TIFF, HEIC, and PNG (eXIf chunk) via [`evanoberholster/imagemeta`](https://github.com/evanoberholster/imagemeta). GIF/WebP/BMP/SVG fall back to stdlib `image.DecodeConfig` for `img_width`/`img_height` only.

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

The algorithm is planar ray-casting — accurate for neighbourhoods, cities, and small countries. For continent-scale polygons or anything near the poles, project to a flat coordinate system before feeding the vertices in.
