# Recipes — Project type detection

file-search-on identifies what *kind* of project lives in a directory based on canonical indicator files (`go.mod` → Go, `package.json` → Node, `Cargo.toml` → Rust, `*.tf` → Terraform, etc.). Mirrors the content-type concept up one level — content types classify *files*, project types classify *directories*.

Two surfaces:

- **`detect-project <dir>`** — what is THIS directory?
- **`find-projects <root>`** — find all projects under a root.

## Built-in project types

| Name | Indicators |
|---|---|
| `go` | `go.mod` |
| `node` | `package.json` |
| `rust` | `Cargo.toml` |
| `python` | `pyproject.toml`, `requirements.txt`, `Pipfile`, `setup.py`, `setup.cfg` (any) |
| `ruby` | `Gemfile` |
| `java-maven` | `pom.xml` |
| `java-gradle` | `build.gradle`, `build.gradle.kts`, `settings.gradle`, `settings.gradle.kts` (any) |
| `dotnet` | `*.csproj`, `*.fsproj`, `*.vbproj`, `*.sln` (any) |
| `terraform` | `*.tf` |
| `docker-compose` | `docker-compose.yml`, `docker-compose.yaml`, `compose.yml`, `compose.yaml` (any) |

Multiple types can match a single directory simultaneously — a Go module that also ships `docker-compose.yml` fires both `go` and `docker-compose`. Mirrors the cross-firing semantics for file content types (PR #95).

## detect-project — what is this directory?

```sh
file-search-on detect-project .                          # default human output
file-search-on detect-project . -o json                  # machine-readable
file-search-on detect-project /path/to/repo
```

Example output:

```
/Users/me/Code/my-app
  go                via go.mod
  docker-compose    via docker-compose.yml
```

Non-recursive — only the given directory's own listing is read. Use `find-projects` to walk a tree.

## find-projects — discover projects under a root

```sh
# All projects under ~/Code (stops at the first match per branch by default)
file-search-on find-projects ~/Code --exclude node_modules --exclude .git

# Only Go and Rust projects
file-search-on find-projects ~/Code --type go --type rust

# Recurse into matched roots to surface monorepo sub-projects
file-search-on find-projects ~/Code --nested

# JSON output for piping to jq
file-search-on find-projects ~/Code -o json | jq '.projects[].path'

# Bounded walk — abort after 30s, return what was found so far
file-search-on find-projects ~/big-tree --timeout 30s
```

Example output:

```
/Users/me/Code/blog       [node]
/Users/me/Code/cli-tool   [go,docker-compose]
/Users/me/Code/data-pipe  [python]

3 project(s) found in 0.043s
```

By default the walker stops at each matched project root — so a Go workspace with vendored Go submodules surfaces ONCE as the outer project. Pass `--nested` to also walk into matched roots; useful for monorepos with multiple workspaces.

## MCP

Both surfaces are also exposed as MCP tools:

```json
// What is this directory?
{
  "name": "mcp__file-search-on__detect_project",
  "arguments": { "dir": "/Users/me/Code/my-app" }
}

// Find all Go + Rust projects under ~/Code
{
  "name": "mcp__file-search-on__find_projects",
  "arguments": {
    "dir": "/Users/me/Code",
    "types": ["go", "rust"],
    "excludes": ["node_modules", ".git", "target", "dist"]
  }
}
```

`find_projects` honours the same partial-result contract as the file-search tools: on timeout it returns the projects found so far with `cancelled: true` rather than erroring.

## Common patterns

```sh
# Are any of my repos missing a CI workflow?
for d in $(file-search-on find-projects ~/Code --type go -o json | jq -r '.projects[].path'); do
  [ -d "$d/.github/workflows" ] || echo "missing CI: $d"
done

# What's the language mix across my workspace?
file-search-on find-projects ~/Code -o json | jq -r '.projects[].types[].type' | sort | uniq -c

# Find every Terraform stack under infra/
file-search-on find-projects ./infra --type terraform
```

## Custom project types via CEL

Register your own project types in YAML and load them with `--project-type-config <path>`. CEL expressions evaluate against two list-of-string variables: `files` (basenames of files in the inspected dir) and `subdirs` (basenames of immediate subdirectories).

```yaml
# ~/projects.yaml
project_types:
  - name: helm-chart
    description: Helm chart directory
    indicators:
      - cel: '"Chart.yaml" in files && "values.yaml" in files'
  - name: my-app
    description: Internal Foo app
    indicators:
      - cel: '"services" in subdirs && "foo.yaml" in files'
  - name: tf-stack
    indicators:
      - has_glob: "*.tf"
      - cel: '"main.tf" in files'   # any indicator firing counts
```

```sh
file-search-on --project-type-config ~/projects.yaml detect-project ./my-app
file-search-on --project-type-config ~/projects.yaml find-projects ~/Code --type helm-chart
```

Indicators are OR'd within a project type — any matching indicator counts. Custom types coexist with the 10 built-ins. CEL compile errors fail the config load with the offending entry's name surfaced.

The CEL surface is intentionally minimal for MVP — standard CEL operators (`in`, `exists`, `endsWith`, `startsWith`, `matches`, `size`) cover the vocabulary:

```cel
"Cargo.toml" in files                              // file presence
"src" in subdirs                                   // subdir presence
files.exists(f, f.endsWith(".tf"))                 // glob-like via stdlib
size(files) > 50                                   // many files
"Dockerfile" in files && "docker-compose.yml" in files
```

## Project-aware file search

Pass `--resolve-projects` (CLI) / `resolve_projects: true` (MCP) on the file `search` to populate two new CEL variables for every match:

- `project_types` — `list<string>` — names of every project type the containing project matches
- `project_type` — `string` — first (sorted) match; ergonomic for `==` queries

The walker resolves each file's nearest project ancestor by walking up the directory chain (cached per-dir, one ReadDir per unique directory visited).

```sh
# Find Go source files inside actual Go modules (excludes loose .go scripts)
file-search-on 'is_source && language == "go" && project_type == "go"' \
    --resolve-projects -d ~/Code

# Find Rust source NOT inside a Cargo project (e.g. ad-hoc scripts)
file-search-on 'is_source && language == "rust" && project_type == ""' \
    --resolve-projects -d ~/Code

# Find files inside multiple project types
file-search-on 'size(project_types) > 1' --resolve-projects -d ~/Code
```

Why opt-in? Resolution does extra I/O (one ReadDir per unique directory walked, cached). Tight CEL filters that don't reference project context shouldn't pay the cost.

## Out of scope (further follow-ups)

- **Project attributes** beyond names — `primary_language`, `package_manager`, `language_version`, etc. per detected project.
- **CEL helper functions** specific to directory context (`glob`, `has_file`, `has_subdir`) — covered by stdlib `exists` + string ops for MVP.
- **Standard config search paths** (`~/.config/file-search-on/project-types.yaml` auto-load) — explicit `--project-type-config` flag only.
- **Project-root-aware excludes** — auto-pruning `vendor/`, `node_modules/`, `target/` based on detected project type.
