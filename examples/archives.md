# Recipes — Archives

Archive content types: `archive/zip` (`.zip`, `.jar`, `.war`, `.ear`), `archive/tar` (`.tar`), `archive/tar+gzip` (`.tar.gz`, `.tgz`), `archive/gzip` (`.gz`). Umbrella boolean `is_archive`.

Hand-rolled on top of Go's `archive/zip`, `archive/tar`, and `compress/gzip` — no CGo, no external libs. Out of scope for v1: 7Z, RAR (need third-party libs), bzip2 / xz tarballs (xz isn't in stdlib), nested archives.

## Downloads-folder triage

The umbrella query — every archive in your Downloads:

```sh
file-search-on 'is_archive' -d ~/Downloads
```

By format:

```sh
file-search-on 'is_archive && content_type == "archive/zip"'      # ZIPs (incl. .jar / .war / .ear)
file-search-on 'is_archive && content_type == "archive/tar"'      # plain tarballs
file-search-on 'is_archive && content_type == "archive/tar+gzip"' # gzipped tarballs
file-search-on 'is_archive && content_type == "archive/gzip"'     # standalone .gz (single file)
```

Find the largest unpacked-content archives (sum of contents — distinct from the file's own compressed `size`):

```sh
file-search-on 'is_archive && uncompressed_size > 1000000000' -d ~/Downloads   # ≥ 1 GB unpacked
```

## ZIP-bomb / sprawl detection

A well-mannered tarball has all entries under a single top-level directory: the Unix convention. Archives that explode entries directly into the working directory when unpacked — `has_root_dir == false` — are either intentionally adversarial (ZIP bombs, rogue installers) or just badly packaged:

```sh
file-search-on 'is_archive && !has_root_dir' -d ~/Downloads
```

Combined with size — large archives that sprawl on extraction are the high-impact ones:

```sh
file-search-on 'is_archive && !has_root_dir && uncompressed_size > 100000000'   # ≥ 100 MB sprawl
```

Find archives whose top-level entry doesn't match the filename (a rough "did the maintainer name the tarball after the directory?" heuristic):

```sh
file-search-on 'is_archive && has_root_dir && top_level_entries[0] != name.replace(".tar.gz", "").replace(".zip", "")'
```

## Entry counts

Find sprawling archives — many files, large total content:

```sh
file-search-on 'is_archive && entry_count > 1000'              # node_modules-style bundles
file-search-on 'is_archive && entry_count > 10000'             # genuinely huge
```

Empty or near-empty archives — often broken downloads:

```sh
file-search-on 'is_archive && entry_count <= 1 && size > 1000' # 1 entry but kilobytes of overhead
```

## Compression ratio

Cross the file's `size` with `uncompressed_size` to spot ZIP bombs (extreme ratio) or already-compressed payloads (near 1:1):

```sh
# Compression ratio > 100x — ZIP-bomb candidate
file-search-on 'is_archive && size > 0 && uncompressed_size > 0 &&
                uncompressed_size / size > 100'

# Near-uncompressible (already-compressed JPEGs / videos in a ZIP)
file-search-on 'is_archive && size > 0 && uncompressed_size > 0 &&
                uncompressed_size * 100 / size < 110'   # within 10% of compressed size
```

## JAR / WAR / EAR

Java packages register as `archive/zip` because they ARE ZIPs. Filter by extension to find them specifically:

```sh
file-search-on 'is_archive && ext == ".jar"' -d ~/.m2          # Maven local repo
file-search-on 'is_archive && ext == ".war"' -d /opt/tomcat    # Tomcat webapps
```

If you want a per-format predicate (`is_jar`, `is_war`) it can be added later — for now the `ext` filter does the job.

## Standalone gzip

`.gz` files (without `.tar.gz`) are single-file gzip streams — `entry_count` is always 1 and `has_root_dir` is always `false`. The interesting attribute is `uncompressed_size` from the gzip ISIZE footer:

```sh
file-search-on 'content_type == "archive/gzip" && uncompressed_size > 100000000'   # gz of files ≥ 100 MB uncompressed
```

**Caveat**: ISIZE is mod 2³², so files whose uncompressed payload exceeds 4 GiB report a wrapped value (matches `gzip -l` behaviour). For accurate sizing on huge gzips, decompress to count.

## Useful output formats

```sh
# Path + content type + entry count + unpacked size, tab-separated
file-search-on 'is_archive' --format '{{.Path}}\t{{.ContentType}}\t{{.EntryCount}}\t{{.UncompressedSize}}'

# JSON for jq pipelines — sort by unpacked size descending
file-search-on 'is_archive' -o json | jq -s 'sort_by(-.uncompressed_size) | .[0:10] | .[] | "\(.uncompressed_size)\t\(.path)"'

# Bare paths for xargs (e.g. extract all top-level-rooted tarballs into a clean dir)
file-search-on 'is_archive && has_root_dir && content_type == "archive/tar+gzip"' -o bare \
  | xargs -I {} tar -xzf {} -C /tmp/extracted/
```
