# Recipes — Apple property lists

Content type: `system/plist` — Apple property-list files (`.plist`) in either the binary (`bplist00` magic at offset 0) or XML encoding. The dominant config / metadata file format on macOS / iOS / tvOS / watchOS — every installed app ships at least one `Info.plist`, every LaunchAgent / LaunchDaemon is a plist, and `~/Library/Preferences/*.plist` holds every app's user preferences.

The parser uses [`howett.net/plist`](https://pkg.go.dev/howett.net/plist) (pure-Go) and surfaces a typed attribute set covering bundle identity (`CFBundleIdentifier`, `CFBundleShortVersionString`, …), executable info, minimum OS version, and the LaunchAgent / LaunchDaemon persistence keys.

`is_plist` fires for both encodings. `is_macos_metadata` fires alongside because plist is Apple-specific by convention even though the content type isn't named `system/macos-plist`. The cross-OS `is_system_metadata` umbrella fires too via the `system/` prefix.

## Find apps requiring a minimum macOS version

```sh
# Apps blocking my OS upgrade — Info.plist with LSMinimumSystemVersion >= 14.0
file-search-on 'is_plist && plist_kind == "info" && plist_min_os_version >= "14.0"' -d /Applications

# Sort by min-OS version desc to find the most aggressive cutoffs
file-search-on 'is_plist && plist_kind == "info" && plist_min_os_version != ""' -d /Applications \
  --sort plist_min_os_version --order desc -o json | \
  jq -r '"\(.plist_min_os_version)\t\(.plist_bundle_identifier)\t\(.path)"'
```

## Audit LaunchAgents and LaunchDaemons

LaunchAgents (per-user) and LaunchDaemons (system-wide) are the macOS persistence mechanism. Useful for triage: a malicious app installs a LaunchAgent so it runs on every login.

```sh
# Every LaunchAgent on the system (user + system + global)
file-search-on 'is_plist && plist_kind == "launch-agent"' -d ~/Library/LaunchAgents -d /Library/LaunchAgents

# Persistent agents — RunAtLoad AND KeepAlive (the strongest persistence pattern)
file-search-on 'is_plist && plist_run_at_load && plist_keep_alive' -d ~/Library/LaunchAgents -d /Library/LaunchAgents -o verbose

# LaunchAgents that shell out to Python — common malware-persistence pattern
file-search-on 'is_plist && plist_kind == "launch-agent" && plist_program.contains("python")' \
  -d ~/Library/LaunchAgents -d /Library/LaunchAgents

# LaunchAgents that source from an unexpected location (outside /usr or /Applications)
file-search-on 'is_plist && plist_kind == "launch-agent" && !plist_program.startsWith("/usr") && !plist_program.startsWith("/Applications")' \
  -d ~/Library/LaunchAgents -d /Library/LaunchAgents
```

## Find apps by bundle identifier

```sh
# Find the Info.plist for a specific bundle id
file-search-on 'is_plist && plist_bundle_identifier == "com.apple.Safari"' -d /Applications

# All Apple system apps (com.apple.* bundle ids)
file-search-on 'is_plist && plist_bundle_identifier.startsWith("com.apple.")' -d /Applications

# Apps from a specific vendor
file-search-on 'is_plist && plist_bundle_identifier.startsWith("com.anthropic.")' -d /Applications
```

## Version + executable inventory

```sh
# List every installed app's bundle id + short version + executable
file-search-on 'is_plist && plist_kind == "info"' -d /Applications -o json | \
  jq -r '"\(.plist_bundle_identifier)\t\(.plist_bundle_short_version)\t\(.plist_executable)"'

# Sort apps by version desc — find the newest builds
file-search-on 'is_plist && plist_kind == "info" && plist_bundle_short_version != ""' \
  -d /Applications --sort plist_bundle_short_version --order desc --limit 20
```

## Preferences triage

```sh
# Every user-level app preference plist
file-search-on 'is_plist && plist_kind == "preferences"' -d ~/Library/Preferences

# Find preferences for a specific bundle id (when CFBundleIdentifier is set inside)
file-search-on 'is_plist && plist_kind == "preferences" && plist_bundle_identifier == "com.apple.Safari"' \
  -d ~/Library/Preferences
```

## Surface unrecognised plists for manual triage

```sh
# Plists that the registry couldn't classify — useful for finding novel patterns
file-search-on 'is_plist && plist_kind == ""' -d ~/Library --limit 20

# Plists whose root isn't a dict — uncommon, often arrays for account lists or sync state
file-search-on 'is_plist && plist_root_kind != "dict"' -d ~/Library
```

## Cross-family queries

`is_plist` composes with every other CEL filter — time-bucket grouping, hash allowlists, sort + top-K, etc.

```sh
# Plists modified in the last week (e.g. recently-installed apps)
file-search-on 'is_plist && mod_time > timestamp("2026-05-16T00:00:00Z")' -d /Applications

# Largest plists on the system — sometimes a sign of corrupted preferences
file-search-on 'is_plist' -d ~/Library --sort size --order desc --limit 10

# Group plists by kind to see the breakdown
file-search-on stats 'is_plist' -d ~/Library --group-by plist_kind
```

## Known limitations

- **Read-only by design.** This tool never writes plists. Use `defaults write` or `plutil` for modifications.
- **Encrypted plists** (`encryptedplist` magic) — extremely rare, ~zero deployed footprint — surface as binary noise.
- **Arbitrary deep-key access.** The curated attribute set covers the high-value top-level keys (CFBundle*, LaunchAgent / LaunchDaemon basics). Plists with custom nested keys aren't exposed — agents wanting `MyCustomKey.subkey` can use `body.contains(...)` on the XML variant or fall back to `defaults read`.
- **`defaults` semantics aren't replicated.** `defaults read com.apple.Safari` merges values from user / host / global preference domains; this tool surfaces the bytes in one file at a time.
- **Path-based classification.** `plist_kind` is dominated by path heuristics — a LaunchAgent dropped outside `LaunchAgents/` won't classify as `launch-agent` even if its body has `Label` + `ProgramArguments`. Pair with explicit `plist_label != ""` filters when path is unreliable.
