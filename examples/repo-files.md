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

## Precedence — exact name beats extension

The detector tries exact basenames first, then extension matches. Practical consequences:

- `package.json` → `content_type=manifest/node` (NOT generic `json`). It still has `is_json=false` and no `json_kind`. If you want generic JSON queries to include it, use `content_type == "manifest/node" || is_json` or filter on the body.
- `Cargo.toml` → `content_type=manifest/cargo` (no separate `toml` content type exists; this is strictly an improvement).
- `requirements.txt` → `content_type=manifest/python-reqs` (NOT `text`). Same body-filter approach applies if you want text-style queries.

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

## Out of scope (today)

- LICENSE SPDX detection (e.g. "is this MIT?" via content fuzzy-match against known license texts)
- Dockerfile multi-stage / exposed ports / ENV / ARG
- Makefile target enumeration
- package.json `name` / `version` / `scripts` / `dependencies`
- Cargo.toml package metadata
- Pipfile / Gemfile / requirements.txt parsed package list
