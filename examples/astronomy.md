# Recipes — Astronomy & scientific data

Content types covered:

- **`science/fits`** — Flexible Image Transport System, the dominant binary container in astronomy since 1981. Used by every major observatory and space telescope (HST, JWST, Chandra, ALMA, Gaia, TESS, Kepler) for images, tables, spectra, and data cubes.
- **`science/votable`** — IVOA tabular standard, XML-based. Used by every VO service (Simbad, Vizier, MAST, ESO archive, Gaia archive) for catalog query results and source lists.

Umbrella boolean `is_science_data` fires for both. Future HDF5 / PDS / CDF additions will join the same family.

The parser reads the FITS primary HDU header (80-byte ASCII cards packed into 2880-byte blocks) plus an HDU walk to count extensions. No pixel-data read — header-only metadata. Pure-Go stdlib, no third-party libs.

Detection is **magic + extension**: FITS files start with the literal ASCII bytes `SIMPLE  =` at offset 0, so files without the canonical `.fits` / `.fit` / `.fts` extension still detect correctly via the first-512-byte sniffer.

## All FITS files under a directory

```sh
file-search-on 'is_fits' -d ~/data/observations
file-search-on 'is_science_data' -d ~/data           # umbrella predicate
```

## Filter by telescope, instrument, target

```sh
# Everything observed with JWST
file-search-on 'is_fits && telescope == "JWST"' -d ~/data

# Specific instrument (Hubble's WFC3)
file-search-on 'is_fits && instrument == "WFC3"' -d ~/data

# Andromeda observations (object names often have spacing variants —
# use `contains` for fuzzy match)
file-search-on 'is_fits && object.contains("M31")' -d ~/data

# All exposures of a named target across telescopes
file-search-on 'is_fits && object == "NGC 1068"' -d ~/data
```

## Exposure and dimension filters

```sh
# Deep exposures (> 10 minutes)
file-search-on 'is_fits && exptime > 600.0' -d ~/data

# 2D images only (not spectra, not cubes)
file-search-on 'is_fits && naxis == 2' -d ~/data

# Large images (> 1024 px on the first axis)
file-search-on 'is_fits && naxis == 2 && naxis1 > 1024' -d ~/data

# Float-typed data (BITPIX = -32 or -64) — typically calibrated images
file-search-on 'is_fits && bitpix < 0' -d ~/data

# Specific filter / bandpass
file-search-on 'is_fits && filter == "F814W"' -d ~/data
```

## Sky-region (bounding-box) search

`ra` / `dec` are in degrees, sourced from CRVAL1 / CRVAL2 (WCS standard) with fallback to RA / DEC keywords:

```sh
# Andromeda neighbourhood (RA 10°-11°, Dec 41°-42°)
file-search-on 'is_fits && ra > 10.0 && ra < 11.0 && dec > 41.0 && dec < 42.0' \
  -d ~/data

# Galactic plane survey (Dec ± 5°)
file-search-on 'is_fits && dec > -5.0 && dec < 5.0' -d ~/data
```

## Multi-extension FITS triage

```sh
# Files with multiple HDUs (MEF — mosaic detectors, complex pipelines)
file-search-on 'is_fits && hdu_count > 1' -d ~/data

# Heavy mosaics
file-search-on 'is_fits && hdu_count > 10' -d ~/data --sort-by hdu_count --order desc
```

## Time-bucketed observations

`taken_at` is parsed from the `DATE-OBS` header (ISO 8601, falling back to plain `YYYY-MM-DD`), so the same time-bucket vocabulary that works for images works here:

```sh
# Observations by year
file-search-on stats 'is_fits' -d ~/data --group-by taken_at_year

# Observations in 2025 only
file-search-on 'is_fits && taken_at > timestamp("2025-01-01T00:00:00Z")' -d ~/data
```

## Combine with the document / image vocabulary

`OBJECT` is lifted to the shared `title` attribute and `OBSERVER` to `author`, so cross-family queries compose:

```sh
# All files (any family) mentioning M31 in the title — markdown notes,
# PDF papers, FITS observations
file-search-on 'title.contains("M31")' -d ~/research

# Files authored by a specific PI — emails, papers, FITS data
file-search-on 'author == "Dr Smith"' -d ~/projects
```

## Output formats

```sh
# Verbose record per file
file-search-on 'is_fits' -d ~/data -o verbose

# NDJSON for piping into jq / a notebook
file-search-on 'is_fits' -d ~/data -o json | jq 'select(.exptime > 600)'

# Stats by content_type (default — separates FITS from other files)
file-search-on stats 'is_fits' -d ~/data

# Aggregate by year via the time-bucket vocabulary
file-search-on stats 'is_fits' -d ~/data --group-by taken_at_year
```

`group_by telescope` / `group_by instrument` are not yet wired into the stats bucketing layer — use `jq` over the JSON output if you need ad-hoc grouping by those keys:

```sh
file-search-on 'is_fits' -d ~/data -o json | \
  jq -r '.telescope' | sort | uniq -c | sort -rn
```

## VOTable — IVOA catalog files

VOTable files (`.vot` / `.votable`) carry tabular astronomical data from Virtual Observatory services. The parser reads the XML header — VOTABLE version, RESOURCE / TABLE structure, FIELD definitions — without walking row payloads.

```sh
# All VOTable files under a directory
file-search-on 'is_votable' -d ~/data/queries

# Specific VOTable version (e.g. recent VO services emit 1.4)
file-search-on 'is_votable && votable_version == "1.4"' -d ~/data/queries

# Tables with > 1000 rows (uses the TABLE@nrows attribute when present)
file-search-on 'is_votable && total_rows > 1000' -d ~/data

# Multi-resource files (catalog with multiple tables)
file-search-on 'is_votable && table_count > 1' -d ~/data
```

### Filter by column / UCD

`field_names` carries every column name in declaration order; `field_ucds` carries the IVOA Unified Content Descriptors (semantic types like `phot.mag`, `pos.eq.ra`, `time.epoch`):

```sh
# Catalogs that contain a magnitude column
file-search-on 'is_votable && "mag" in field_names' -d ~/data

# Files with right-ascension / declination columns (positional catalogs)
file-search-on 'is_votable && "pos.eq.ra" in field_ucds' -d ~/data

# Files that include a redshift column
file-search-on 'is_votable && "src.redshift" in field_ucds' -d ~/data
```

### Data-format triage

```sh
# Files using TABLEDATA (XML rows) — searchable as text
file-search-on 'is_votable && votable_data_format == "tabledata"' -d ~/data

# Files with base64-encoded binary payloads (faster but opaque to grep)
file-search-on 'is_votable && (votable_data_format == "binary" || votable_data_format == "binary2")' -d ~/data
```

### Cross-format vocabulary

`title` is populated from the root `<DESCRIPTION>` text; `author` from `<INFO name="creator">`. The same cross-family queries that work for FITS work here:

```sh
# All scientific data files (FITS + VOTable) by a specific author
file-search-on 'is_science_data && author == "Gaia DPAC"' -d ~/data

# Files described as Gaia data (across formats)
file-search-on 'is_science_data && title.contains("Gaia")' -d ~/data
```

## Known limitations

- **Header-only**: pixel-data inside the FITS file is never read. Use astropy / fitsio if you need the actual array.
- **No WCS projection**: `ra` / `dec` are the raw `CRVAL1` / `CRVAL2` reference values, not full sky-position computations for arbitrary pixels. Reading the CD matrix and projecting pixel → sky would need `wcslib` — out of scope.
- **Single attribute set**: multi-extension files surface attributes from the primary HDU only. Per-extension drilling (e.g. `hdu[1].telescope`) is not modelled.
- **VOTable row payloads**: the parser stops at the first table's `<DATA>` element. Row data (TABLEDATA TR/TD, BINARY/BINARY2 base64 streams) is never walked — search filters on `total_rows` rely on the `nrows` attribute set by the publishing tool. Tables without `nrows` contribute 0.
- **VOTable namespace requirement**: files literally named `.xml` that happen to contain VOTable XML detect as plain XML, not `science/votable`. Rename to `.vot` / `.votable` to engage the VOTable parser.
- **Other astronomy formats** (HDF5, PDS, CDF) are not yet supported but the `is_science_data` umbrella is positioned to extend cleanly when those land — see issues #161, #162, #163.
