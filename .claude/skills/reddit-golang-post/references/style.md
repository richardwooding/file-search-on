# r/golang writing style and Reddit formatting

## Contents

- [Post types](#post-types)
- [Title conventions](#title-conventions)
- [Flairs](#flairs)
- [Self-promotion etiquette](#self-promotion-etiquette)
- [Voice](#voice)
- [Code formatting](#code-formatting)
- [Links](#links)
- [Length](#length)
- [What r/golang dislikes](#what-rgolang-dislikes)
- [Posting timing](#posting-timing)
- [Reddit markdown — what renders](#reddit-markdown--what-renders)

## Post types

- **Text post (self-post)** — body is markdown; this is what the skill produces. Use for write-ups, Show & Tell, questions, discussions.
- **Link post** — just a URL, no body. Rarely the right call on r/golang: a naked GitHub link without context tends to get downvoted or removed. Convert to a text post with the URL inline.

## Title conventions

Direct, descriptive, no clickbait. Title patterns that work on r/golang:

- `<Project>: <one-line description>` — e.g. `file-search-on: search files by metadata via CEL expressions`
- `Show & Tell: <project> (<description>)` — explicit Show & Tell prefix is conventional but not required.
- `Help: <specific question>` — for help requests; specifics in the title get faster answers.
- `<topic> — <discussion / opinion>` — for design / opinion posts.

Avoid:

- Clickbait (`"You won't believe..."`, `"This one trick..."`).
- ALL CAPS or excessive punctuation (`!!!`, `???`).
- Emoji in the title — reads as low-effort.
- `[OC]` / `[Original Content]` tags — those are r/pics conventions, not r/golang.
- Vague titles (`"check this out"`, `"thoughts?"`).

Reddit's hard cap is 300 chars. Aim for under 120 — long titles get truncated in the feed.

## Flairs

r/golang's flairs change occasionally; the typical set:

- **Show & Tell** — "I built X" announcements.
- **Help** — questions where the user is stuck.
- **Discussion** — opinion / design / philosophy.
- **Newbie Question** — explicitly for beginners; signals tolerance for basic questions.
- **Code Review** — "would appreciate eyes on this".
- **Generics** — generics-specific (still a hot topic).

The script doesn't pick a flair automatically; the user selects it from Reddit's dropdown after pasting. The frontmatter `flair:` field is just a reminder for the user.

## Self-promotion etiquette

r/golang allows "I made X" posts, but expects context:

- **Yes**: a write-up explaining what the project does, why you built it, technical decisions, tradeoffs. Repo URL inline, not buried.
- **No**: a naked GitHub link with one-line description. Often removed by mods.
- **No**: "please star my repo" / "please follow me on Twitter" CTAs.
- **No**: posting the same project repeatedly (once per major release is the rough threshold).
- The 9:1 rule applies generally — mostly engage with the community, occasionally promote your own work.

A good Show & Tell post **invites discussion** (e.g. "feedback welcome on the parsing approach in `frontmatter.go`") rather than asking for a specific reaction.

## Voice

- **First person, technical, plain.** "I built X. It does Y. Here's how Z works." Skip marketing voice.
- The audience is professional Go developers — assume they know the language, the stdlib, and the common ecosystem (net/http, gorilla, sqlx, cobra/kong, etc.).
- **Lead with code, not motivation.** What does the API look like? What's the interesting line in the parser? Show before telling.
- **Acknowledge tradeoffs.** "This is slower than ffmpeg but ships as a single binary" beats "this is the fastest". The community rewards honesty over hype.
- **No "vibe-coded" branding.** If you used an LLM heavily, that's fine, but show that you understand what the code does. Walls of unedited AI output are a fast downvote.

## Code formatting

- **Fenced triple-backtick blocks**:

  ````
  ```go
  func main() {
      fmt.Println("hello")
  }
  ```
  ````

  The `go` language tag is preserved in source but Reddit doesn't syntax-highlight (yet). Harmless to include.

- **Inline `code`** — single backticks, e.g. `package main`. Renders as monospace.
- **Indent-based code blocks** (4-space prefix) work too but are uglier in the markdown source. Prefer fences.
- **Don't paste 200-line code blocks.** Reddit will display them but readers won't scroll. Excerpt the interesting bit and link to the file on GitHub.

## Links

- `[anchor text](https://example.com)` markdown renders as clickable. Bare URLs auto-link too but inline anchor text reads better.
- **2–4 inline links per post is plenty.** Links to your own repo / docs / specific files are fine. Links to your blog / Twitter / company site get scrutinised — only include if directly relevant.
- Don't link the title of a Show & Tell post away from Reddit; keep the body the destination.

## Length

| Mode | Sweet spot | Hard maximum | Why |
| --- | --- | --- | --- |
| Show & Tell | 200–600 words | 1500 (engagement falls off) | Past 1500 readers TL;DR |
| Discussion / Help | 100–400 words | 1000 | Specific questions get specific answers |

Add a one-line **TL;DR** at the top or bottom for any post over ~600 words. It's the read most people give the post — make it work alone.

## What r/golang dislikes

- Generic "is Go better than Rust" / "is Go dying" posts — saturated, locked, or removed.
- Showcase posts with no source link or proprietary code.
- AI-generated walls of text (long-winded, repetitive, hedge-everywhere prose).
- Cross-posts from other subs without adaptation — r/golang's audience is more technical than r/programming.
- "Please upvote / star / follow" CTAs.
- Posts that conflate Go the language with Google the company politics.

## Posting timing

r/golang's audience skews US/EU working hours. Weekday mornings (UTC, ~13:00–17:00) tend to get the most eyeballs in the first hour, which Reddit's algorithm rewards. Not actionable from the skill — the user submits manually — but worth mentioning when the user asks "when should I post this?".

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
