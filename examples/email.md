# Recipes — Email

Email content types: `email/rfc822` (single RFC 5322 message — `.eml`, `.email`), `email/mbox` (Unix mbox archive — `.mbox`). Umbrella boolean `is_email`.

Hand-rolled on top of stdlib `net/mail` (headers, address parsing), `mime/multipart` (boundary walks), `mime/quotedprintable` + `encoding/base64` (Content-Transfer-Encoding decoders). No third-party libs. Out of scope: Outlook `.msg`, encoded-attachment text extraction, DKIM/PGP signature verification.

## All-email triage

The umbrella query — every email message or mbox archive under a directory:

```sh
file-search-on 'is_email' -d ~/Mail
```

By format:

```sh
file-search-on 'is_email && content_type == "email/rfc822"' -d ~/Mail/cur   # one message per file (Maildir style)
file-search-on 'is_email && content_type == "email/mbox"'   -d ~/Backups    # mbox archives
```

## Find by sender / recipient

`author` carries the From header (display name preferred). `email_to` and `email_cc` are address-only lists for predictable membership tests:

```sh
# Emails I sent.
file-search-on 'is_email && author == "Alice Tester"' -d ~/Mail/Sent

# Emails sent TO a specific address (matches both To and Cc).
file-search-on 'is_email && ("alice@example.com" in email_to || "alice@example.com" in email_cc)' -d ~/Mail

# Phonetic / fuzzy match on display name (handy for misspelled From values).
file-search-on 'is_email && soundex(author) == soundex("Smith")' -d ~/Mail
file-search-on 'is_email && levenshtein(author, "Alice Tester") <= 2' -d ~/Mail
```

## Find by subject

Subject is exposed as `title` (reused with markdown / PDF / EPUB / office / audio titles — same query vocabulary):

```sh
file-search-on 'is_email && title.contains("invoice")' -d ~/Mail
file-search-on 'is_email && title.startsWith("[ALERT]")' -d ~/Mail/Alerts

# Fuzzy subject match.
file-search-on 'is_email && ngram_similarity(title, "kubernetes outage", 3) > 0.4' -d ~/Mail
```

## Time-based filters

`sent_at` is a CEL `timestamp` parsed from the Date header. Compose with the standard CEL timestamp operators:

```sh
# Emails sent in 2026.
file-search-on 'is_email && sent_at >= timestamp("2026-01-01T00:00:00Z") && sent_at < timestamp("2027-01-01T00:00:00Z")' -d ~/Mail

# Emails older than a year.
file-search-on 'is_email && sent_at < timestamp("2025-05-05T00:00:00Z")' -d ~/Mail/Archive

# Specific date range matching a project window.
file-search-on 'is_email && sent_at >= timestamp("2026-04-01T00:00:00Z") && sent_at <= timestamp("2026-04-30T23:59:59Z")' -d ~/Mail
```

## Attachment hunting

`attachment_count` counts top-level multipart parts that carry `Content-Disposition: attachment` or a `filename` parameter:

```sh
# Any attachment.
file-search-on 'is_email && attachment_count > 0' -d ~/Mail

# Heavy attachments (multiple files).
file-search-on 'is_email && attachment_count >= 3' -d ~/Mail

# No attachments — text-only correspondence.
file-search-on 'is_email && attachment_count == 0' -d ~/Mail/Sent
```

Combine with `size`:

```sh
# Big emails that have attachments.
file-search-on 'is_email && attachment_count > 0 && size > 1000000' -d ~/Mail   # > 1 MB
```

## Threading

`email_message_id` and `email_in_reply_to` carry the canonical IDs (angle brackets stripped):

```sh
# Find every reply to a specific message.
file-search-on 'is_email && email_in_reply_to == "thread-root@example.com"' -d ~/Mail

# Roots of threads (no In-Reply-To).
file-search-on 'is_email && email_in_reply_to == ""' -d ~/Mail
```

## mbox archives

For `.mbox` archives, per-message attributes reflect the **first** message (a sniff at archive contents). `email_count` carries the multi-message shape:

```sh
# Count messages in every mbox in a backup directory.
file-search-on 'content_type == "email/mbox"' -d ~/Backups -o json |
  jq -s 'map({path, email_count}) | sort_by(-.email_count)'

# Find big archives.
file-search-on 'content_type == "email/mbox" && email_count > 1000' -d ~/Backups

# Empty / near-empty mboxes — broken exports.
file-search-on 'content_type == "email/mbox" && email_count <= 1' -d ~/Backups
```

## Useful output formats

```sh
# Path + subject + from + sent_at + attachment count, tab-separated.
file-search-on 'is_email' --format '{{.Path}}\t{{.Title}}\t{{.Author}}\t{{.SentAt}}\t{{.AttachmentCount}}'

# JSON for jq pipelines — group by sender.
file-search-on 'is_email' -d ~/Mail -o json |
  jq -s 'group_by(.author) | map({sender: .[0].author, count: length}) | sort_by(-.count) | .[0:20]'

# Bare paths for xargs (e.g. delete every email older than a year — DESTRUCTIVE, dry-run first).
file-search-on 'is_email && sent_at < timestamp("2025-05-05T00:00:00Z")' -d ~/Mail/Archive -o bare \
  | xargs -I {} echo rm -f {}   # drop the `echo` to actually delete
```

## Body-content search

Pass `--body` (CLI) / `include_body: true` (MCP) and the `body` CEL variable carries the extracted message text. The extractor walks the MIME tree:

- **multipart/alternative**: prefers `text/plain` over `text/html` (typical for marketing email — the plain version is what an agent wants to grep).
- **multipart/mixed** / **related** / **signed**: concatenates every non-attachment text part.
- **text/plain** / **text/html**: decodes Content-Transfer-Encoding (`quoted-printable` / `base64` / `7bit` / `8bit`) and, for HTML, strips tags via the same permissive XHTML reader the EPUB extractor uses.
- **mbox archives**: every message's body is concatenated with a blank-line separator, so `body.contains("invoice")` searches the whole inbox.

Headers (Subject / From / etc.) deliberately don't appear in the body — those already surface as `title` / `author` / `email_to` / `email_message_id` / `sent_at`. Attachments are skipped entirely (binary payloads, encoded text inside attachments, etc.).

```sh
# Find every email that mentions a specific invoice number
file-search-on 'is_email && body.contains("INV-2025-0042")' --body -d ~/Mail

# Receipts from a specific vendor — combine header + body
file-search-on 'is_email && author.contains("@stripe.com") && body.matches("(?i)\\$[0-9]+\\.[0-9]{2}")' --body

# Across an entire mbox archive
file-search-on 'content_type == "email/mbox" && body.contains("password reset")' --body -d ~/Mail/Archives

# Combine with find-matches for line-level grep + context
file-search-on find-matches '(?i)\bunsubscribe\b' --expr 'is_email' -d ~/Mail -C 2
```

The 1 MiB body cap (`--body-max-bytes`) applies to extracted text, not raw bytes — a 50 MB mbox with sparse text still reads cheaply. For mbox archives the extractor stops mid-archive once the cap is reached.
