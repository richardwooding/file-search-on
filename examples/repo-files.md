# Recipes — Repo files (Dockerfile, Makefile, LICENSE, manifests…)

file-search-on detects exact-name files alongside extension-matched ones. These are the build automation, repo metadata, ignore patterns, and dependency manifests that live in nearly every modern repo.

Per-type predicates (`is_dockerfile`, `is_makefile`, `is_license`, …) coexist with family predicates (`is_build`, `is_repo_meta`, `is_ignore`, `is_manifest`, `is_platform`) — the family fires automatically for every type in its family, mirroring how `is_image` covers every `image/*` subtype.

## Find by type

```sh
file-search-on 'is_dockerfile'                                    # Dockerfile, Containerfile
file-search-on 'is_makefile'                                      # Makefile (+ GNUmakefile, BSDmakefile)
file-search-on 'is_license'                                       # LICENSE, LICENCE, COPYING
file-search-on 'is_gitignore'                                     # .gitignore, .gitattributes
file-search-on 'is_dockerignore'                                  # .dockerignore
file-search-on 'is_gomod'                                         # go.mod, go.sum
file-search-on 'is_node_manifest'                                 # package.json, package-lock.json
file-search-on 'is_cargo_manifest'                                # Cargo.toml, Cargo.lock
file-search-on 'is_procfile'                                      # Procfile
file-search-on 'is_vagrantfile'                                   # Vagrantfile
```

## Find by family

Family predicates fire on the `content_type` name prefix. Use them when you want to scope a query at the family level without listing every type:

```sh
file-search-on 'is_build'                                         # any build automation file
file-search-on 'is_repo_meta'                                     # LICENSE, CHANGELOG, CONTRIBUTING, CODEOWNERS
file-search-on 'is_ignore'                                        # .gitignore, .gitattributes, .dockerignore
file-search-on 'is_manifest'                                      # go.mod, package.json, Cargo.toml, Pipfile, …
file-search-on 'is_platform'                                      # Procfile, Vagrantfile
```

## Light attribute extraction

### go.mod — `module` and `go_version`

```sh
# Find Go modules with a stale toolchain version
file-search-on 'is_gomod && go_version < "1.22"' -d ~/Code -o verbose

# Find all repos under a specific module path prefix
file-search-on 'is_gomod && module.startsWith("github.com/myorg/")' -d ~/Code
```

The parser is `golang.org/x/mod/modfile` — handles the full go.mod syntax (require / replace / exclude / retract blocks). Only the bare `module` and `go` directives are surfaced as CEL attributes; deeper parsing (require list, etc.) is a follow-up.

### Dockerfile — `base_image`

```sh
# Find Dockerfiles that base on Alpine
file-search-on 'is_dockerfile && base_image.startsWith("alpine")' -d ~/Code

# Anything still using a deprecated base
file-search-on 'is_dockerfile && base_image.contains("python:2")' -d ~/Code

# Multi-stage: only the FIRST FROM line is surfaced today
```

Multi-stage parsing (every FROM directive, exposed ports, ENV vars) is a follow-up.

## Precedence + cross-firing — exact name AND underlying format

The detector tries exact basenames first, then extension matches — so `content_type` is the most specific match (e.g. `manifest/node` for `package.json`, not generic `json`). But predicates aren't mutually exclusive: when an exact-name type is also a recognised syntactic format, **both predicates fire**. Mirrors the existing `is_image` / `is_jpeg`-style coexistence for image variants.

| File | `content_type` | All predicates that fire |
|---|---|---|
| `package.json` | `manifest/node` | `is_node_manifest`, `is_manifest`, `is_json` |
| `package-lock.json` | `manifest/node` | `is_node_manifest`, `is_manifest`, `is_json` |
| `Cargo.toml` | `manifest/cargo` | `is_cargo_manifest`, `is_manifest`, `is_toml` |
| `Cargo.lock` | `manifest/cargo` | `is_cargo_manifest`, `is_manifest`, `is_toml` |
| `requirements.txt` | `manifest/python-reqs` | `is_python_reqs`, `is_manifest`, `is_text` |
| `LICENSE` | `repo/license` | `is_license`, `is_repo_meta`, `is_text` |
| `CHANGELOG` | `repo/changelog` | `is_changelog`, `is_repo_meta`, `is_text` |
| `CONTRIBUTING` | `repo/contributing` | `is_contributing`, `is_repo_meta`, `is_text` |

So an agent looking for "any JSON file" via `is_json` picks up `package.json` automatically; "any plain-text file" via `is_text` picks up `LICENSE` and `requirements.txt`. The underlying-format-specific *attributes* (`json_kind`, `yaml_kind`) are NOT populated for exact-name types — `package.json` has `is_json=true` but `json_kind` stays empty. For a JSON-specific shape query, filter via `content_type == "manifest/node"` or `body.startsWith("{")` (with `--body`).

`Pipfile` (TOML) / `Pipfile.lock` (JSON) are intentionally NOT cross-fired today — they share one content type but have different underlying formats. Splitting them is a follow-up if needed.

## Combine with body filtering

The classic use case — narrow by family, refine by content:

```sh
# All GitHub Actions workflows (YAML in .github/workflows)
file-search-on 'is_yaml && body.contains("uses: actions/")' --body

# All Dockerfiles that install Node
file-search-on 'is_dockerfile && body.contains("npm install")' --body

# go.mod files declaring a specific dep
file-search-on 'is_gomod && body.contains("github.com/spf13/cobra")' --body
```

## OS-generated metadata files

`.DS_Store`, `.localized` (macOS), `Thumbs.db` / `ehthumbs.db` / `ehthumbs_vista.db` / `Desktop.ini` (Windows), and `.directory` (KDE) are first-class types under the `system/*` family. Detection only — the binary formats (`.DS_Store`, `Thumbs.db`) and INI formats (`Desktop.ini`, `.directory`) are not parsed for attributes today.

```sh
# Find every macOS leftover under ~/Code
file-search-on 'is_macos_metadata' -d ~/Code

# Find every Windows thumbnail cache under a shared drive
file-search-on 'is_thumbs_db' -d /mnt/shared

# Find every OS-cruft file across a tree (macOS + Windows + Linux)
file-search-on 'is_system_metadata' -d ~/Downloads

# Bulk delete with xargs (read-only tool itself; deletion via shell)
file-search-on 'is_system_metadata' -d ~/Downloads -o bare | xargs rm -i
```

| Filename(s) | Content type | Predicates that fire |
|---|---|---|
| `.DS_Store` | `system/macos-ds-store` | `is_ds_store`, `is_macos_metadata`, `is_system_metadata` |
| `.localized` | `system/macos-localized` | `is_localized`, `is_macos_metadata`, `is_system_metadata` |
| `Thumbs.db`, `ehthumbs.db`, `ehthumbs_vista.db` | `system/windows-thumbs-db` | `is_thumbs_db`, `is_windows_metadata`, `is_system_metadata` |
| `Desktop.ini` | `system/windows-desktop-ini` | `is_desktop_ini`, `is_windows_metadata`, `is_system_metadata` |
| `.directory` | `system/linux-directory` | `is_kde_directory`, `is_linux_metadata`, `is_system_metadata` |

Each OS-specific family (`is_macos_metadata` / `is_windows_metadata` / `is_linux_metadata`) AND the cross-OS `is_system_metadata` both fire — the family-prefix block in `setTypeFlags` evaluates them independently. `is_kde_directory` is named that way (rather than `is_directory`) to avoid the misleading "is this a directory" reading; `.directory` is the KDE Dolphin folder-properties file.

## Out of scope (today)

- LICENSE SPDX detection (e.g. "is this MIT?" via content fuzzy-match against known license texts)
- Dockerfile multi-stage / exposed ports / ENV / ARG
- Makefile target enumeration
- package.json `name` / `version` / `scripts` / `dependencies`
- Cargo.toml package metadata
- Pipfile / Gemfile / requirements.txt parsed package list
- Parsing the binary `.DS_Store` and `Thumbs.db` formats (proprietary Apple / Microsoft layouts)
- Parsing the INI-shaped `Desktop.ini` / `.directory` (IconResource, [.ShellClassInfo], etc.) for attributes
