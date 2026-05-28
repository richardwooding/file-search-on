# Slides — file-search-on talk

A Marp deck for a ~15 minute Show-&-Tell talk on `file-search-on`.

## Files

- `deck.md` — the slides. Speaker notes are inline as HTML comments.
- `demo.sh` — the live commands, one block per demo slide.
- `scripts/build_semantic_corpus.sh` — generates the Demo 2 corpus
  (12 markdown notes in `~/Documents/semantic-demo/`). Run once before
  the talk.
- `scripts/build_ocr_corpus.sh` — generates the Demo 4 OCR corpus
  (12 synthetic text-bearing JPGs in `~/Pictures/ocr-demo/`). Run once
  before the talk. Requires ImageMagick.
- `README.md` — you are here.

## Pre-talk checklist

1. `brew install marp-cli imagemagick ollama` (renderer + image builder + embeddings).
2. Pull the embedding model once: `ollama pull nomic-embed-text`.
3. Make sure Ollama is running: `ollama serve &` (or open the Ollama app).
4. Build the semantic corpus: `./scripts/build_semantic_corpus.sh`
5. Build the OCR corpus: `./scripts/build_ocr_corpus.sh`
6. Confirm the SA photo corpus is still at `~/Pictures/south-africa-holiday/` (66 GPS-tagged JPGs).
7. `rm -f /tmp/ocr.db /tmp/semantic.db` (wipe the caches so the cold/warm timing demos land).
8. Have a Claude Code / Claude Desktop window with `file-search-on mcp` registered, sitting on a blank prompt for Demo 6.

## Render

The deck uses [Marp](https://marp.app). Install once:

```sh
brew install marp-cli       # or: npm i -g @marp-team/marp-cli
```

Then render or preview:

```sh
marp deck.md --pdf          # → deck.pdf
marp deck.md --html         # → deck.html
marp deck.md --pptx         # → deck.pptx
marp -s .                   # local preview server (auto-reloads on edit)
```

`marp -s .` (the server mode) is the most useful when iterating on the
deck — it watches the directory and live-reloads on save.

## Speaker notes

The script for each slide lives in `<!-- SCRIPT: ... -->` comments. Marp's
HTML / PPTX renders surface them as presenter notes. For PDF, install the
[VS Code Marp extension](https://marketplace.visualstudio.com/items?itemName=marp-team.marp-vscode)
and use "Marp: Show Preview" to see slides + notes side-by-side while
practising.

## Running the demos

Drive `demo.sh` line-by-line rather than `bash demo.sh` — each block is
its own beat in the talk. Two tips:

1. Run the talks's terminal at a **bigger font** than your IDE
   (`Cmd-+` ×2 in Terminal.app / iTerm2). The default is unreadable
   from row 5.
2. Pre-stage the photo corpus (`~/Pictures/south-africa-holiday/`) and
   the file-search-on binary on `PATH` before going live.

## Timing

| Section | Time |
| --- | --- |
| Slides 1-2 (intro) | ~1m |
| Slides 3-5 (problem + pitch) | ~2m |
| Demo 1 (Go codebase tour) | ~2m |
| Demo 2 (semantic search over docs) | ~2m |
| Demo 3 (SA photos, bbox + polygon) | ~2.5m |
| Demo 4 (OCR over images) | ~2m |
| Demo 5 (visual similarity / phash) | ~1.5m |
| Demo 6 (MCP / agent) | ~2.5m |
| Slides 11-13 (under the hood / learned / open) | ~3m |
| Summary + Q&A | ~2m + open |

Total floor: ~20-21 minutes plus questions.
