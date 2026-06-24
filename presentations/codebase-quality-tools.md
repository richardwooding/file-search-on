---
marp: true
paginate: true
size: 16:9
title: "Making a Codebase Better with file-search-on"
author: Richard Wooding
---

<style>
:root {
  --span-orange: #F7941D;
  --span-cream:  #FFF5E6;
  --span-dark:   #333333;
  --span-gray:   #4A4A4A;
}

section {
  background: var(--span-cream);
  color: var(--span-dark);
  font-family: Helvetica, Arial, sans-serif;
  font-size: 26px;
  padding: 60px 80px 60px 70px;
  border-right: 20px solid var(--span-orange);
}

section h1 {
  color: var(--span-dark);
  font-size: 46px;
  border-bottom: 4px solid var(--span-orange);
  padding-bottom: 12px;
  margin-bottom: 24px;
}

section h2 {
  color: var(--span-orange);
  font-size: 34px;
  margin-bottom: 10px;
}

section h3 {
  color: var(--span-gray);
  font-size: 24px;
  margin: 6px 0;
}

section strong { color: var(--span-orange); }

section code {
  background: #fff;
  color: var(--span-dark);
  border: 1px solid #e8d9c0;
  border-radius: 4px;
  padding: 1px 6px;
  font-size: 0.9em;
}

section pre {
  background: #fff;
  border-left: 5px solid var(--span-orange);
  border-radius: 6px;
  font-size: 20px;
}

section pre code { border: none; background: transparent; }

section table {
  font-size: 22px;
  border-collapse: collapse;
  width: 100%;
}
section th {
  background: var(--span-orange);
  color: #fff;
  text-align: left;
  padding: 8px 12px;
}
section td {
  border-bottom: 1px solid #e8d9c0;
  padding: 7px 12px;
}

section a { color: var(--span-orange); }

section.lead, section.title {
  border-right: none;
  display: flex;
  flex-direction: column;
  justify-content: center;
}
section.title h1 {
  font-size: 60px;
  border-bottom: none;
}
section.title h1::after {
  content: "";
  display: block;
  width: 140px;
  height: 8px;
  background: var(--span-orange);
  margin-top: 20px;
}

section .badge-cli, section .badge-mcp {
  display: inline-block;
  font-size: 15px;
  font-weight: bold;
  border-radius: 4px;
  padding: 1px 8px;
  vertical-align: middle;
}
section .badge-cli { background: var(--span-dark); color: #fff; }
section .badge-mcp { background: var(--span-orange); color: #fff; }

section footer { color: var(--span-gray); font-size: 14px; }
section::after { color: var(--span-gray); }
</style>

<!-- _class: title -->
<!-- _paginate: false -->

# Making a Codebase Better with file-search-on

### Code-quality tooling — CLI **&** MCP

Richard Wooding · Span Digital

---

# The idea

`file-search-on` started as a **content-type-aware file search** — query files by *typed attributes* with a CEL expression, not just filenames or grep.

That same engine now ships a layer of **codebase-health tools**: dead code, complexity, duplication, test gaps, call-graph navigation, churn.

Two ways to reach them:

- <span class="badge-cli">CLI</span> &nbsp;ad-hoc, in your terminal, in CI
- <span class="badge-mcp">MCP</span> &nbsp;an agent (Claude) calls them while it works

> Legend used throughout: <span class="badge-cli">CLI</span> = `file-search-on` subcommand · <span class="badge-mcp">MCP</span> = tool exposed over `file-search-on mcp`

---

# Six ways to make a codebase better

1. **Cut dead weight** — unused code & duplication
2. **Tame complexity** — hotspots & coupling
3. **Close test gaps** — untested & under-tested code
4. **Navigate with confidence** — the call graph
5. **Manage risk** — churn, ownership, API drift
6. **Search & watch** — find anything, get told when it changes

---

# 1 · Cut dead weight

Every tool here is on **both** surfaces — CLI subcommand *and* MCP tool.

| Tool | Surface | What it finds |
|------|---------|---------------|
| `dead-code` | <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> | Unreferenced functions/types |
| `unused-exports` | <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> | Public API nobody imports |
| `duplicate-functions` | <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> | Copy-pasted logic → extract a helper |
| `near-duplicates` | <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> | Similar files (SimHash) — template copies, drifted forks |
| `duplicates` | <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> | Byte-identical files (sha256) |

```sh
file-search-on dead-code -d .
file-search-on unused-exports -d .
file-search-on duplicate-functions -d ./internal
file-search-on near-duplicates 'is_source' -d .
```

---

# 2 · Tame complexity

**`complexity`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — rank functions by cyclomatic complexity. The refactor backlog, sorted by where it hurts.

**`coupling`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — afferent/efferent coupling + instability (Martin metrics). The fragile-hub seams. *(Go, Rust, JVM, C#, Python, JS/TS, PHP — selected by build manifest.)*

```sh
file-search-on complexity 'is_source && language=="go"' --top 10
file-search-on coupling -d . --top 20
```

> Pair with `churn-owners` (§5 · Manage risk): *complex* **and** *frequently changed* = your top refactor candidate.

---

# 3 · Close test gaps

**`test-gaps`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — source files/functions with **no corresponding test**. No instrumentation needed.

**`coverage-gaps`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — cross-reference a Go coverage profile to surface functions below a threshold.

```sh
file-search-on test-gaps -d .
go test -coverprofile=cover.out ./...
file-search-on coverage-gaps cover.out --threshold 0.8
```

The payoff: write tests where they **matter**, not where they're easy.

---

# 4 · Navigate with confidence

The call-graph family — every one is <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span>, all built on the same symbol index:

| Tool | Answers |
|------|---------|
| `find-definition` | Where is X defined? |
| `references` | Everywhere X is used |
| `who-calls` / `calls` | Callers of X / callees of X |
| `call-path` | How does A reach B? |
| `impact` | What breaks if I change X? |
| `code-graph` | The whole dependency picture |
| `imported-by` | Who depends on this package? |

```sh
file-search-on impact BuildCodeGraph -d .      # blast radius
file-search-on call-path Run BuildCodeGraph    # the route A→B
```

> Before a refactor: **`impact`** turns "I think this is safe" into "here is exactly what's affected."

---

# 5 · Manage risk

**`churn-owners`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — git-aware ownership / bus-factor per directory. Find single-maintainer hotspots.

**`api-diff`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — removed exported symbols between two trees. A release gate for breaking changes.

**`diff`** <span class="badge-cli">CLI</span> · **`diff_trees`** <span class="badge-mcp">MCP</span> — cross-tree set operations by content hash.

```sh
file-search-on churn-owners --expr is_source -d .
file-search-on api-diff ./v1 ./v2 --expr 'is_source && language=="go"'
```

---

# 6 · Search & watch

**`search`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — the core. CEL over typed attributes:

```sh
file-search-on 'is_source && language == "go" && function_count > 20'
file-search-on 'is_source && body.matches("TODO|FIXME|HACK")'
```

- **semantic search** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — `search --semantic-query` (CLI) / `search_semantic` (MCP), embeddings via `all-minilm`
- **`find-matches`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — regex with before/after context
- **`validate`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> · **`playground`** <span class="badge-cli">CLI</span> — author & test CEL before you walk
- **`watch`** <span class="badge-cli">CLI</span> <span class="badge-mcp">MCP</span> — *tell me when a matching file appears*

---

# Under the hood · one symbol graph

Most quality tools share a single pass — `BuildCodeGraph` walks the tree once and builds a cross-file **import + definition + reference** graph. Every query is then a lookup over that graph.

**Symbol extraction, per language:**

- **Go** → the standard library's `go/ast` — exact, no external grammar
- **16 other languages** (Rust, TypeScript, JavaScript, Python, Java, C, C++, C#, Ruby, Swift, Kotlin, Scala, PHP, Perl, R, MATLAB) → **pure-Go tree-sitter** (`gotreesitter`), grammars embedded at build time

> Default matching is **name-based** (fast, language-agnostic). Go adds opt-in **type resolution** (`--resolve`) so same-named methods on different types don't conflate — exactness when you need it.

---

# Under the hood · the algorithms

| Tool | Technique |
|------|-----------|
| `complexity` | Cyclomatic complexity over each function's AST span |
| `coupling` | Martin metrics — Ca / Ce, instability `I = Ce / (Ca+Ce)` *(7 ecosystems, by build manifest)* |
| `circular` | Tarjan strongly-connected components over the import graph |
| `who-calls` · `impact` · `call-path` | BFS over the call graph (reverse / forward / shortest-path) |
| `dead-code` · `unused-exports` | Invert the reference graph — what nothing points at |
| `test-gaps` | Reference graph filtered to `is_test_file` callers |
| `coverage-gaps` | Parse a Go `-coverprofile`, fold coverage onto function spans |
| `churn-owners` | git authorship aggregated per directory |
| `duplicate-functions` · `near-duplicates` | **SimHash** fingerprints (fuzzy); `duplicates` uses **sha256** (exact) |

> All of it filters through **CEL** (`cel-go`) over typed attributes, and a `(size, mtime)`-validated **bbolt** index makes repeat runs near-instant.

---

# Two surfaces, one engine

Every analysis tool is a thin wrapper over the **same** `internal/search` / `celexpr` functions — reachable both ways. **Near-total parity.**

### <span class="badge-cli">CLI</span> — you drive
Terminal, scriptable, CI gates. `dead-code`, `complexity`, `impact`, `test-gaps`, `validate` — same engine, no agent needed.

### <span class="badge-mcp">MCP</span> — the agent drives
`file-search-on mcp` exposes **40 tools** to Claude, which runs the very same checks *while reasoning* about your change.

> Pick the surface, not the capability: a human at a prompt and an agent mid-task reach identical analysis.

---

<!-- _class: title -->
<!-- _paginate: false -->

# A healthier codebase, on demand

### Find the rot · understand the impact · test what matters

`go run ./cmd/file-search-on mcp` &nbsp;·&nbsp; `brew install richardwooding/tap/file-search-on`

Richard Wooding · Span Digital
