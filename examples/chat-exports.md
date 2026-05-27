# Chat exports — Slack / Discord / Signal

Slack workspace exports, Discord (DiscordChatExporter) dumps, and signal-cli `--json` output are all JSON archives full of searchable conversation. file-search-on detects them by their **top-level JSON shape** (not extension — they're arbitrary-named `.json` files) and surfaces message-level metadata plus a searchable `body`.

| Content type | Source | `chat_channel` | `chat_workspace` |
| --- | --- | --- | --- |
| `chat/slack-export` | Slack workspace export, `{workspace}/{channel}/YYYY-MM-DD.json` | channel (from directory) | workspace (from directory) |
| `chat/discord-export` | DiscordChatExporter JSON | `channel.name` | `guild.name` |
| `chat/signal-cli` | signal-cli `--json` (NDJSON or array) | contact (file basename) | — (none) |

## Predicates and attributes

- **Family predicate**: `is_chat_export` (content_type starts with `chat/`).
- **Per-format**: `is_slack_export`, `is_discord_export`, `is_signal_export`.
- **Attributes**: `chat_message_count` (int), `chat_participants` (list&lt;str&gt; — distinct authors, capped 500), `chat_channel` (str), `chat_workspace` (str), `chat_start_at` / `chat_end_at` (timestamp).
- **Body** (`--body`): one `{RFC3339 timestamp}\t{author}\t{text}` line per message, so `body.contains(...)` / `body.matches(...)` grep the conversation.

## How detection works

These files have no fixed basename (a Slack channel-day file is named by date), so they can't be filename-matched like browser bookmarks. Instead a content-discriminator tier kicks in **only when extension matching would otherwise yield generic `json`**: the detector reads the file head once and a streaming `json.Decoder` inspects the top-level structure —

- top-level object with `guild` + `channel` + `messages` → Discord
- top-level array of `{ts, user, text}` (or `{"messages": [...]}`) → Slack
- top-level `envelope` objects (NDJSON or array) → Signal

A plain config `.json` matches none of these and stays generic `json`.

## CLI

```sh
# Every chat export under a backup tree
file-search-on 'is_chat_export' -d ~/exports -o bare

# Busy channels — more than 500 messages
file-search-on 'is_chat_export && chat_message_count > 500' -d ~/exports

# Find the #engineering conversation that mentioned kubernetes (Slack)
file-search-on 'is_slack_export && chat_channel == "engineering" && body.contains("kubernetes")' \
  --body -d ~/slack-export

# Discord messages from a specific guild containing a URL
file-search-on 'is_discord_export && chat_workspace == "My Server" && body.matches("https?://")' \
  --body -d ~/discord

# Who participated? (distinct authors across a Signal dump)
file-search-on 'is_signal_export' --body -d ~/signal -o json | jq '.chat_participants'

# Conversations active in Q3 2024
file-search-on 'is_chat_export && chat_start_at > timestamp("2024-07-01T00:00:00Z") && chat_end_at < timestamp("2024-10-01T00:00:00Z")' \
  -d ~/exports
```

`body.contains(...)` is the workhorse — it greps message text. Pair it with `chat_channel` / `chat_workspace` / `chat_participants` to scope.

## MCP

The same vocabulary works through the `search` tool. To search message bodies, pass `include_body: true`:

```json
{
  "name": "search",
  "arguments": {
    "expr": "is_chat_export && body.contains(\"incident\")",
    "dir": "/Users/me/exports",
    "include_body": true
  }
}
```

Each match carries `chat_message_count`, `chat_participants`, `chat_channel`, `chat_workspace`, `chat_start_at`, `chat_end_at`.

## Pitfalls

- **Message text only.** Attachments, reactions, threading, and edits are out of scope — the body is the flat message stream.
- **Participant identity is by display string.** Slack messages without a `user_profile` fall back to the raw user ID, so the same person can appear under two names if some of their messages lack a profile. Discord prefers nickname over username; Signal prefers `sourceName` over the phone number.
- **Defensive caps.** Up to 100 MB is read per file, 10000 messages are parsed, and the participant list is capped at 500. `chat_message_count` reflects parsed messages (capped at 10000), not necessarily the file's true total.
- **Slack channel/workspace come from the directory.** Keep the `{workspace}/{channel}/YYYY-MM-DD.json` layout intact; a flattened dump leaves `chat_channel` / `chat_workspace` empty.
- **signal-cli NDJSON is supported.** The default newline-delimited output works as-is (no need to wrap it in an array), as does an array-wrapped form.

## Related recipes

- [`body-search.md`](body-search.md) — the `--body` / `body.contains` / `body.matches` mechanics these recipes lean on.
- [`email.md`](email.md) — the closest sibling: message-level body search over `.eml` / `.mbox`.
- [`browser-bookmarks.md`](browser-bookmarks.md) — the other content-discriminator content type (Chromium vs Safari).
