# Recipes — Extended attributes (macOS)

Every macOS file carries metadata that lives alongside the file content but isn't visible to the content-type pipeline: **extended attributes** (xattrs). Two of them are forensic-grade signals nothing else in the project surfaces:

- **`com.apple.quarantine`** — Gatekeeper's record of where a downloaded file came from. Set by every browser, every email client, every messaging app. Records source URL, download date, agent name, and whether the user has manually approved running / opening the file.
- **`com.apple.metadata:_kMDItemUserTags`** — Finder's color tags + arbitrary user-added text tags. A binary plist stored as an xattr.

Plus the arbitrary xattr surface — apps can stash anything under reverse-DNS keys (`com.apple.cs.CodeRequirements-1`, `org.gpgtools.signature`, …).

**Opt-in**: `--with-xattrs` on the CLI, `with_xattrs: true` on MCP `search` / `read_attributes`. Off by default — two extra syscalls per file (Listxattr + Getxattr) add up across `/Applications`-shape walks. Darwin-only: non-Darwin builds silently leave the attrs empty.

## Quarantine: who, when, where from

```sh
# Every file downloaded from the web in the last week
file-search-on 'is_quarantined && quarantine_download_date > timestamp("2026-05-17T00:00:00Z")' --with-xattrs -d ~/Downloads

# Files downloaded from a specific vendor
file-search-on 'is_quarantined && quarantine_source_url.contains("github")' --with-xattrs -d ~/Downloads

# Group by downloading agent (Safari, Chrome, Mail, Slack, etc.)
file-search-on 'is_quarantined' --with-xattrs -d ~/Downloads -o json | \
  jq -r '.quarantine_agent' | sort | uniq -c | sort -rn

# Where did THIS file come from? Single-file lookup via MCP read_attributes
file-search-on '"com.apple.quarantine" in xattr_keys' --with-xattrs -d /path/to/file
```

## Malware-triage classics

The headline composition with the just-shipped code-signature work (#187):

```sh
# Unsigned Mach-O binaries downloaded from the web — "should I be worried?"
file-search-on 'binary_format == "mach-o" && !is_codesigned && is_quarantined' --with-xattrs

# Unsigned binaries the user has explicitly approved via Gatekeeper
file-search-on 'binary_format == "mach-o" && !is_codesigned && is_quarantined && quarantine_user_approved' --with-xattrs

# Apps with no team ID that the user downloaded from outside the App Store
file-search-on 'is_apple_signed == false && is_third_party_signed == false && is_quarantined' --with-xattrs -d /Applications

# Cross-vendor: files downloaded with Safari that ALSO have a code signature
file-search-on 'quarantine_agent == "Safari" && is_codesigned' --with-xattrs -d ~/Downloads
```

## Finder tags + colors

```sh
# Red-tagged files older than a year (cleanup candidates)
file-search-on 'finder_color == "red" && mod_time < timestamp("2025-05-24T00:00:00Z")' --with-xattrs

# Files tagged "work-2026"
file-search-on '"work-2026" in finder_tags' --with-xattrs -d ~/Documents

# Every coloured file under a project directory
file-search-on 'finder_color != ""' --with-xattrs -d ~/Code/MyProject

# Files with Finder comments (the comment text isn't surfaced; presence is)
file-search-on 'has_finder_comment' --with-xattrs -d ~/Documents
```

## Arbitrary xattr-key membership

```sh
# Files carrying a custom code-signing requirement xattr
file-search-on '"com.apple.cs.CodeRequirements-1" in xattr_keys' --with-xattrs

# Files Time Machine has touched (legacy xattr)
file-search-on '"com.apple.metadata:com_apple_backup_excludeItem" in xattr_keys' --with-xattrs

# Files with custom GPG signatures
file-search-on '"org.gpgtools.signature" in xattr_keys' --with-xattrs

# Outlier files with lots of xattrs (often custom-app state)
file-search-on 'xattr_count > 5' --with-xattrs --sort xattr_count --order desc --limit 20
```

## Forensic timeline reconstruction

```sh
# Every download from a specific browser in chronological order
file-search-on 'is_quarantined && quarantine_agent == "Safari"' --with-xattrs --sort quarantine_download_date --order asc -o json | \
  jq -r '"\(.quarantine_download_date)\t\(.quarantine_source_url // "(no URL)")\t\(.path)"'

# Files downloaded from a specific page (referrer URL)
file-search-on 'quarantine_referrer_url.contains("hacker-news")' --with-xattrs

# Downloads that bypassed Gatekeeper (no quarantine xattr but came from outside)
# — typically files copied off USB / network shares / drag-from-archive
file-search-on '!is_quarantined && binary_format == "mach-o"' --with-xattrs -d ~/Downloads
```

## Known limitations

- **macOS Sonoma+ sets `com.apple.provenance` on every new file** — even files you created locally. `is_xattr_rich` is therefore true for almost every file on a modern Mac; pair with `is_quarantined` / `has_finder_comment` for actionable signals.
- **Field 4 of the quarantine string is `quarantine_event_id`, not bundle id.** Apple's older docs called it the agent bundle id but modern browsers stuff a UUID there. Use as an opaque identifier; the real bundle id isn't recorded by modern macOS.
- **Quarantine `source_url` falls back to `kMDItemWhereFroms`** when the quarantine string's field 5 is empty (the modern Safari / Chrome shape). Both surfaces feed the same CEL variable.
- **Cross-platform**: Linux xattr support is out of scope for v1. Linux xattrs (`security.capability`, `security.selinux`, `user.*`) exist but rarely carry the agent-triage shape this feature targets.
- **Writing xattrs**: read-only by design.
- **Permission-denied files**: silently skipped (returns empty xattrs rather than failing the walk).
- **System-protected directories** (`~/Library/Safari`, `~/Library/Mail`): the CLI process needs Full Disk Access TCC permission to read xattrs there. Without it, the walk surfaces zero files.
