# Recipes — Secret / credential scanning

Two built-in CEL functions turn file-search-on into a quick credential-triage layer — no gitleaks / trufflehog setup:

- `has_secrets(body) -> bool` — true when the body contains a credential / token / key.
- `secret_kinds(body) -> list<string>` — the categories matched (`["aws-access-key", "private-key-pem", …]`).

Both operate on the `body` variable, so pass `--body` (CLI) / `include_body: true` (MCP). Detection only — no validation, no redaction. This is a fast first-pass triage, not a replacement for a dedicated scanner with entropy analysis + git-history walking.

## What it catches

Anchored, low-false-positive patterns for:

| Category | Shape |
|---|---|
| `aws-access-key` | `AKIA` / `ASIA` / `AGPA` / … + 16 base32 chars |
| `aws-secret-key` | 40-char base64, context-anchored to `aws_secret_access_key =` |
| `github-token` | `ghp_` / `gho_` / `ghu_` / `ghs_` / `ghr_` + 36 chars |
| `github-fine-grained-pat` | `github_pat_` + 82 chars |
| `gitlab-token` | `glpat-` + 20 chars |
| `slack-token` | `xoxb-` / `xoxp-` / `xoxa-` / `xoxr-` / `xoxs-` + body |
| `slack-webhook` | `https://hooks.slack.com/services/T…/B…/…` |
| `stripe-key` | `sk_live_` / `rk_live_` + 24+ chars |
| `google-api-key` | `AIza` + 35 chars |
| `npm-token` | `npm_` + 36 chars |
| `openai-key` | `sk-…T3BlbkFJ…` |
| `private-key-pem` | `-----BEGIN … PRIVATE KEY-----` (RSA / EC / DSA / OpenSSH / PGP) |
| `jwt` | `eyJ….eyJ….…` (three base64url segments) |
| `credit-card` | Visa / Mastercard / Amex / Discover prefixes (best-effort) |
| `generic-assignment` | `password` / `secret` / `token` / `api_key` = `"<16+ opaque chars>"` |

## Boolean triage

```sh
# Source files in my home that might contain a secret
file-search-on 'is_source && has_secrets(body)' --body -d ~/Code

# Configs (JSON / YAML / TOML) with embedded credentials
file-search-on '(is_json || is_yaml || is_toml) && has_secrets(body)' --body -d ~/Code

# Markdown notes that leaked a token
file-search-on 'is_markdown && has_secrets(body)' --body -d ~/Notes

# Anything in a repo's tracked files (compose with --prune-build-artefacts)
file-search-on 'has_secrets(body)' --body --prune-build-artefacts -d ~/Code/myproject
```

## Which kind fired — `secret_kinds`

```sh
# Find files specifically containing a PEM private key
file-search-on 'is_source && "private-key-pem" in secret_kinds(body)' --body -d ~/Code

# AWS keys only (access OR secret)
file-search-on '
  has_secrets(body) &&
  (("aws-access-key" in secret_kinds(body)) || ("aws-secret-key" in secret_kinds(body)))
' --body -d ~/Code

# Show every match with the categories, as JSON
file-search-on 'has_secrets(body)' --body -d ~/Code -o json | \
  jq -r '"\(.path)"'   # then re-scan per-file, or use the MCP secret_kinds in the agent
```

## Compose with the rest of CEL

```sh
# Recently-modified configs with secrets (a fresh leak)
file-search-on '
  (is_json || is_yaml) &&
  has_secrets(body) &&
  mtime_year == 2026
' --body -d ~/Code

# Secrets in files NOT covered by a .gitignore (the dangerous ones —
# they'd actually get committed)
file-search-on 'has_secrets(body)' --body --respect-gitignore -d ~/Code/myproject

# Large files with secrets — often accidentally-committed .env dumps
file-search-on 'has_secrets(body) && size > 4096' --body -d ~/Code
```

## Performance

Each pattern is an anchored RE2 regex compiled once at startup. `has_secrets` short-circuits on the first match; `secret_kinds` runs the full set. Bodies are capped at `--body-max-bytes` (default 1 MiB). Pair with a tight type predicate (`is_source && …`) so the body read only happens on candidate files.

## Known limitations

- **Detection, not validation.** A matched `aws-access-key` might be a revoked / example key (the docs use `AKIAIOSFODNN7EXAMPLE`). The tool reports the shape; it doesn't call AWS to confirm the key is live.
- **No entropy heuristic.** High-entropy blobs without a recognised prefix (some custom token formats) won't fire. This keeps false-positives low at the cost of recall. For exhaustive scanning, use gitleaks / trufflehog.
- **`credit-card` and `generic-assignment` are the noisiest patterns.** A 16-digit order number that happens to match a card prefix, or a `password = "..."` line with a placeholder, will fire. Filter with `secret_kinds` to exclude them if needed: `has_secrets(body) && !("credit-card" in secret_kinds(body))`.
- **No git-history walking.** This scans the working-tree file content only. A secret committed and later removed is invisible — use gitleaks for history.
- **Requires `--body`.** Both functions take the `body` variable; without `--body` the body is empty and they always return false / empty.
