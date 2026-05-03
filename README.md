# file-search-on

**Content-type aware file search with CEL-powered attribute filtering.**

`file-search-on` walks a directory tree and returns files matching a [CEL](https://github.com/google/cel-spec) expression evaluated over each file's metadata and content-type-specific attributes. Instead of grepping by name, you can ask things like *"all PDFs with more than 10 pages"* or *"all Markdown files over 500 words whose title starts with 'Draft'"*.

## Features

- **Pluggable content-type detection** — extension-first, with magic-byte fallback. Markdown, JSON, XML, HTML, PDF, and the common image formats (JPEG, PNG, GIF, WebP, SVG, TIFF, BMP) are supported out of the box.
- **Rich, type-specific attributes** — page count and author for PDFs, title and word count for Markdown, root element for XML, dimensions for images, and more.
- **CEL expressions** — the full Common Expression Language is available, so you can compose conditions naturally with `&&`, `||`, comparisons, string functions, and so on.
- **Parallel walking** — files are evaluated across a configurable worker pool (defaults to the number of CPU cores).

## Install

Requires Go 1.25 or newer.

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

Each matching file is printed as `<path>\t[<content-type>]\t<size> bytes`. The match count is written to stderr so it doesn't pollute pipelines.

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
| `is_markdown`, `is_json`, `is_xml`, `is_html`, `is_pdf`, `is_image` | bool | Type predicates |

Type-specific attributes (zero-valued when not applicable):

| Attribute | Type | Source |
| --- | --- | --- |
| `title` | string | Markdown H1, HTML `<title>`, or PDF metadata |
| `word_count` | int | Markdown |
| `page_count` | int | PDF |
| `author` | string | PDF |
| `root_element` | string | XML |
| `json_kind` | string | `"object"` or `"array"` |
| `img_width`, `img_height` | int | Image dimensions in pixels |

Run `file-search-on --list` to see the full, up-to-date list along with the registered content types.

## Development

```sh
go build ./...                                  # build everything
go test -race -coverprofile=coverage.out ./...  # run the test suite
go vet ./...
golangci-lint run
```

The codebase has three internal packages: `internal/content` (the pluggable type registry), `internal/celexpr` (the CEL evaluator and attribute builder), and `internal/search` (the parallel walker). See [CLAUDE.md](./CLAUDE.md) for an architecture overview and a step-by-step guide to adding a new content type.

## License

[MIT](./LICENSE)
