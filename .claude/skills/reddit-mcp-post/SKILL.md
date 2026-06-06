---
name: reddit-mcp-post
description: Drafts a self-post for the [r/mcp](https://www.reddit.com/r/mcp) subreddit (Model Context Protocol community) in Markdown with a YAML frontmatter title and optional flair, then runs a script that strips the frontmatter, places the body markdown on the macOS clipboard via `pbcopy`, and prints the title and flair to stderr so the user can paste them into Reddit's title field, flair dropdown, and markdown editor. Use when the user asks to write, draft, polish, or post content explicitly intended for r/mcp — phrases like "post on r/mcp", "Show & Tell on r/mcp", "draft an MCP subreddit post", or "write a Reddit post for the MCP community".
---

# r/mcp Post

[r/mcp](https://www.reddit.com/r/mcp) is the subreddit for the [Model Context Protocol](https://modelcontextprotocol.io/) — readers are agent developers, tool builders, and integrators who care about MCP servers, clients, and spec details. Reddit's post composer accepts markdown directly, so the script just splits the YAML frontmatter from the body and pipes the body to `pbcopy`.

## Quick start

1. Gather from the user: project / topic, key technical points, repo URL, which MCP clients have been tested (Claude Desktop, Cline, Continue, Cursor, Zed, etc.), transport (stdio / Streamable HTTP / SSE), and post type (Show & Tell vs Discussion / Help).
2. Pick a template — [templates/show-and-tell.md.tmpl](templates/show-and-tell.md.tmpl) for project announcements, [templates/discussion.md.tmpl](templates/discussion.md.tmpl) for spec / design questions or troubleshooting.
3. Draft to `/tmp/reddit-mcp-post.md`. Frontmatter must include `title:`; optionally `flair:`.
4. **Run** `bash .claude/skills/reddit-mcp-post/scripts/copy.sh /tmp/reddit-mcp-post.md` — body markdown lands on the clipboard; title and flair print to stderr.
5. Tell the user to paste the title into Reddit's title field, select the flair from the dropdown, switch the editor to **Markdown** mode, and paste the body.

## Post shape

| Mode | Word target | Use for |
| --- | --- | --- |
| **Show & Tell** | 200–600 words | "I built an MCP server / client for X" |
| **Discussion / Help** | 100–400 words | Spec questions, transport debugging, design opinions |

Always include: which MCP client(s) you tested with, which transport, which spec version (2024-11-05 / 2025-03-26 / 2025-06-18). r/mcp readers ask these in the comments anyway — pre-empt them.

## Frontmatter format

```yaml
---
title: "Show & Tell: file-search-on — content-type-aware file search as an MCP server"
flair: Show & Tell
---
```

- `title:` is required. Keep under 300 chars (Reddit's hard cap); under ~120 chars reads better in the feed.
- `flair:` is optional. Common r/mcp flairs include **Show & Tell** / **Showcase**, **Discussion**, **Question** / **Help**, **Tutorial**, **News**, **Server**, **Client**.
- Quote the title (handles colons cleanly).

## Voice and structure

See [references/style.md](references/style.md) for r/mcp culture, title conventions, what context the audience expects (clients tested, transport, spec version), and the security caveats specific to a community where readers will literally `npx`/`uvx`/`pip install` your code into their agent's blast radius.

## Scripts

- **Run** `bash scripts/copy.sh <path-to-markdown>` — strips YAML frontmatter, places body on the macOS clipboard, prints title (and flair) to stderr. Exits non-zero if the input file doesn't exist.

## Templates

- [templates/show-and-tell.md.tmpl](templates/show-and-tell.md.tmpl) — "I built X" skeleton with hook, what-it-does, MCP-specific context (client / transport / spec version), interesting bits, what's next.
- [templates/discussion.md.tmpl](templates/discussion.md.tmpl) — question / discussion skeleton for spec, transport, or design topics.

## References

- [references/style.md](references/style.md) — r/mcp voice, title patterns, flair list, the "client + transport + spec version" disclosure norm, security expectations, and what the markdown editor renders.

## Conventions

- **Markdown source first, clipboard second.** Always keep the `.md` file the user can re-edit.
- **Drafts go in `/tmp/`** — they're throwaway. If the user wants the source long-term, they say so and the agent writes to a path they specify.
- **Plain text clipboard.** Reddit's markdown editor ingests plain markdown — `pbcopy` straight, no `pandoc`, no `textutil`.
- **macOS only.** Linux / Windows would need a different clipboard hand-off — out of scope for v1.
- **Don't auto-publish.** This skill produces clipboard content; the user submits manually via the Reddit web UI.
- **Disclose tested clients and transport.** r/mcp readers expect this context — bake it into every Show & Tell.
- **r/mcp-specific.** Etiquette and conventions vary between subreddits; this skill is scoped to r/mcp. The sibling `reddit-golang-post` skill follows the same structural pattern.
