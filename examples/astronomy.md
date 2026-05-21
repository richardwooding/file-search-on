# Recipes — Astronomy & scientific data

Content types covered (the full `science/*` family):

- **`science/fits`** — Flexible Image Transport System, the dominant binary container in astronomy since 1981. Used by every major observatory and space telescope (HST, JWST, Chandra, ALMA, Gaia, TESS, Kepler) for images, tables, spectra, and data cubes.
- **`science/votable`** — IVOA tabular standard, XML-based. Used by every VO service (Simbad, Vizier, MAST, ESO archive, Gaia archive) for catalog query results and source lists.
- **`science/hdf5`** — Hierarchical Data Format v5. Used by LSST / Vera Rubin sky survey, LIGO gravitational waves, NetCDF4 (built on HDF5), every modern simulation pipeline, PyTorch / NumPy checkpoints.
- **`science/pds3`** — NASA Planetary Data System v3 (PVL labels). Voyager / Galileo / Cassini / MESSENGER / Mars rovers through Curiosity.
- **`science/pds4`** — NASA Planetary Data System v4 (XML labels). Current standard for Perseverance, Lucy, OSIRIS-REx, and future missions.
- **`science/cdf`** — NASA Common Data Format v3+ for heliophysics. Used by ACE, Wind, Cluster, MMS, Parker Solar Probe, Solar Orbiter.

Umbrella boolean `is_science_data` fires for all six. `is_pds` is the cross-version umbrella for PDS specifically.

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

## HDF5 — Hierarchical Data Format v5

HDF5 is the workhorse for large-scale scientific data — LSST sky-survey chunks, LIGO frames, every modern PyTorch / NumPy checkpoint, and the substrate for NetCDF4. v1 scope is superblock-only metadata; the recursive group / dataset hierarchy walk is a follow-up.

```sh
# All HDF5 files under a directory
file-search-on 'is_hdf5' -d ~/data

# Files written by libhdf5 1.10+ (compact v2/v3 superblock)
file-search-on 'is_hdf5 && hdf5_format_version >= 2' -d ~/data

# Legacy files (libhdf5 1.6 / 1.8 era)
file-search-on 'is_hdf5 && hdf5_format_version <= 1' -d ~/data

# 32-bit-era files with 4-byte offset addresses (rare today)
file-search-on 'is_hdf5 && hdf5_size_of_offsets == 4' -d ~/data
```

`.hdf` files: HDF4 (a different, older format) is NOT detected — its magic differs from HDF5's. The HDF5 magic-byte detector is reliable enough that `is_hdf5` doesn't false-positive on HDF4 even when both share the `.hdf` extension.

## PDS — NASA Planetary Data System

PDS covers every planetary-mission archive — Voyager through Perseverance — and ships in two distinct flavours:

- **PDS3**: PVL (Parameter Value Language) labels, free-form `KEYWORD = VALUE` pairs in `.lbl` files. Detected by extension AND by the `PDS_VERSION_ID` magic at offset 0.
- **PDS4**: XML labels in the NASA PDS namespace, named `.lblx`. The v1 parser handles `Product_Observational` only; Bundle / Collection / Document variants detect but don't populate attrs.

```sh
# All PDS labels (both versions)
file-search-on 'is_pds' -d ~/data

# PDS3-era archives only
file-search-on 'is_pds3' -d ~/data/voyager
file-search-on 'is_pds3' -d ~/data/cassini

# PDS4 (current standard)
file-search-on 'is_pds4' -d ~/data/perseverance

# Cross-version umbrella with version-specific behaviour
file-search-on 'is_pds && pds_version == "PDS4"' -d ~/data
```

### Filter by mission / target / instrument

```sh
# All Mars observations across missions
file-search-on 'is_pds && target_name.contains("MARS") || target_name == "Mars"' -d ~/data

# Perseverance Mastcam-Z images
file-search-on 'is_pds4 && instrument_name == "Mastcam-Z"' -d ~/data

# Specific mission
file-search-on 'is_pds && mission_name.contains("Voyager")' -d ~/data

# Find a specific product
file-search-on 'is_pds && product_id.contains("PSP_007146")' -d ~/data
```

### Time-bucketed observations

`start_time` is parsed into `taken_at` so the time-bucket vocabulary that works for FITS / images works here:

```sh
# PDS observations by year
file-search-on stats 'is_pds' -d ~/data --group-by taken_at_year

# Recent Perseverance data
file-search-on 'is_pds4 && taken_at > timestamp("2025-01-01T00:00:00Z")' -d ~/data
```

### Cross-family vocabulary

`title` is populated for both PDS3 (synthesised from `INSTRUMENT_NAME + TARGET_NAME`) and PDS4 (from `Identification_Area > title`). The same cross-family queries work here:

```sh
# All scientific data files mentioning Jezero (the Perseverance landing site)
file-search-on 'is_science_data && title.contains("Jezero")' -d ~/data
```

## CDF — Common Data Format (heliophysics)

CDF is NASA Goddard's archive format for space-physics time-series — used by every solar / magnetospheric mission (ACE, Wind, Cluster, MMS, Parker Solar Probe, Solar Orbiter) plus the broader SPDF Space Physics Data Facility archives.

```sh
# All CDF files
file-search-on 'is_cdf' -d ~/data/heliophysics

# CDF v3.8 or newer
file-search-on 'is_cdf && cdf_version >= "3.8"' -d ~/data

# Big files (many variables)
file-search-on 'is_cdf && variable_count > 100' -d ~/data --sort-by variable_count --order desc

# Row-major (C-style) layouts
file-search-on 'is_cdf && cdf_majority == "row"' -d ~/data

# Modern encoding (network or PC little-endian)
file-search-on 'is_cdf && (cdf_encoding == "network" || cdf_encoding == "ibmpc")' -d ~/data
```

The full `is_science_data` umbrella now covers astronomy (FITS, VOTable, HDF5), planetary science (PDS3, PDS4), and heliophysics (CDF) — every NASA-archive-standard format an agent might encounter under `~/data` lights up:

```sh
# Reconnaissance: how many of each science format under a directory?
file-search-on stats 'is_science_data' -d ~/data
```

## Known limitations

- **Header-only**: pixel-data inside the FITS file is never read. Use astropy / fitsio if you need the actual array.
- **No WCS projection**: `ra` / `dec` are the raw `CRVAL1` / `CRVAL2` reference values, not full sky-position computations for arbitrary pixels. Reading the CD matrix and projecting pixel → sky would need `wcslib` — out of scope.
- **Single attribute set**: multi-extension files surface attributes from the primary HDU only. Per-extension drilling (e.g. `hdu[1].telescope`) is not modelled.
- **VOTable row payloads**: the parser stops at the first table's `<DATA>` element. Row data (TABLEDATA TR/TD, BINARY/BINARY2 base64 streams) is never walked — search filters on `total_rows` rely on the `nrows` attribute set by the publishing tool. Tables without `nrows` contribute 0.
- **VOTable namespace requirement**: files literally named `.xml` that happen to contain VOTable XML detect as plain XML, not `science/votable`. Rename to `.vot` / `.votable` to engage the VOTable parser.
- **HDF5 hierarchy walk**: v1 ships superblock metadata only. Group / dataset enumeration (`group_count`, `dataset_count`, `top_level_groups`) is deferred — parsing v0/v1 B-trees and v2/v3 fractal heaps without real-world binary fixtures was higher risk than the metadata payoff justified. Tracked as a follow-up.
- **HDF5 superblock placement**: the spec allows the superblock at file offset 0, 512, 1024, 2048, etc. v1 only parses files with the superblock at offset 0 (overwhelmingly the common case). Non-zero offsets detect by extension (`.h5` / `.hdf5`) but surface no attributes.
- **PDS3 nested OBJECT/END_OBJECT groups**: only top-level keywords are parsed. The block-structured sub-objects (IMAGE, TABLE, etc.) that describe data layout aren't walked.
- **PDS4 Product_Bundle / Product_Collection / Product_Document**: only `Product_Observational` is supported in v1. Other product kinds detect (by `.lblx` extension) but return empty attrs.
- **PDS4 detection via `.xml`**: PDS4 labels literally named `.xml` detect as plain XML, not `science/pds4`. Rename to `.lblx` or symlink to engage the PDS4 parser.
- **CDF ISTP-convention global attributes**: v1 surfaces only the CDR + GDR header fields (version, encoding, majority, variable_count, attribute_count). The walk through the ADR (attribute descriptor record) linked list for ISTP convention attributes — `TITLE` → `title`, `PI_name` → `author`, first-record Epoch → `taken_at` — is deferred to a follow-up.
- **CDF v2.x**: detected only via the `.cdf` extension (v2 magic `0x0000FFFF` collides with too many other file types to register safely). Modern CDF files are v3+, where the `0xCDF30001` magic detects reliably.
- **CDF GDR beyond the read cap**: for files where the GDR sits past the 64 KiB initial read window AND the underlying fs.FS isn't seekable, `variable_count` and `attribute_count` are unset (the parser still surfaces `cdf_version`, `cdf_encoding`, `cdf_majority`). Real on-disk files (`os.DirFS`) are seekable so this is mostly an in-memory-test-FS limitation.
