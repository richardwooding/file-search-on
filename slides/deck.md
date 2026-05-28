---
marp: true
theme: default
paginate: true
size: 16:9
title: file-search-on
author: Richard Wooding
style: |
  section {
    font-size: 26px;
    padding: 50px 60px;
    line-height: 1.4;
  }
  section h1 { font-size: 56px; }
  section h2 { font-size: 36px; margin-bottom: 0.35em; }
  section h3 { font-size: 28px; }
  pre, code {
    font-size: 17px;
    line-height: 1.35;
  }
  pre {
    padding: 0.6em 0.8em;
    margin: 0.4em 0;
  }
  table { font-size: 22px; }
  th, td { padding: 0.3em 0.6em; }
  ul, ol { margin: 0.3em 0; }
  li { margin: 0.15em 0; }
  blockquote { font-size: 24px; margin: 0.4em 0; }
  section.lead { font-size: 32px; }
  section.lead h1 { font-size: 72px; }
---

<!-- _class: lead -->

# `file-search-on`

### A content-type-aware file search, in CEL

Richard Wooding — Span Digital — 2026

<!--
SCRIPT:
Good [morning / afternoon] everyone, and thanks for the time.

I'm Richard Wooding, I'm a staff engineer at Span Digital. Over the next
fifteen minutes I'm going to show you a tool I've been building called
file-search-on — what it is, why it exists, and what surprised me along
the way.

Hold questions to the end — there'll be a Q&A — but please flag me if
anything on screen is hard to read.
-->

---

## Who I am

- Staff engineer at Span Digital — we build content and dev tooling
- I scratch my own itches in Go
- Background: search, indexing, content systems
- Today's project lives at **github.com/richardwooding/file-search-on**

<!--
SCRIPT:
Quick "who am I" — I'm a staff engineer at Span Digital, where we work
on content platforms and developer tooling. I write Go for fun and for
work.

Most of my projects start the same way: I have a question I want to ask
of my filesystem, my tools won't answer it cleanly, and I get annoyed
enough to write code. file-search-on is the latest one.

It's open source, on GitHub at richardwooding/file-search-on. I'll
share that link again at the end.
-->

---

## What you'll see today

1. The **problem** — what `find`, `grep`, and `glob` cannot answer
2. The **pitch** — one-line CEL expressions over typed file attributes
3. Six live **demos**:
   1. **Touring a Go codebase** — the file-search-on repo itself
   2. **Semantic search** over a docs folder (local Ollama embeddings)
   3. A photo corpus by camera, by GPS bbox, by polygon
   4. Finding **text inside images** with OCR
   5. Finding **visually similar photos** by perceptual hash
   6. An AI agent driving file-search-on through MCP
4. What I **learned** and what's still **open**

<!--
SCRIPT:
Here's the shape of the next twelve minutes. I'll set up the problem,
give you the elevator pitch, then run three short demos — a CLI tour, a
photo-by-GPS query, and an AI agent driving file-search-on through the
Model Context Protocol. I'll close with what surprised me building it,
plus a couple of open issues I haven't cracked yet.

Three demos sounds like a lot for fifteen minutes — they're short and I
won't be typing live. The commands are scripted; I'll talk you through
what each one does as it runs.
-->

---

## The problem

| Question | Tool | Works? |
| --- | --- | --- |
| "Find files named `*.go`" | `find` / glob | yes |
| "Find files **containing** the word panic" | `grep` | yes |
| "Find **PDFs with more than 10 pages**" | — | no |
| "Find **photos taken in Cape Town**" | — | no |
| "Find **MP3s with a missing artist tag**" | — | no |
| "Find **ARM64 binaries** over 100 MB" | — | no |

`find` knows about **paths**. `grep` knows about **bytes**. Neither knows what the file *is*.

<!--
SCRIPT:
Here's the gap that bothered me.

Find and glob are great when you can describe the file by its name —
"give me everything ending in .go". Grep is great when you can describe
the file by its bytes — "give me lines mentioning the word panic".

But a huge class of useful questions don't fit either shape. "Show me
PDFs longer than ten pages." "Show me photos taken inside this GPS
polygon." "Show me MP3s where the artist tag is empty." "Show me ARM64
Mach-O binaries over a hundred megs."

These are all questions about *what the file is* — its typed attributes,
its semantic identity. There was no clean way to ask them on the command
line, so I wrote one.
-->

---

## The pitch

`file-search-on` evaluates a **CEL expression** against **typed file attributes**:

```sh
file-search-on 'is_image && iso > 1600 && gps_lat != 0.0'
file-search-on 'is_pdf && page_count > 10 && word_count > 5000'
file-search-on 'is_source && language == "go" && loc > 200'
```

- **50+ content types** detected by extension + magic bytes
- **200+ attributes** — EXIF, ID3, page counts, video codecs, archive entries, source LOC, binary architectures
- **CEL** — Google's expression language, declarative, sandboxed, fast
- **MCP server** — 20 tools so AI agents get the same query surface

<!--
SCRIPT:
The pitch fits on one slide. file-search-on runs a CEL expression — CEL
is Common Expression Language, Google's safe little expression DSL — over
typed attributes the tool extracts from each file.

So instead of grepping bytes, I write a one-liner that says "is this
file an image, AND was the ISO above sixteen hundred, AND does it have
GPS coordinates". That's a real query I run. Or I might ask for PDFs
over ten pages and over five thousand words — long reads. Or Go source
files with more than two hundred lines of code.

Under the hood there are about fifty content types — markdown, PDF,
image, audio, video, office, all the source-code languages, archive
formats, executables, even disk images and email — and about two
hundred attributes you can filter and sort on. Roughly anything you'd
get out of EXIF, ID3, file headers, you can put in your where-clause.

And it also runs as a Model Context Protocol server, so AI agents like
Claude Code get exactly the same surface through twenty MCP tools. That
last part is the bit that excited me most, and I'll come back to it.
-->

---

## Demo 1 — touring a Go codebase

The corpus is the file-search-on repo itself — dogfooding.

**1. Parse `go.mod` — module + Go version**

```sh
file-search-on attrs ~/Code/Personal/file-search-on/go.mod
```

**2. Top-5 largest Go files by LOC**

```sh
file-search-on -d ~/Code/Personal/file-search-on \
  'is_source && language == "go" && loc > 300' \
  --sort loc --order desc --limit 5
```

**3. TODO / FIXME in production Go (skip test fixtures)**

```sh
file-search-on find-matches '//\s*(TODO|FIXME)\b' \
  --expr 'is_source && language == "go" && !is_test_file' \
  -C 1 --prune-build-artefacts \
  -d ~/Code/Personal/file-search-on
```

<!--
SCRIPT:
First demo — touring a Go codebase. I'm pointing file-search-on at its
own source tree, which is about ten thousand lines of Go. Three
commands.

[run: file-search-on attrs ~/Code/Personal/file-search-on/go.mod]

First — the `attrs` subcommand on go.mod. file-search-on detects this
file as a Go module manifest, parses it, and surfaces the module name
and the required Go version. Five lines of output, no flags, just a
path. That `manifest/gomod` content type is one of about fifty the tool
recognises.

This is the vocabulary I write queries in, and the point is: it's the
tool telling me what's queryable, not docs that drift.

[run: top-5 Go files by LOC]

Second — top five Go files in the repo by lines of code. CEL expression
filters to Go source over three hundred lines; sort_by loc descending;
limit five. The biggest files come back. Notice what's NOT there: no
language-specific tooling, no "if Go then count LOC" code in the
command. The content-type detector identifies each file as Go, the LOC
counter runs as part of the attribute pass, and CEL sorts on the
attribute name. Same surface I'd use for a Python or Rust codebase.

[run: find-matches TODO/FIXME with !is_test_file]

Third — every TODO and FIXME comment in production Go source. The
pattern is a regex matching slash-slash optionally followed by
TODO-or-FIXME. The pre-filter is is_source and language equals go AND
NOT is_test_file. The is_test_file predicate is true for any source
file matching the per-language test convention — *_test.go in Go,
test_*.py in Python, and so on. With the dash-C-one flag, we get one
line of context above and below each match.

[pause for the result]

Zero hits. The codebase has no actual TODOs in production code. If I
drop the !is_test_file filter, ten matches come back — all in
*_test.go files, all of them string fixtures for the find-matches tests
themselves. Which is itself a fun bit of recursion: a tool that finds
TODOs has TODO strings in its tests for the TODO-finding tests. But
that's the kind of distinction the is_test_file predicate makes easy.
-->

---

## Demo 2 — semantic search over a docs folder

12 short technical notes in `~/Documents/semantic-demo/`. Find the
right document when the query *paraphrases* the file.

**Plain English query — no keyword overlap with the matching file**

```sh
file-search-on search -d ~/Documents/semantic-demo \
  --semantic-query "container orchestration deployment strategies" \
  --embedding-model nomic-embed-text --similarity-threshold 0.50
```

→ `k8s-scheduling.md`. None of those words appear in the file:

```sh
grep -rli 'orchestration\|container\|deployment strateg' ~/Documents/semantic-demo
# (nothing)
```

**A second query — warm cache, sub-second**

```sh
file-search-on search -d ~/Documents/semantic-demo \
  --semantic-query "what happens when two writers update the same record" \
  --embedding-model nomic-embed-text --similarity-threshold 0.50
```

→ `transaction-isolation.md`.

<!--
SCRIPT:
Demo two — semantic search.

The corpus is twelve short technical notes I dropped in
~/Documents/semantic-demo. Things like an incident post-mortem, a Go
GC tuning note, a Mongo migration write-up, kubernetes pod scheduling,
TLS handshake, transaction isolation. Real-shaped writing, eight
hundred to a thousand bytes each.

[run: the kubernetes query]

The query is "container orchestration deployment strategies". Watch:
file-search-on returns k8s-scheduling.md. Here's the trick.

[run: the grep command]

That same query phrasing — "container", "orchestration", "deployment
strategies" — appears NOWHERE in any file in the corpus. Grep returns
nothing. Zero matches. The k8s-scheduling.md file is talking about
node selectors, affinity rules, taints and tolerations. None of the
query words appear in it.

Semantic search found it because the EMBEDDING vector of the document
sits close to the embedding vector of the query. That's the entire
point of semantic search versus keyword search: similarity in meaning
space, not character space.

[run: the transaction query]

Second query — "what happens when two writers update the same record".
That's a question, not a search term. The top hit is
transaction-isolation.md, which is exactly the file talking about
READ COMMITTED, REPEATABLE READ, SERIALIZABLE, and what postgres does
when two transactions touch the same row. Again, no overlap between
the question words and the matching document.

A few notes on the stack. The embeddings come from a local Ollama
instance running the nomic-embed-text model. No cloud, no API keys, no
data leaving your laptop. The embeddings cache per file by size and
mtime, so the first walk does the work — about a second and a half
for these twelve documents — and every subsequent query is sub-second.
And the similarity score is exposed as a CEL variable so you can
compose it with the rest of the query — "is_pdf AND similarity is
above zero point seven" works.

This isn't a separate product mode. It's a flag on the existing
search tool.
-->

---

## Demo 3 — the South Africa photo corpus

This morning I curated 66 geotagged South African photos from Wikimedia Commons.

**Histogram by camera**

```sh
file-search-on stats -d ~/Pictures/south-africa-holiday \
  'is_image' --group-by camera_make
```

**Bounding box — central Cape Town (rectangle) → 5 photos**

```sh
file-search-on -d ~/Pictures/south-africa-holiday \
  'is_image && gps_lat > -33.96 && gps_lat < -33.7
            && gps_lon >  18.3 && gps_lon <  18.7'
```

**Polygon — the Cape Peninsula (any shape) → 12 photos**

```sh
file-search-on -d ~/Pictures/south-africa-holiday \
  'is_image && point_in_polygon(gps_lat, gps_lon,
       [-33.85, 18.30,  -33.85, 18.55,
        -34.15, 18.55,  -34.40, 18.50,
        -34.40, 18.32])'
```

<!--
SCRIPT:
Demo three — photos. This is where typed search really pays off.

A bit of context: I built a small test corpus this morning of geotagged
South African photos. Sixty-six images, all Creative Commons, all with
GPS coordinates in EXIF. I'll talk about how I built it in a minute.

[run: file-search-on stats -d ... 'is_image' --group-by camera_make]

First command — bucket the photos by camera make. Canon's the biggest
group, then Sony, then Panasonic. Notice nobody asked me to install an
EXIF parser or write a histogram function. The stats subcommand takes a
group_by attribute, runs through the corpus, and prints the histogram.

[run: the GPS-bbox query]

Second command — a bounding box around central Cape Town. Four greater-
thans and less-thans on lat and lon. Five photos come back — the shots
taken inside that rectangle.

[run: the polygon query]

Third command — same idea, but with `point_in_polygon`. The Cape
Peninsula is a narrow finger pointing south from Cape Town. Cape Point
and the Cape of Good Hope sit right at the southern tip — well below
where any sensible Cape Town bbox would stop. If I expand the bbox to
catch them, I also catch Stellenbosch and the winelands which I don't
want. A polygon resolves that — I give it five vertices that hug the
peninsula's shape and twelve photos come back, including the seven Cape
Point and Cape of Good Hope shots the bbox missed.

The polygon is a flat list of latitude-longitude pairs — no GeoJSON
wrapper, no library to import. It's the same `is_image` predicate plus
one more function call.

The point: I went from "I have photos" to "I have photos taken on the
Cape Peninsula" in three short commands. No SQL, no GIS library, no
script.
-->

---

## Demo 4 — text inside images (OCR)

A separate corpus of synthetic text-bearing JPGs in
`~/Pictures/ocr-demo/`: error screenshots, receipts, signs, meeting
notes, code, posters.

**Cold pass — 12 images, OCR'd by macOS Vision (~2.5s wall-clock)**

```sh
file-search-on search --ocr --index-path /tmp/ocr.db \
  -d ~/Pictures/ocr-demo \
  'is_image && body.contains("ERROR")'
```

**Warm pass — cache hit, sub-second**

```sh
file-search-on search --ocr --index-path /tmp/ocr.db \
  -d ~/Pictures/ocr-demo \
  'is_image && body.matches("(?i)\\b(invoice|total)\\b")'
```

```sh
file-search-on search --ocr --index-path /tmp/ocr.db \
  -d ~/Pictures/ocr-demo \
  'is_image && body.contains("Athena")'
```

<!--
SCRIPT:
Demo four — text inside images. This is one of the most useful little
features I've shipped, and it didn't exist three months ago.

The corpus is a folder of twelve synthetic JPGs I generated with
ImageMagick — terminal error screenshots, receipts, invoices, road
signs, meeting notes, conference slides, code snapshots. Reproducible
from a script, no real-world data. About 35 kB per image.

[run: first query — `body.contains("ERROR")`]

First command. The `--ocr` flag tells file-search-on to run macOS Vision
over every image file as it walks. The query is is_image AND body
contains "ERROR". That's the *recognized text* of each image. Watch the
elapsed time — about two and a half seconds for twelve images, because
the OCR is happening cold for the first time. Two hits — the terminal
error screenshot and the log entry.

The footer at the bottom is interesting: "index: 0 hits, 12 misses, 12
stored". The on-disk cache is now populated.

[run: second query — invoice|total regex]

Second command. Same flags, different filter — a case-insensitive regex
matching the words "invoice" or "total" on word boundaries. Watch the
timing. [pause] Thirty milliseconds. The OCR results were cached from
the previous walk, so we just replayed the matcher against the body
strings. Twelve cache hits, zero misses, sub-second.

The hits are the receipt, the invoice, and the printed email — that
third one mentions "invoice 2026-0042" in the body, so it's a legit
match, not a false positive.

[run: third query — Athena]

One more — a project codename, "Athena", lives only in the meeting
notes image. One hit, instant.

The point: OCR is expensive — point five to two seconds per image — but
file-search-on caches the recognised text alongside the file's size
and mtime. Touch the file, you re-OCR. Don't touch it, you don't.
Useful for screenshots-as-knowledge-base workflows, scanned receipts,
slide decks, design mockups — anything where the searchable signal is
in the pixels, not the bytes.

Caveat: today this runs on macOS Vision only. Linux Tesseract and the
Windows Media OCR bridge are on the roadmap as drop-in providers, and
I'll cover that on the open-issues slide.
-->

---

## Demo 5 — finding visually similar photos

Perceptual hash (pHash) — a 64-bit fingerprint per image. Hamming
distance ≤ a few bits means the eye sees the same thing.

**One reference photo, find its visual neighbours**

```sh
file-search-on search \
  -d ~/Pictures/south-africa-holiday \
  "is_image && image_similar_to(phash, \
     '~/Pictures/south-africa-holiday/cape-of-good-hope_Cape_Point_Cape_Town_IMG_20180717_174658.jpg', \
     0.60)"
```

8 hits — the reference plus four Mossel Bay coastal shots, a Drakensberg
landscape, a Knysna outdoor scene, a Plettenberg dune. All wide-frame
nature shots; the function clusters by *visual layout*, not by EXIF tag
or filename.

`image_similar_to` auto-enables `--with-phash`, so you don't have to
remember the flag. Threshold is a similarity score; 0.85 ≈ near-duplicate
frame, 0.60 ≈ "same kind of scene".

<!--
SCRIPT:
Demo five — visual similarity.

This is where file-search-on stops being a "metadata filter" and starts
being something closer to an image search engine. Watch.

[run the command]

The query says: "is_image AND image_similar_to of phash, this reference
file, threshold zero point six". The image_similar_to function is a
built-in. It computes a sixty-four-bit perceptual hash of each photo as
the walk runs, and compares it to the reference photo's hash by Hamming
distance.

Eight hits come back. The first is the reference itself — every photo
is perceptually identical to itself. Then four shots from Mossel Bay,
all wide-frame coastal landscapes. A Drakensberg mountain shot. A
Knysna outdoor scene. A Plettenberg dune. None of these have anything
in common in their filenames, EXIF, or paths — the perceptual hash is
clustering them by *visual layout*. Wide horizon, lots of sky or water.

A couple of quality-of-life notes worth mentioning. First, I don't have
to pass --with-phash on the CLI. The tool sees the function reference
in my CEL expression and turns the flag on automatically. Second, the
threshold maps to a similarity score, not a Hamming distance. Zero
point eight five and up gets you near-duplicate frames — the same
photo re-encoded, screenshots-of-screenshots. Zero point six is what I
just ran — "same kind of scene". Anything below zero point five
basically returns most of the corpus.

The point: a content-type-aware search engine should be able to ask
the question "show me photos that look like this one". With one
function call in your CEL expression, file-search-on can.
-->

---

## Demo 6 — an AI agent driving it via MCP

```sh
file-search-on mcp   # serves MCP over stdio
```

The same query surface — `search`, `stats`, `find_duplicates`,
`diff_trees`, `find_matches`, `search_semantic`, `watch_search` … —
is exposed as 20 MCP tools.

**Live**: in Claude Code, ask:

> "I love this Cape Point shot. Find me other photos in
> `~/Pictures/south-africa-holiday` that are visually in the same style —
> coastal scenes, similar composition. Use a loose threshold; I want
> scene resemblance, not byte-identical duplicates."

The agent picks `search` with `with_phash: true` and an
`image_similar_to(phash, "<cape-point-path>", 0.60)` CEL filter, sorted
by similarity. Returns ~8 coastal / landscape shots — the same cluster
the CLI demo just showed.

<!--
SCRIPT:
Last demo — agents.

[switch to Claude Code window]

file-search-on also runs as a Model Context Protocol server. Same query
language, same content-type detection, but exposed as twenty tools an AI
agent can call. Stuff like search, stats, diff_trees, find_matches, even
semantic search through local embeddings.

I'll paste a question into Claude Code. [paste the question on screen]

"I love this Cape Point shot. Find me other photos in the
south-africa-holiday folder that are visually in the same style —
coastal scenes, similar composition. Use a loose threshold; I want
scene resemblance, not byte-identical duplicates."

Watch the tool calls in the timeline. Claude picks the `search` tool
with `with_phash` turned on and an `image_similar_to` function in the
CEL filter — exactly the same surface I just ran on the CLI in the
previous demo, but now selected by the agent from the natural-language
question. The "loose threshold" wording in my question nudges it down
to around point six, matching the cluster from Demo 5. Then it sorts
by similarity score and returns the same eight coastal shots. One tool
call, one prose answer.

The point I want to underline is: I didn't write a prompt template, I
didn't tell Claude to use perceptual hashing, I didn't tell it which
CEL function to call. The tool descriptions are good enough that the
agent picks the right approach itself.

And critically — this is NOT a sha256 hash check. Sha256 only finds
byte-identical files, which for photos is almost never useful — every
re-export or re-encode changes the bytes. Perceptual hashing catches
near-duplicates even when the bytes are completely different. That's
the kind of typed-attribute reasoning that's basically impossible with
find and grep.
-->

---

## Under the hood

- **Go 1.26**, ~10k LOC
- **cel-go** — Google's CEL implementation
- **Content-type plugin registry** — each type self-registers in `init()`
- **Detection**: exact-name → extension → magic-byte (512 B prefix)
- **Pluggable extras** — OCR (macOS Vision), perceptual hashes, EXIF, Dublin Core
- **Index cache** keyed by `(size, mtime)` — bbolt on disk or in-memory
- **20-tool MCP server** built on the Go SDK, plus a localhost dashboard
- Released via **GoReleaser + ko + Homebrew tap**

<!--
SCRIPT:
Thirty seconds on what's inside.

It's Go, about ten thousand lines, leaning on cel-go for the expression
engine. The interesting design choice is the content-type registry —
every file format is a Go file under internal/content that implements a
four-method interface and self-registers. Adding a new format is one
file plus, if it has new attributes, four corresponding edits to the CEL
schema.

Detection is three layers: exact filename match — package.json,
Dockerfile, go.mod — falls through to extension, falls through to magic
bytes on the first five hundred and twelve.

There's an optional cache so you don't re-parse unchanged files. There's
the MCP server I just demoed. Releases ship via GoReleaser, ko for the
container image, and a Homebrew tap.

Build steps are documented in CLAUDE.md if you want to dig in.
-->

---

## What surprised me

- **CEL is dramatically underused** outside of Google Cloud — it's
  sandboxed, fast, declarative, and lets non-Go users extend queries
  without a recompile.
- **MCP changed the framing**. Once the same tools are available to a
  human at a CLI *and* to an AI agent over a wire protocol, the surface
  area you have to design becomes one surface, not two.
- **Fuzz testing earned its keep** — five-minute nightly runs against
  hand-rolled binary parsers (MP3 ID3, MP4 box walker, MKV EBML) caught
  real panics on real files.

<!--
SCRIPT:
A few things that surprised me building this.

First — CEL is shockingly underused outside the Google ecosystem. It's
sandboxed, it's fast, it lets non-Go contributors write filters without
touching the source. If you're building any kind of policy or query
engine, look at it.

Second — the MCP angle reframed the design. Once the same query
surface is exposed to a human at the CLI and to an agent through MCP, I
stopped designing two interfaces and started designing one. That made
the codebase simpler, not more complex.

And — this isn't strictly surprising but worth noting — fuzz tests on
the hand-rolled binary parsers have caught real bugs. I run them
nightly for five minutes each. Cheap, high signal.
-->

---

## Open issues

- **OCR is Darwin-only** today — macOS Vision. Linux Tesseract + Windows
  Media OCR bridges are on the roadmap (`#189`).
- **Semantic search needs Ollama** running locally — works great when
  it's there, fails clearly when it isn't. A hosted-embedding fallback is
  in design.
- **Browser-history content type** — Chromium / Safari bookmarks ship,
  but History DBs don't. Wants a SQLite content-type extension.
- **Watch is bounded** — `watch_search` returns after a window. An
  unbounded streaming MCP transport is sketched but not built.
- **Windows path semantics** — works, but the dashboard's localhost
  registry uses `XDG_CACHE_HOME` first; Windows path conventions need a
  pass.

Track on **github.com/richardwooding/file-search-on/issues**.

<!--
SCRIPT:
A few things I haven't solved yet, in case you're curious or want to
contribute.

OCR is Mac-only. I built against the macOS Vision framework because
it's free and ships in the OS. Tesseract on Linux and Windows Media OCR
are obvious follow-ups but I haven't done them. Issue 189.

Semantic search runs through Ollama on localhost. That's deliberate — I
don't want to ship anything that calls out to a hosted service by
default — but if Ollama isn't installed, semantic just fails loudly. A
sane fallback story is on the list.

I'd love a browser-history content type. Chromium and Safari bookmarks
work today; the SQLite history databases don't.

Watch is currently bounded — you give it a duration and it returns when
the window closes. Streaming over MCP is a design problem I haven't
finished.

And Windows works but the dashboard's cross-process registry leans on
XDG conventions. Needs a portability pass.

Everything's tracked on GitHub.
-->

---

<!-- _class: lead -->

## Summary

`file-search-on`: typed CEL search for files, with first-class agent access.

```sh
brew install richardwooding/tap/file-search-on
file-search-on --help
file-search-on mcp     # add to claude_desktop_config.json
```

**github.com/richardwooding/file-search-on**

Open to contributors. Questions?

<!--
SCRIPT:
Wrapping up.

file-search-on is content-type-aware file search. Write a CEL expression
over typed attributes, get back paths plus structured metadata. Same
surface for humans on the CLI and AI agents through MCP.

It's on the homebrew tap if you want to try it — that command line at
the top. The GitHub repo has install instructions for other platforms,
plus an examples directory with thirty or forty recipes if you want to
see what's possible.

I'm open to contributors — there's a CONTRIBUTING file with the
conventions, and I'm responsive on issues.

That's me. Thanks for the time. Happy to take questions.
-->
