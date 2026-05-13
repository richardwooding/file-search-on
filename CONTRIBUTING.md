# Contributing to file-search-on

Thanks for considering a contribution! The project is small enough to read in an afternoon and welcoming to first-time contributors. This document covers how to set up the project, the conventions for branches and commits, and how PRs get reviewed.

By participating, you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting bugs

Open a [GitHub issue](https://github.com/richardwooding/file-search-on/issues) using the bug report template. Please include:

- What you expected to happen.
- What actually happened.
- The version (`file-search-on --version`).
- A minimal repro if possible.

For **security issues**, do **not** open a public issue. See [SECURITY.md](SECURITY.md) for the private reporting channel.

## Suggesting features

Open a feature-request issue describing the use case before writing code — that's the cheapest way to avoid two-week PRs that turn out to be the wrong shape. The maintainers are happy to discuss direction in the issue before any code is written.

Easy entry points: open issues filtered by [`good first issue`](https://github.com/richardwooding/file-search-on/labels/good%20first%20issue), [`help wanted`](https://github.com/richardwooding/file-search-on/labels/help%20wanted), or [`enhancement`](https://github.com/richardwooding/file-search-on/labels/enhancement).

## Development setup

Requires Go 1.26.2 or newer.

```sh
git clone https://github.com/richardwooding/file-search-on.git
cd file-search-on
go build ./...
go test -race ./...
```

That's the whole setup — no environment variables, no fixtures to download, no services to run locally. Tests are fully hermetic (the content-type test suite uses on-disk fixtures under `internal/content/testdata/fixtures/` that are committed to the repo).

## Architecture map

[CLAUDE.md](./CLAUDE.md) is the canonical architecture map — five internal packages, the CEL evaluator's data shape, the walker's cancellation contract, the MCP server's tool surface, the release pipeline, and where every gotcha is documented. Written for both human and LLM contributors; either audience should find it readable.

For repetitive contributions (adding a content type, extending the CEL schema, adding an MCP tool, cutting a release), the repo ships step-by-step templates under [`.claude/skills/`](./.claude/skills/). Useful whether you're working solo or pairing with an LLM agent.

## Branching and commits

- Work off **`main`**. Feature branches use `<type>/<short-name>` — e.g. `feat/yaml-content-type`, `fix/exif-orientation`, `perf/markdown-scanner`, `docs/readme-cleanup`.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`, `perf:`. First line is a short summary scoped if relevant (e.g. `feat(content): add YAML detection`); the body (optional) explains *why*, not *what*.
- Sign-off (`git commit -s`) is appreciated but not required.

## Pull requests

1. Open a draft PR early if you'd like feedback on direction.
2. Make sure the full CI matrix passes locally before marking ready for review:
   ```sh
   go build ./...
   go test -race ./...
   go vet ./...
   golangci-lint run
   go fix -diff ./...   # CI enforces an empty diff
   ```
3. CI must be green: build, tests, lint, modernizer-diff.
4. PRs are squash-merged unless the change history is genuinely worth preserving.

PRs should:

- Include tests for new behaviour. The race detector is always on; new code that's not covered is rare and intentional.
- Update docs if user-facing behaviour changed. The `examples/*.md` tree is the home for per-feature recipes; the README points at it.
- Update the README schema-coverage test stays green — every attribute and CEL function declared in `celexpr.Schema()` must appear backticked somewhere in `README.md`. `go test ./internal/celexpr/` is the guard.

## Code style

```sh
go fmt ./...
golangci-lint run
go fix -diff ./...
```

Run before pushing. CI will fail otherwise.

## Fuzz testing

High-risk parsers (frontmatter, MP3, MKV, MP4, CEL compile, gob decoder) have native [Go fuzz targets](https://go.dev/doc/security/fuzz). The seed corpus runs on every CI build; a scheduled workflow (`.github/workflows/fuzz.yml`) runs each target for 5 minutes nightly to discover new failures. Crashes get committed back to `testdata/fuzz/<FuzzName>/` as regression coverage.

Run locally:

```sh
go test -run=FuzzSplitFrontmatter ./internal/content/                # seed corpus only (fast)
go test -fuzz=FuzzSplitFrontmatter -fuzztime=30s ./internal/content/ # mutate for 30 seconds
```

If you add a new parser, please add a matching fuzz target — see [CLAUDE.md § Fuzz testing](./CLAUDE.md#fuzz-testing) for the pattern.

## License of contributions

By submitting a PR, you agree your contribution is licensed under the same license as the project — see [LICENSE](LICENSE) (MIT).
