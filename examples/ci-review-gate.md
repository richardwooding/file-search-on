# Recipes — CI review gate (GitHub Action)

`file-search-on review` is a diff-scoped review gate: it resolves the files changed in a git
diff, runs the per-file analyses (cyclomatic complexity, cognitive complexity, dead code)
scoped to just those files, and emits a `pass` / `warn` / `fail` verdict whose exit code
gates CI. The repo ships a composite **GitHub Action** that wraps it and uploads findings to
**Code Scanning** as SARIF — over-complex functions and dead code show up as inline
annotations on the pull-request diff.

> The action is a *composite* action, not a Docker action: `review` shells out to `git`, and
> the published OCI image's base (`chainguard/static`) has no git or shell. The action
> downloads the matching release binary and runs it on the runner, where git + the checkout
> already exist.

## Minimal PR gate

```yaml
# .github/workflows/review.yml
name: review
on:
  pull_request:
    branches: [main]
permissions:
  contents: read
  security-events: write   # required for SARIF upload (Code Scanning)
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0     # required — the PR gate needs the merge base
      - uses: richardwooding/file-search-on@v0.115.0
        with:
          base: origin/${{ github.base_ref }}
```

Two requirements are easy to miss:

- **`fetch-depth: 0`** on `actions/checkout` — `review --base origin/<branch>` computes the
  `<base>...HEAD` 3-dot diff, which needs the merge base in history. A shallow clone has no
  merge base and the diff comes back empty.
- **`permissions: security-events: write`** — `github/codeql-action/upload-sarif` writes to
  Code Scanning. Without it the upload step fails (the gate still works).

## Tuning the thresholds

```yaml
      - uses: richardwooding/file-search-on@v0.115.0
        with:
          base: origin/${{ github.base_ref }}
          max-complexity: "20"        # cyclomatic ceiling (default 15)
          max-cognitive: "25"         # cognitive ceiling (default 15)
          skip-dead-code: "true"      # complexity only — skip the second graph pass
          expr: "is_source && !path.contains('/vendor/')"
          exclude: |
            *_test.go
            *.gen.go
          prune-build-artefacts: "true"
```

## Baseline mode — don't block on pre-existing debt

The gate is diff-scoped at *file* granularity: by default it flags every over-threshold
function in a changed file, so a PR that merely touches a complex file is blocked on code
it didn't change. Set `baseline: true` to fail only on complexity that is **new or worsened**
versus the base ref — a function whose cyclomatic/cognitive value is unchanged (or lower)
than its baseline is left alone, even if it's over the ceiling.

```yaml
      - uses: richardwooding/file-search-on@v0.116.0
        with:
          base: origin/${{ github.base_ref }}
          baseline: "true"   # only NEW or WORSENED complexity fails the gate
```

The baseline is the same content the diff is taken against (the merge-base of `base` and
`HEAD`). New files and worsened functions still fail; pre-existing debt in touched files
does not. The same flag is `--baseline` on the CLI and `baseline: true` on the MCP `review`
tool.

## Report-only mode

To surface annotations without blocking the merge, set `gate: false`. The SARIF still
uploads, so findings appear in the PR and the Security tab, but the job stays green.

```yaml
      - uses: richardwooding/file-search-on@v0.115.0
        with:
          base: origin/${{ github.base_ref }}
          gate: false
```

Or flip the opposite way with `strict: "true"` to fail the job on `warn`-level findings
(e.g. dead code) too, not just `fail`-level ones.

## Using the outputs

```yaml
      - id: review
        uses: richardwooding/file-search-on@v0.115.0
        with:
          base: origin/${{ github.base_ref }}
          gate: false
      - if: steps.review.outputs.fail-count != '0'
        run: echo "::warning::${{ steps.review.outputs.fail-count }} fail / ${{ steps.review.outputs.warn-count }} warn — verdict ${{ steps.review.outputs.verdict }}"
```

Outputs: `verdict` (`pass` / `warn` / `fail` / `timeout` / `interrupted`), `fail-count`,
`warn-count`, `sarif-file`.

## Pinning the binary version

By default the action uses the binary from its own ref (`@v0.115.0` → the `v0.115.0`
release) or `latest` when referenced by branch/SHA. Override explicitly with `version`:

```yaml
      - uses: richardwooding/file-search-on@main
        with:
          version: v0.115.0     # pin the binary even when tracking @main
          base: origin/${{ github.base_ref }}
```

## Pre-commit / local equivalent

The same gate runs locally without the action — empty `--base` reviews uncommitted changes
against `HEAD`:

```sh
file-search-on review                       # uncommitted vs HEAD (pre-commit hook)
file-search-on review --base origin/main    # everything on this branch since main
file-search-on review --base origin/main -o sarif > review.sarif
```

Exit codes: `0` pass, `1` fail (or `warn` under `--strict`), `124` timeout, `130`
interrupted.

> Supported runners: `ubuntu-*` and `macos-*`. Windows is not yet supported.
