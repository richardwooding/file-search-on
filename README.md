# file-search-on

**Content-type aware file search with CEL-powered attribute filtering.**

`file-search-on` walks a directory tree and returns files matching a [CEL](https://github.com/google/cel-spec) expression evaluated over each file's metadata and content-type-specific attributes. Instead of grepping by name, ask things like:

```sh
file-search-on 'is_pdf && page_count > 10 && author == "Jane Doe"'
file-search-on 'is_image && gps_lat > 51.4 && gps_lat < 51.6'        # photos near home
file-search-on 'is_audio && artist == "Radiohead" && year < 2000'
file-search-on 'is_video && video_height >= 2160 && video_codec == "h265"'
file-search-on 'is_office && language == "fr"'
file-search-on 'is_markdown && "longread" in tags && word_count > 1000'
```

Across **24 file formats** organised into eight content-type families (documents, data, images, audio, video, office, ebooks, plain text), with format-specific metadata extraction.

## Features

- **Pluggable content-type detection** — extension-first with magic-byte fallback. New formats are a single registration call.
- **Eight content-type families**, each with its own metadata extractors:

  | Family | Formats | Bundle of attributes |
  | --- | --- | --- |
  | **Documents** | PDF, EPUB | title, author, language, page_count |
  | **Markup** | Markdown, HTML, XML | title, word_count, frontmatter, language, root_element |
  | **Data** | JSON, CSV, TSV | json_kind, column_count, csv_columns |
  | **Plain text** | TXT, log, … | line_count, word_count |
  | **Images** | JPEG, PNG, GIF, WebP, TIFF, BMP, SVG, HEIC | dimensions + EXIF: camera, lens, GPS, ISO, focal_length, taken_at |
  | **Audio** | MP3, M4A, FLAC, OGG | tags (artist, album, genre, year, …) + duration, bitrate, sample_rate, channels |
  | **Video** | MP4, MOV, MKV, WebM, AVI | duration, bitrate, video_codec, audio_codec, video_width/height, frame_rate |
  | **Office** | DOCX, XLSX, PPTX, ODT | title, author, language (Dublin Core) |

  Type predicates (`is_pdf`, `is_image`, `is_audio`, `is_video`, `is_office`, `is_epub`, …) light up automatically from the registered content type. See [examples/](./examples/) for recipes by family.

- **First-class Markdown front-matter** — YAML (`---`), TOML (`+++`), and JSON (`{ ... }`) are recognised by leading bytes. Common keys (`title`, `author`, `language`, `tags`, `categories`, `draft`, `date`) become top-level CEL variables; everything else lives in a generic `frontmatter` map. See [examples/markdown.md](./examples/markdown.md).
- **CEL expressions** — the full Common Expression Language: comparisons, `&&`/`||`, string functions, list membership, timestamp arithmetic. Composes naturally with structural attributes.
- **Multiple output formats** — `bare` (paths only), `default`, `verbose` (multi-line), `json` (NDJSON), or a Go `text/template` via `--format`.
- **MCP server mode** — same binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server (stdio, HTTP, or SSE). LLM agents can invoke `search` and `list_attributes` tools directly.
- **Pure Go, no CGO** — cross-compiles cleanly to all six release targets. No image/audio/video decoder dependencies.
- **Parallel walking** — files are evaluated across a worker pool (defaults to `NumCPU`).

## Install

### Homebrew (macOS / Linux)

```sh
brew install richardwooding/tap/file-search-on
```

The cask is published from this repo on every tagged release to [`richardwooding/homebrew-tap`](https://github.com/richardwooding/homebrew-tap).

> **macOS Gatekeeper:** the binary isn't yet signed with an Apple Developer ID, so macOS may block it on first run. The cask's post-install hook strips the quarantine xattr automatically. If macOS still blocks it:
>
> ```sh
> sudo xattr -dr com.apple.quarantine $(brew --prefix)/bin/file-search-on
> ```

### Container (Docker / Podman)

OCI images are published to GitHub Container Registry on every tag, with `linux/amd64` and `linux/arm64` manifests:

```sh
docker run --rm -v "$PWD:/work" ghcr.io/richardwooding/file-search-on:latest \
  'is_markdown && draft' -d /work
```

Pin to a specific version with `:vX.Y.Z`. The base image is [`cgr.dev/chainguard/static`](https://images.chainguard.dev/directory/image/static), so the container has the binary and nothing else (no shell).

### Pre-built binaries

Pre-built archives for Linux, macOS, and Windows on `amd64` and `arm64` are attached to every [GitHub Release](https://github.com/richardwooding/file-search-on/releases), along with a `checksums.txt` you should verify.

### From source

Requires Go 1.26.2 or newer.

```sh
go install github.com/richardwooding/file-search-on/cmd/file-search-on@latest
```

Or build from a clone:

```sh
git clone https://github.com/richardwooding/file-search-on.git
cd file-search-on
go build -o file-search-on ./cmd/file-search-on
```

## Usage

```sh
file-search-on [EXPR] [-d DIR] [-w WORKERS]
file-search-on --list
```

| Flag | Description | Default |
| --- | --- | --- |
| `EXPR` | CEL expression to match files against. | `true` (matches everything) |
| `-d`, `--dir` | Directory to search. | `.` |
| `-w`, `--workers` | Number of parallel workers. | number of CPU cores |
| `-l`, `--list` | List supported attributes and registered content types. | |
| `-L`, `--max-line-bytes` | Per-line scanner cap for text/CSV/HTML in bytes. Raise for very long log lines. | 1 MiB |
| `-o`, `--output` | Output preset: `bare` (paths only), `default`, `verbose` (multi-line records), `json` (NDJSON). | `default` |
| `--format` | Custom Go `text/template` per match (e.g. `'{{.Path}}\t{{.Title}}'`). Takes precedence over `-o`. | |

Each matching file is printed as `<path>\t[<content-type>]\t<size> bytes`. The match count is written to stderr so it doesn't pollute pipelines.

### Output presets

```sh
file-search-on 'is_markdown' -d ./docs -o bare       # one path per line; pipe-friendly
file-search-on 'is_markdown' -d ./docs               # default (back-compat)
file-search-on 'is_markdown' -d ./docs -o verbose    # multi-line, all attributes
file-search-on 'is_markdown' -d ./docs -o json       # NDJSON, one object per line
file-search-on 'is_markdown' -d ./docs --format '{{.Path}}\t{{.Title}}\t{{.WordCount}}'
```

`-o bare`, `-o json`, and `--format` suppress the `<N> file(s) found` summary on stderr (the count is implicit in the line count). `--format` uses Go [`text/template`](https://pkg.go.dev/text/template); the data context is a flat record — `{{.Path}}`, `{{.Title}}`, `{{.WordCount}}`, `{{.Frontmatter}}`, all the `Is*` booleans, etc. Backslash escapes (`\t`, `\n`) are expanded before parsing.

## Recipes

Focused recipe collections live under [`examples/`](./examples/):

| Recipe file | What's in it |
| --- | --- |
| [`examples/markdown.md`](./examples/markdown.md) | Front-matter (YAML / TOML / JSON), draft flags, tag membership, custom keys |
| [`examples/images.md`](./examples/images.md) | EXIF camera/lens, GPS bounding boxes, ISO / aperture / focal length, taken-at ranges |
| [`examples/audio.md`](./examples/audio.md) | Artist / album / genre / year, bitrate, sample rate, hi-res filtering |
| [`examples/video.md`](./examples/video.md) | Codec, resolution, frame rate, duration, MKV vs MP4 |
| [`examples/office.md`](./examples/office.md) | DOCX / XLSX / PPTX / ODT — title, author, language |
| [`examples/epub.md`](./examples/epub.md) | EPUB books — title, author, language; XMP fallback |
| [`examples/data.md`](./examples/data.md) | JSON arrays vs objects, CSV column membership, XML root elements |
| [`examples/text.md`](./examples/text.md) | Plain text / log files — line count, word count, big-line caps |
| [`examples/cookbook.md`](./examples/cookbook.md) | Cross-cutting recipes — dedupe, mixed media filters, pipeline integration |

A handful of representative one-liners:

```sh
# All Markdown files larger than 500 words
file-search-on 'is_markdown && word_count > 500' -d ./docs

# 4K HEVC videos longer than 30 minutes
file-search-on 'is_video && video_height >= 2160 && video_codec == "h265" && duration > 1800' -d ~/Videos

# Photos taken in 2024 with a Sony camera at high ISO
file-search-on 'is_image && camera_make == "SONY" && iso > 1600 && taken_at > timestamp("2024-01-01T00:00:00Z")' -d ~/Pictures

# CSVs with a "revenue" column
file-search-on 'is_csv && csv_columns.exists(c, c == "revenue")' -d ./reports

# French-language office documents
file-search-on 'is_office && language == "fr"' -d ~/Documents

# Audio tracks ≥ 96 kHz (hi-res)
file-search-on 'is_audio && sample_rate >= 96000' -d ~/Music
```

Combine paths and types — find HTML files inside a `build/` directory:

```sh
file-search-on 'is_html && dir.contains("build")'
```

## Available attributes

Run `file-search-on --list` for the canonical, up-to-date listing. The summary tables below are organised by attribute family.

### Common (always present)

| Attribute | Type | Description |
| --- | --- | --- |
| `name`, `path`, `dir` | string | Path components |
| `size` | int | File size in bytes |
| `ext` | string | File extension (e.g. `.md`) |
| `content_type` | string | Detected content type |
| `is_markdown`, `is_json`, `is_xml`, `is_html`, `is_pdf`, `is_image`, `is_text`, `is_csv`, `is_epub`, `is_office`, `is_audio`, `is_video` | bool | Type predicates |

### Document / markup

| Attribute | Type | Source |
| --- | --- | --- |
| `title` | string | Markdown front-matter / H1; HTML `<title>`; PDF `/Info`; EPUB `<dc:title>`; office `<dc:title>`; audio tags |
| `author` | string | Markdown front-matter, PDF `/Info`, EPUB / office `<dc:creator>` |
| `language` | string | EPUB / office `<dc:language>`; HTML `<html lang>`; markdown front-matter; PDF `/Lang` (XMP fallback) |
| `word_count` | int | Markdown body, plain text |
| `line_count` | int | Plain text |
| `page_count` | int | PDF |

### Data / structural

| Attribute | Type | Source |
| --- | --- | --- |
| `column_count`, `csv_columns` | int / `list<string>` | CSV/TSV header row |
| `json_kind` | string | JSON top-level: `"object"` or `"array"` |
| `root_element` | string | XML |

### Markdown front-matter (promoted)

| Attribute | Type | Notes |
| --- | --- | --- |
| `tags`, `categories` | `list<string>` | Bare strings are wrapped to single-element lists |
| `draft` | bool | Defaults to `false` when missing |
| `date` | timestamp | Native TOML dates + RFC3339 / `YYYY-MM-DD` strings |
| `frontmatter` | `map<string, dyn>` | Full parsed map — reach any custom key with `frontmatter.foo` |
| `frontmatter_format` | string | `"yaml"`, `"toml"`, `"json"`, or `""` |

### Images (EXIF)

| Attribute | Type | Source |
| --- | --- | --- |
| `img_width`, `img_height` | int | Pixel dimensions |
| `camera_make`, `camera_model`, `lens` | string | EXIF |
| `taken_at` | timestamp | EXIF DateTimeOriginal → CreateDate → ModifyDate fallback |
| `orientation` | int | EXIF orientation (1-8) |
| `gps_lat`, `gps_lon` | double | Decimal degrees, north / east positive |
| `iso` | int | EXIF ISO |
| `focal_length`, `f_stop`, `exposure_time` | double | mm, F-number, seconds |

### Audio

| Attribute | Type | Source |
| --- | --- | --- |
| `artist`, `album`, `album_artist`, `composer`, `genre` | string | Audio tags (ID3v1/v2, MP4 atoms, Vorbis comments) |
| `year`, `track` | int | Release year, track number |
| `duration` | double | Seconds |
| `bitrate` | int | kbps (file_size × 8 / duration / 1000) |
| `sample_rate` | int | Hz |
| `channels` | int | 1 = mono, 2 = stereo, … |

### Video

| Attribute | Type | Source |
| --- | --- | --- |
| `video_codec`, `audio_codec` | string | h264, h265, av1, vp9, aac, opus, ... |
| `video_width`, `video_height` | int | Frame pixels |
| `frame_rate` | double | fps |
| `duration` | double | Seconds (shared with audio) |
| `bitrate` | int | Kbps (shared with audio) |

## MCP server mode

The same binary can run as a [Model Context Protocol](https://modelcontextprotocol.io) server, exposing the search to any MCP-compatible client (Claude Desktop, IDE plugins, agents). Three transports:

```sh
file-search-on mcp                                       # stdio (default; for desktop clients)
file-search-on mcp --transport http --addr :8080         # Streamable HTTP (MCP 2025-03-26)
file-search-on mcp --transport sse  --addr :8080         # HTTP+SSE (DEPRECATED — MCP 2024-11-05)
```

| Transport | Spec version | When to use |
| --- | --- | --- |
| `stdio` | all | Desktop clients (Claude Desktop, IDE plugins) — the agent spawns the binary as a subprocess. |
| `http` | 2025-03-26 | Network-accessible servers, multi-client, or Docker deployments. |
| `sse` | 2024-11-05 | Legacy clients only. The HTTP+SSE transport was deprecated in the 2025-03-26 spec; new deployments should pick `http`. |

For HTTP and SSE, `--addr` (default `:8080`) is the bind address and `--path` (default `/`) is the URL prefix.

Two tools are exposed:

| Tool | Input | Output |
| --- | --- | --- |
| `search` | `expr`, `dir`, `workers`, `max_line_bytes` | `matches[]` (full attribute set per match) and `count` |
| `list_attributes` | none | `schema` (common, type_specific, frontmatter) and `content_types[]` |

Empty `expr` matches everything; empty `dir` defaults to `.`. `workers` falls back to `runtime.NumCPU()`.

Example Claude Desktop entry in `claude_desktop_config.json` (stdio):

```json
{
  "mcpServers": {
    "file-search-on": {
      "command": "file-search-on",
      "args": ["mcp"]
    }
  }
}
```

For HTTP-based clients, point at `http://<host>:<port>/` after starting the server with `--transport http`.

Built on [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

## Development

```sh
go build ./...                                  # build everything
go test -race -coverprofile=coverage.out ./...  # run the test suite
go vet ./...
go fix ./...                                    # apply Go 1.26 modernizers — see below
golangci-lint run
```

The codebase has three internal packages: `internal/content` (the pluggable type registry), `internal/celexpr` (the CEL evaluator and attribute builder), and `internal/search` (the parallel walker). See [CLAUDE.md](./CLAUDE.md) for an architecture overview and a step-by-step guide to adding a new content type.

### Keeping the code modern with `go fix`

Go 1.26 reintroduced [`go fix`](https://go.dev/blog/gofix) as a code-modernization tool that rewrites your sources to use newer language and standard-library features (`slices.Contains`, `any`, `min`/`max`, `range` over integers, `sync.WaitGroup.Go`, and more).

```sh
go fix -diff ./...   # preview the changes
go fix ./...         # apply them
go tool fix help     # list every available fixer
```

CI runs `go fix ./... && git diff --exit-code` after every build, so the project stays idiomatic for whichever Go release the toolchain is pinned to. After bumping the Go version, run `go fix ./...` from a clean working tree and commit the result on its own.

## License

[MIT](./LICENSE)
