# r/mcp writing style and Reddit formatting

## Contents

- [Audience](#audience)
- [Post types](#post-types)
- [Title conventions](#title-conventions)
- [Flairs](#flairs)
- [The disclosure norm](#the-disclosure-norm)
- [Voice](#voice)
- [Code formatting](#code-formatting)
- [Links](#links)
- [Length](#length)
- [Security caveats](#security-caveats)
- [What r/mcp dislikes](#what-rmcp-dislikes)
- [Reddit markdown — what renders](#reddit-markdown--what-renders)

## Audience

r/mcp readers are agent / tool builders and integrators across multiple languages (TypeScript, Python, Go, Rust). The community is younger than r/golang and norms are still settling, but technical depth is expected. Lead with what the server / client / spec change actually does, not why you're excited about it.

## Post types

- **Text post (self-post)** — body is markdown; this is what the skill produces. Use for write-ups, Show & Tell, questions, discussion of the spec.
- **Link post** — naked URL, no body. Almost always wrong on r/mcp: a bare repo link without context (which clients work, which transports, which spec version) gets ignored or downvoted. Convert to a text post with the URL inline.

## Title conventions

Direct, descriptive, no clickbait. Patterns that work:

- `Show & Tell: <project> — <one-line description>` — clean for project announcements.
- `<project>: MCP server for <domain>` — emphasises the integration.
- `Released: <name> v<version> (<short summary>)` — for version-bump posts.
- `Discussion: <spec topic or design question>`
- `Help: <specific symptom>` — e.g. "Help: Streamable HTTP transport returns 405 from Claude Desktop".

Avoid:

- Clickbait (`"You won't believe what MCP can do..."`).
- Vague (`"check out my new MCP server"`).
- ALL CAPS or `[OC]` tags.
- Emoji in the title.
- "First MCP server for X" / "Best MCP server for X" — superlatives age badly.

Reddit caps titles at 300 chars; aim under 120.

## Flairs

r/mcp's flair set is still evolving. Common ones include **Show & Tell** / **Showcase**, **Discussion**, **Question** / **Help**, **Tutorial**, **News**, **Server**, **Client**, **Spec**. Pick the closest match; the user selects from Reddit's dropdown after pasting.

## The disclosure norm

Every Show & Tell on r/mcp should answer three questions in the body, ideally in the first 150 words:

1. **Which MCP clients have you tested with?** Claude Desktop, Cline, Continue, Cursor, Zed, Goose, the SDK's in-memory transport, etc. Saying "should work with any MCP client" without testing one is a red flag.
2. **Which transport(s) does it support?** Stdio (the desktop-app default), Streamable HTTP (2025-03-26+), HTTP+SSE (deprecated 2024-11-05).
3. **Which spec version?** 2024-11-05 / 2025-03-26 / 2025-06-18. Tools, resources, and prompts have evolved across versions.

Readers will ask in the comments anyway. Pre-empting saves a comment thread of triage.

## Voice

- **First person, technical, plain.** "I built X. It exposes Y tools. It integrates with Z client over W transport."
- The audience knows what tools / resources / prompts / sampling are. Don't explain the protocol from scratch unless that's the post's whole point.
- **Show before telling.** What does the tool definition look like? What does the JSON-RPC look like on the wire? Concrete > abstract.
- **Acknowledge tradeoffs.** "Stdio only — HTTP transport is on the roadmap" beats silent omission.

## Code formatting

- **Fenced triple-backtick blocks**:

  ````
  ```typescript
  server.tool("search", { query: z.string() }, async ({ query }) => {
    return { content: [{ type: "text", text: await search(query) }] };
  });
  ```
  ````

  Reddit doesn't syntax-highlight; the language tag is harmless.

- **Inline `code`** for tool names, transport names (`stdio`, `streamable-http`), spec terms.
- A small JSON-RPC excerpt is often the most informative thing you can paste — show what a `tools/call` response actually looks like.

## Links

- `[anchor text](https://example.com)` markdown renders as clickable. 2–4 inline links per post.
- **Always link the repo.** A Show & Tell without a public repo is suspect.
- Link to the [MCP spec](https://modelcontextprotocol.io/) page when referencing protocol details.
- Link to your tested clients (Claude Desktop, Cline, Cursor) sparingly — readers know what those are.

## Length

| Mode | Sweet spot | Hard maximum | Why |
| --- | --- | --- | --- |
| Show & Tell | 200–600 words | 1500 (engagement falls off) | Past 1500 readers TL;DR |
| Discussion / Help | 100–400 words | 1000 | Specific questions get specific answers |

Add a one-line **TL;DR** at the top or bottom for posts over ~600 words.

## Security caveats

r/mcp posts are pre-installation pitches: readers are deciding whether to `npx` / `uvx` / `pip install` / `go install` your server into their agent's blast radius. That changes the rules.

- **Public repo, MIT/Apache or similar** — closed-source MCP servers don't get installed. Say so explicitly.
- **What does the server access?** Filesystem? Network? Local DB? Spell it out. "Reads files under a configurable root" is helpful; silence is suspicious.
- **No hard-coded secrets / API keys in the README.** If your server needs an API key, document the env var.
- **Don't post raw conversation logs** that include API keys, tokens, or personal data — agents leak more than people expect.
- **Sandboxing / permissions** — call out anything you've done (read-only mode, path allowlists, dry-run flags). The community rewards careful design.

## What r/mcp dislikes

- "Wrapper around someone else's API with no value-add" posts.
- Marketing copy / "revolutionary AI integration" tone.
- Posts hiding behind a Notion / Docs link instead of saying what the server does.
- AI-generated walls of text — readable to a person reads better than longer.
- "Thoughts?" with no anchor for discussion.
- Asking for stars / Discord joins / mailing list signups.

## Reddit markdown — what renders

| Format | Renders? |
| --- | --- |
| Headings (`# … ######`) | Yes |
| **Bold**, *italic* | Yes |
| Bulleted / numbered lists | Yes |
| Tables (GFM-style with `|`) | Yes (recent Reddit) |
| Blockquotes (`>`) | Yes |
| Horizontal rules (`---`) | Yes |
| Fenced code blocks (```` ``` ````) | Yes (no syntax highlighting) |
| Inline code (`` ` ``) | Yes |
| `[text](url)` hyperlinks | Yes (clickable) |
| Footnotes (`[^1]`) | **No** — use parenthetical asides instead |
| Raw HTML | **Stripped** |
| Images embedded inline | **No** — upload via the UI as a separate attachment |

The skill produces standard CommonMark + GFM tables; everything in the "yes" list survives the paste cleanly.
