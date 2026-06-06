---
name: reddit-golang-post
description: Drafts a self-post for the [r/golang](https://www.reddit.com/r/golang) subreddit in Markdown with a YAML frontmatter title and optional flair, then runs a script that strips the frontmatter, places the body markdown on the macOS clipboard via `pbcopy`, and prints the title and flair to stderr so the user can paste them into Reddit's title field, flair dropdown, and markdown editor. Use when the user asks to write, draft, polish, or post content explicitly intended for r/golang — phrases like "post on r/golang", "Show & Tell on r/golang", "draft a golang subreddit post", or "write a Reddit post for golang".
---

# r/golang Post

[r/golang](https://www.reddit.com/r/golang)'s post composer accepts markdown directly — headings, fenced code blocks, lists, and `[text](url)` links all render. No format conversion is needed; the script just splits the YAML frontmatter (which carries the title and optional flair) from the body and pipes the body to `pbcopy`. The author's job is the writing — the etiquette of r/golang is what makes or breaks the post.

## Quick start

1. Gather from the user: topic, key technical points, repo / reference URLs, post type (Show & Tell vs Discussion / Help).
2. Pick a template — [templates/show-and-tell.md.tmpl](templates/show-and-tell.md.tmpl) for project announcements, [templates/discussion.md.tmpl](templates/discussion.md.tmpl) for questions / opinion-seeking.
3. Draft to `/tmp/reddit-golang-post.md`. Frontmatter must include `title:`; optionally `flair:`.
4. **Run** `bash .claude/skills/reddit-golang-post/scripts/copy.sh /tmp/reddit-golang-post.md` — body markdown lands on the clipboard; title and flair print to stderr.
5. Tell the user to paste the title into Reddit's title field, select the flair from the dropdown, switch the editor to **Markdown** mode, and paste the body.

## Post shape

| Mode | Word target | Use for |
| --- | --- | --- |
| **Show & Tell** | 200–600 words | "I built X" project announcements |
| **Discussion / Help** | 100–400 words | Specific questions, design opinions |

Reddit caps body at 40,000 characters but engagement drops sharply past ~1500 words. Posts under 50 words read as low-effort. Always include a one-line TL;DR at the top or bottom for posts longer than ~600 words.

## Frontmatter format

```yaml
---
title: "Show & Tell: file-search-on — search files by metadata via CEL"
flair: Show & Tell
---
```

- `title:` is required. Keep under 300 chars (Reddit's hard cap); under ~120 chars reads better in the feed.
- `flair:` is optional. Reddit's flairs change occasionally — common ones on r/golang are **Show & Tell**, **Help**, **Discussion**, **Newbie Question**, **Code Review**, **Generics**.
- Quotes around the title are recommended (handles colons and special chars cleanly).

## Voice and structure

See [references/style.md](references/style.md) for r/golang culture, title conventions, self-promotion etiquette, code-formatting expectations, and what r/golang's audience reliably dislikes. The short version: first person, technical, plain. Lead with what the code looks like, not why you're excited. Acknowledge tradeoffs honestly. No "please star my repo" CTAs.

## Scripts

- **Run** `bash scripts/copy.sh <path-to-markdown>` — strips YAML frontmatter, places body on the macOS clipboard, prints title (and flair, if present) to stderr. Exits non-zero if the input file doesn't exist.

## Templates

- [templates/show-and-tell.md.tmpl](templates/show-and-tell.md.tmpl) — "I built X" skeleton with hook, what-it-does, why, interesting bits, what's next.
- [templates/discussion.md.tmpl](templates/discussion.md.tmpl) — question / discussion skeleton with framing, background, specific question.

## References

- [references/style.md](references/style.md) — r/golang voice, title patterns, flair list, self-promotion rules, code-block formatting, length guidance, and what Reddit's markdown editor does and doesn't render.

## Conventions

- **Markdown source first, clipboard second.** Always keep the `.md` file the user can re-edit. The script regenerates the clipboard in milliseconds.
- **Drafts go in `/tmp/`** — they're throwaway. If the user wants the source long-term, they say so and the agent writes to a path they specify.
- **Plain text clipboard.** Reddit's markdown editor ingests plain markdown. No `pandoc`, no `textutil`, no RTF — `pbcopy` straight.
- **macOS only.** The script uses `pbcopy`. Linux / Windows would need a different clipboard hand-off — out of scope for v1.
- **Don't auto-publish.** This skill produces clipboard content; the user submits manually via the Reddit web UI. There is no Reddit API integration here.
- **r/golang-specific.** Etiquette and conventions vary between subreddits; this skill is scoped to r/golang. A sibling skill (e.g. `reddit-rust-post`) would be a fork-and-edit of this one.
