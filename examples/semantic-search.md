# Semantic similarity search

Word-level matching (`body.contains("Q4 revenue")`) is brittle: it misses documents that say "fourth-quarter earnings" or "FY25 financials". The **`similarity`** CEL variable closes that gap — every walked file gets a cosine score (0-1) against a natural-language query, computed via a local Ollama embedding model. Filter, rank, and compose with the rest of the CEL vocabulary.

## Setup (one-time)

1. **Install Ollama** — <https://ollama.com>. On macOS: `brew install ollama`. Then `ollama serve` (runs as a background daemon).

2. **Pull an embedding model.** Smaller models are faster but slightly less accurate. Pick one:
   ```sh
   ollama pull nomic-embed-text       # ~270 MB, 768 dims — fast default
   ollama pull mxbai-embed-large      # ~670 MB, 1024 dims — best quality
   ollama pull bge-large-en-v1.5      # ~670 MB, 1024 dims — competitive
   ```

3. **Confirm it's pulled**:
   ```sh
   ollama list
   ```

The MCP server / CLI boots fine WITHOUT Ollama running — the connection is lazy. You'll only see an error when the first semantic search runs.

## CLI

```sh
# Find docs about revenue projections — sorted by similarity desc
file-search-on \
  --semantic-query "Q4 revenue forecast" \
  --embedding-model nomic-embed-text \
  -d ~/Documents

# Tighter threshold; PDFs only
file-search-on 'is_pdf' \
  --semantic-query "tax forms and IRS correspondence" \
  --embedding-model nomic-embed-text \
  --similarity-threshold 0.7 \
  -d ~/Documents

# Cache embeddings — first run is slow, second run is fast
file-search-on \
  --semantic-query "machine learning notes" \
  --embedding-model nomic-embed-text \
  --index-path /tmp/sem.idx \
  -d ~/Documents
# Second run: same query → vectors served from cache
file-search-on \
  --semantic-query "machine learning notes" \
  --embedding-model nomic-embed-text \
  --index-path /tmp/sem.idx \
  -d ~/Documents

# Compose with the rest of the CEL vocabulary
file-search-on 'is_office && author == "Alice" && similarity > 0.6' \
  --semantic-query "budget projections" \
  --embedding-model nomic-embed-text \
  -d ~/work
```

## MCP

```json
{
  "name": "mcp__file-search-on__search_semantic",
  "arguments": {
    "query": "Q4 revenue forecast",
    "dir": "/Users/me/docs",
    "threshold": 0.7,
    "limit": 20,
    "expr": "is_pdf || is_office"
  }
}
```

**MCP server startup** — pass the default model so per-call requests don't have to:

```sh
file-search-on mcp --embedding-model nomic-embed-text
```

Per-call `model` / `embedding_server` inputs override the startup defaults. If neither is set, `search_semantic` returns `"no embedding model configured"`.

**Pointing at a non-default Ollama**: both the CLI (`search --embedding-server`) and the MCP server (`mcp --embedding-server`) honour the standard `OLLAMA_HOST` env var as a fallback. Resolution order: explicit `--embedding-server` flag → `$OLLAMA_HOST` → `http://localhost:11434`. Useful for a remote Ollama box (`export OLLAMA_HOST=http://gpu-box:11434`) without having to pass the flag every invocation.

## How it works

1. The query is embedded ONCE per call (one Ollama round-trip).
2. The walker visits each file matching the CEL pre-filter.
3. For each file:
   - **Cache hit** (`index.Entry.Vector` populated and `(size, mtime)` validates) — just compute the cosine, no Ollama call.
   - **Cache miss** — extract the body (via the existing body cache from PR #142), embed via Ollama, L2-normalise, store the vector, compute the cosine.
4. Results sort by `similarity` desc and apply the threshold filter.

The vector cache is the headline perf win: repeat queries against an unchanged tree avoid ~50-200ms per file of Ollama-call latency. `index_stats` surfaces `embed_hits` / `embed_misses` so you can audit the hit rate.

## Threshold guidance

| Threshold | Behaviour |
|---|---|
| 0.8+ | Very strict; near-perfect topical match. Use for high-precision searches against well-organised corpora. |
| 0.7 | Strict; documents clearly on-topic. Good default for "I'm looking for documents about X". |
| 0.5 | Default. Catches related-but-tangential content alongside on-topic matches. |
| 0.3-0.4 | Loose; surfaces weakly-related documents. Useful for exploration / discovery workflows. |
| 0.0 | Returns everything sorted by similarity. Equivalent to `--limit N` for top-N nearest. |

Cosine similarity is in [-1, 1] for unnormalised vectors, but Ollama returns embeddings in a roughly-positive cone for most models so practical scores cluster in [0, 1] with rare dips below 0 for very dissimilar content.

**The threshold is a CEL clause, not a separate post-filter.** `--similarity-threshold V` (CLI) / `threshold` (MCP) is implemented by AND-ing `similarity >= V` onto your expression. So it *combines* with any `similarity` comparison you write — `--semantic-query … 'similarity > 0.6'` with the default `--similarity-threshold 0.5` yields `(similarity > 0.6) && (similarity >= 0.5)`, an effective floor of 0.6. Set `--similarity-threshold 0` to defer entirely to your own `similarity` predicate (or to rank-only with `--limit`).

## Recipes

### Discovery — explore a tree by topic

```sh
file-search-on \
  --semantic-query "machine learning research" \
  --embedding-model nomic-embed-text \
  --similarity-threshold 0.4 \
  --limit 30 \
  -d ~/Documents -o json | jq -r '.[].path'
```

### Forensic + semantic combination

The forensic surface (#143-#146) composes naturally with semantic search. Find suspicious-looking documents about a specific topic:

```sh
file-search-on '!is_known_good && similarity > 0.6' \
  --semantic-query "credit card numbers and PII" \
  --embedding-model nomic-embed-text \
  --hash-allowlist nsrl.hashset \
  -d /Volumes/Evidence
```

### Code search by intent

```sh
# Find Go files implementing rate limiting / throttling concepts
file-search-on 'is_source && language == "go" && similarity > 0.55' \
  --semantic-query "rate limiting middleware token bucket" \
  --embedding-model nomic-embed-text \
  -d ./src
```

### Sort + take top-K

```sh
# Top 10 most-relevant photos by EXIF caption / title
file-search-on 'is_image && similarity > 0' \
  --semantic-query "mountain landscape sunset" \
  --embedding-model nomic-embed-text \
  --limit 10 \
  -d ~/Pictures
```

Note: image files only have semantic-search-friendly content when they carry text metadata (EXIF caption, title, ImageDescription). Images without those fall back to similarity=0.

## Performance notes

- **First call against a tree**: dominated by Ollama embedding latency. ~50-200ms per file depending on body length + model size. A 1000-file tree with `nomic-embed-text` typically completes in 1-3 minutes cold.
- **Repeat calls against unchanged trees**: vector cache hits skip Ollama entirely. The bottleneck moves to body extraction (which is itself cached from PR #142). Sub-second for the same 1000-file tree.
- **Different queries against the same tree**: query embedding is one extra Ollama call (~50ms), per-file work is a dot product (microseconds). Sub-second after the first warm-up.
- **Switching models** (e.g. `nomic-embed-text` → `mxbai-embed-large`): each cache entry records which model produced its vector. The next walk under the new model detects the mismatch, re-embeds, and stamps the new model name — no manual rebuild needed, no silently-wrong scores. The `embed_model_mismatches` counter in `index_stats` reports how many files were re-embedded vs. genuinely missed. Switching back and forth between models is correct but not free: each switch re-embeds the touched files.

## Out of scope (for now)

- **Bundled embedding model** — too heavy (600 MB+). Ollama lets the user pick.
- **HNSW / FAISS indexes** — brute-force pairwise cosine is fast enough for tens of thousands of files. File a follow-up if you hit a scale where it isn't.
- **Cross-encoder reranking** — bi-encoder (single-embedding) is enough for v1.
- **Per-chunk embeddings for long documents** — v1 embeds the first ~1 MiB of body (the same cap as the body cache). Long-doc retrieval with chunked vectors is a follow-up.
- **OpenAI / llama.cpp / other embedding sources** — same wire shape; future plugin point. v1 is Ollama-only.
- **Query expansion / synonym handling** — embeddings already handle paraphrase implicitly.
