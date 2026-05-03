# file-search-on

**Content-type aware file search with CEL-powered attribute filtering.**

`file-search-on` walks a directory tree and returns files matching a [CEL](https://github.com/google/cel-spec) expression evaluated over each file's metadata and content-type-specific attributes. Instead of grepping by name, you can ask things like *"all PDFs with more than 10 pages"* or *"all Markdown files over 500 words whose title starts with 'Draft'"*.

## Features

- **First-class Markdown front-matter search** — query YAML (`---`), TOML (`+++`), and JSON (`{ ... }`) front-matter directly. Common keys (`title`, `author`, `tags`, `categories`, `draft`, `date`) are promoted to top-level CEL variables, and a generic `frontmatter` map exposes every other key. See the [dedicated section](#markdown-front-matter-search) below.
- **Pluggable content-type detection** — extension-first, with magic-byte fallback. Markdown, JSON, XML, HTML, PDF, and the common image formats (JPEG, PNG, GIF, WebP, SVG, TIFF, BMP) are supported out of the box.
- **Rich, type-specific attributes** — page count and author for PDFs, title and word count for Markdown, root element for XML, dimensions for images, and more.
- **CEL expressions** — the full Common Expression Language is available, so you can compose conditions naturally with `&&`, `||`, comparisons, string functions, and so on.
- **Parallel walking** — files are evaluated across a configurable worker pool (defaults to the number of CPU cores).

## Install

### Homebrew (macOS / Linux)

```sh
brew install richardwooding/tap/file-search-on
```

The cask is published from this repo on every tagged release to [`richardwooding/homebrew-tap`](https://github.com/richardwooding/homebrew-tap).

> **macOS Gatekeeper:** the binary isn't yet signed with an Apple Developer ID, so macOS may block it on first run. The cask's post-install hook strips the quarantine xattr automatically. If macOS still blocks it, run:
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

`-o bare` and `-o json` and `--format` suppress the `<N> file(s) found` summary on stderr (the count is implicit in the line count). `--format` uses Go [`text/template`](https://pkg.go.dev/text/template); the data context is a flat record — `{{.Path}}`, `{{.Title}}`, `{{.WordCount}}`, `{{.Frontmatter}}`, all the `Is*` booleans, etc. Backslash escapes (`\t`, `\n`) are expanded before parsing.

## Markdown front-matter search

Searching across the front-matter of large note collections, blogs, and documentation sites is a primary use case for this tool. Three formats are recognised, detected by the very first bytes of the file:

| Format | Opening | Closing | Common in |
| --- | --- | --- | --- |
| YAML | `---` line | `---` line | Jekyll, Obsidian, Hugo, MkDocs |
| TOML | `+++` line | `+++` line | Hugo, Zola |
| JSON | `{` at byte 0 | matching `}` | Hugo, Eleventy |

Parsing is lightweight: the front-matter block is read and decoded directly with [`gopkg.in/yaml.v3`](https://pkg.go.dev/gopkg.in/yaml.v3), [`pelletier/go-toml/v2`](https://pkg.go.dev/github.com/pelletier/go-toml/v2), or `encoding/json`. The Markdown body is not parsed beyond a single pass for `word_count` and an H1 title fallback, so this stays fast across thousands of files.

Six commonly-used keys are promoted to first-class CEL variables; everything else is reachable through the generic `frontmatter` map.

| CEL variable | Type | Notes |
| --- | --- | --- |
| `title` | string | Front-matter `title` overrides the H1 fallback. |
| `author` | string | From front-matter. |
| `tags` | `list<string>` | A bare string (`tags: solo`) is wrapped as a single-element list. |
| `categories` | `list<string>` | Same coercion as `tags`. |
| `draft` | bool | Defaults to `false` when missing. |
| `date` | timestamp | Native TOML dates and common YAML/JSON string layouts (RFC3339, `YYYY-MM-DD`, etc.) are accepted. |
| `frontmatter` | `map<string, dyn>` | Full parsed map. Reach any custom key with `frontmatter.your_key`. |
| `frontmatter_format` | string | `"yaml"`, `"toml"`, `"json"`, or `""` if no front-matter was present. |

### Front-matter examples

Find drafts in your blog:

```sh
file-search-on 'is_markdown && draft' -d ./content
```

Find Markdown tagged `go` and not draft:

```sh
file-search-on 'is_markdown && "go" in tags && !draft' -d ./posts
```

Find published posts from 2024 onward:

```sh
file-search-on 'is_markdown && date >= timestamp("2024-01-01T00:00:00Z")'
```

Reach a custom front-matter key (e.g. `category: essay`):

```sh
file-search-on 'is_markdown && frontmatter.category == "essay"'
```

Find files in a specific front-matter format:

```sh
file-search-on 'is_markdown && frontmatter_format == "toml"'
```

Combine front-matter with structural attributes — long, tagged, non-draft posts:

```sh
file-search-on 'is_markdown && word_count > 1000 && "longread" in tags && !draft'
```

## Examples

Find all Markdown files larger than 500 words:

```sh
file-search-on 'is_markdown && word_count > 500' -d ./docs
```

Find PDFs with more than 10 pages, written by a specific author:

```sh
file-search-on 'is_pdf && page_count > 10 && author == "Jane Doe"'
```

Find all JSON files whose top-level value is an array:

```sh
file-search-on 'is_json && json_kind == "array"'
```

Find images wider than 1920 pixels:

```sh
file-search-on 'is_image && img_width > 1920' -d ~/Pictures
```

Find photos shot at high ISO on a specific camera:

```sh
file-search-on 'is_image && camera_make == "Canon" && iso > 1600' -d ~/Pictures
```

Find photos taken in a geographic bounding box (London-ish):

```sh
file-search-on 'is_image && gps_lat > 51.4 && gps_lat < 51.6 && gps_lon > -0.2 && gps_lon < 0.0' -d ~/Pictures
```

Find photos taken in a date range:

```sh
file-search-on 'is_image && taken_at > timestamp("2024-01-01T00:00:00Z") && taken_at < timestamp("2025-01-01T00:00:00Z")' -d ~/Pictures
```

Find audio files by artist, album, or genre:

```sh
file-search-on 'is_audio && artist == "Radiohead"' -d ~/Music
file-search-on 'is_audio && genre == "Jazz" && year > 2000' -d ~/Music
file-search-on 'is_audio && album == "OK Computer"' -d ~/Music --format '{{.Track}}\t{{.Title}}'
```

Find audio by duration, bitrate, or sample rate (hi-res):

```sh
file-search-on 'is_audio && duration > 600' -d ~/Music                  # tracks > 10 minutes
file-search-on 'is_audio && bitrate >= 320' -d ~/Music                  # high-bitrate MP3 / lossless
file-search-on 'is_audio && sample_rate >= 96000' -d ~/Music            # hi-res audio
```

Find video by codec, resolution, or duration:

```sh
file-search-on 'is_video && video_codec == "h265"' -d ~/Videos          # HEVC encodes
file-search-on 'is_video && video_height >= 2160' -d ~/Videos           # 4K and above
file-search-on 'is_video && duration > 1800' -d ~/Videos                # over 30 minutes
file-search-on 'is_video && frame_rate > 30' -d ~/Videos                # high-frame-rate
```

Combine paths and types — find HTML files inside a `build/` directory:

```sh
file-search-on 'is_html && dir.contains("build")'
```

## Available attributes

Common attributes (always present):

| Attribute | Type | Description |
| --- | --- | --- |
| `name` | string | Filename |
| `path` | string | Full path |
| `dir` | string | Parent directory |
| `size` | int | File size in bytes |
| `ext` | string | File extension, e.g. `.md` |
| `content_type` | string | Detected content type |
| `is_markdown`, `is_json`, `is_xml`, `is_html`, `is_pdf`, `is_image`, `is_text`, `is_csv`, `is_epub`, `is_office`, `is_audio`, `is_video` | bool | Type predicates (`is_office` covers DOCX/XLSX/PPTX/ODT; `is_audio` covers MP3/M4A/FLAC/OGG; `is_video` covers MP4/MOV/MKV/WebM/AVI) |

Type-specific attributes (zero-valued when not applicable):

| Attribute | Type | Source |
| --- | --- | --- |
| `title` | string | Markdown front-matter, then H1; HTML `<title>`; PDF `/Info` dict; EPUB `<dc:title>`; DOCX/XLSX/PPTX/ODT `<dc:title>` |
| `word_count` | int | Markdown body (front-matter excluded), plain text |
| `line_count` | int | Plain text |
| `column_count` | int | CSV/TSV header row |
| `csv_columns` | `list<string>` | CSV/TSV header field names |
| `page_count` | int | PDF |
| `author` | string | Markdown front-matter, PDF `/Info`, EPUB `<dc:creator>`; DOCX/XLSX/PPTX/ODT `<dc:creator>` |
| `language` | string | EPUB `<dc:language>`; HTML `<html lang="...">`; markdown front-matter `language`; PDF `/Lang` (or XMP `<dc:language>` fallback); DOCX/XLSX/PPTX/ODT `<dc:language>` |
| `root_element` | string | XML |
| `json_kind` | string | `"object"` or `"array"` |
| `img_width`, `img_height` | int | Image dimensions in pixels |
| `camera_make`, `camera_model`, `lens` | string | EXIF camera/lens identification (JPEG/TIFF/HEIC/PNG) |
| `taken_at` | timestamp | EXIF capture time (DateTimeOriginal → CreateDate → ModifyDate fallback) |
| `orientation` | int | EXIF orientation tag (1-8) |
| `gps_lat`, `gps_lon` | double | GPS coordinates in decimal degrees (north / east positive) |
| `iso` | int | EXIF ISO sensitivity |
| `focal_length`, `f_stop`, `exposure_time` | double | EXIF focal length (mm), F-number, exposure (s) |
| `artist`, `album`, `album_artist`, `composer`, `genre` | string | Audio tags (ID3v1/v2 for MP3, MP4 atoms for M4A, Vorbis comments for FLAC/OGG) |
| `year`, `track` | int | Audio release year and track number |
| `duration` | double | Audio or video length in seconds |
| `bitrate` | int | Audio or video average bitrate in kbps (file_size × 8 / duration / 1000) |
| `sample_rate` | int | Audio sample rate in Hz |
| `channels` | int | Audio channel count |
| `video_codec`, `audio_codec` | string | Codec names (h264, h265, av1, vp9, aac, opus, ...) |
| `video_width`, `video_height` | int | Video frame dimensions in pixels |
| `frame_rate` | double | Video frames per second |
| `frontmatter` | `map<string, dyn>` | Full Markdown front-matter map |
| `frontmatter_format` | string | `"yaml"`, `"toml"`, `"json"`, or `""` |
| `tags`, `categories` | `list<string>` | Markdown front-matter |
| `draft` | bool | Markdown front-matter |
| `date` | timestamp | Markdown front-matter |

Run `file-search-on --list` to see the full, up-to-date list along with the registered content types.

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
