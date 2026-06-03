# Slides — file-search-on talk

A Marp deck for a ~15 minute Show-&-Tell talk on `file-search-on`.

## Files

- `deck.md` — the slides. Speaker notes are inline as HTML comments.
- `demo.sh` — the live commands, one block per demo slide.
- `scripts/build_semantic_corpus.sh` — generates the Demo 2 corpus
  (12 markdown notes in `~/Demo/semantic-demo/`). Run once before
  the talk.
- `scripts/build_ocr_corpus.sh` — generates the Demo 4 OCR corpus
  (12 synthetic text-bearing JPGs in `~/Demo/ocr-demo/`). Run once
  before the talk. Requires ImageMagick.
- `README.md` — you are here.

## Pre-talk checklist

1. `brew install marp-cli imagemagick ollama` (renderer + image builder + embeddings).
2. Pull the embedding model once: `ollama pull nomic-embed-text`.
3. Make sure Ollama is running: `ollama serve &` (or open the Ollama app).
4. Build the semantic corpus: `./scripts/build_semantic_corpus.sh`
5. Build the OCR corpus: `./scripts/build_ocr_corpus.sh`
6. Confirm the SA photo corpus is still at `~/Demo/south-africa-holiday/` (66 GPS-tagged JPGs).
7. Wipe the default on-disk index so the cold/warm timing demos land:
   `rm -rf "$(go env GOOS | grep -q darwin && echo ~/Library/Caches/file-search-on || echo ~/.cache/file-search-on)"`.
   (The default index moved to the OS cache location in #243; `/tmp/ocr.db` / `/tmp/semantic.db` are no longer used.)
8. Confirm Demo 1's `hot_files` preset has populated git history — `git -C ~/Code/Personal/file-search-on log --oneline | head` should print recent commits.
9. Have a Claude Code / Claude Desktop window with `file-search-on mcp` registered, sitting on a blank prompt for Demo 6.
10. For Demo 7, leave the Demo 6 MCP server running — its dashboard URL is what `file-search-on monitors` will surface.

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
2. Pre-stage the photo corpus (`~/Demo/south-africa-holiday/`) and
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
| Demo 7 (monitor dashboard) | ~2m |
| Slides 12-14 (under the hood / learned / open) | ~3m |
| Summary + Q&A | ~2m + open |

Total floor: ~22-25 minutes plus questions.
