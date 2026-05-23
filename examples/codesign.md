# Recipes — Mach-O code signatures

Every Mach-O binary on macOS — every system tool under `/usr/lib`, every framework inside `*.app/Contents/Frameworks/`, every CLI under `/usr/local/bin` — carries an `LC_CODE_SIGNATURE` load command. The parser walks the SuperBlob, decodes the CodeDirectory, and extracts the embedded entitlements plist. The surfaced attributes turn the existing `binary/mach-o` detection into a forensic-grade triage capability.

Detection scope: macOS-specific. ELF (Linux/BSD) and PE (Windows) binaries always surface `is_codesigned = false` because their signature formats are different (GnuPG signatures sit alongside, not embedded; Authenticode lives in a separate PE section).

## Find unsigned binaries (malware indicator)

```sh
# Every unsigned Mach-O under /Applications — anything modern from a legitimate vendor SHOULD be signed
file-search-on 'is_mach_o && !is_codesigned' -d /Applications

# Same but excluding archives / dev artefacts
file-search-on 'is_mach_o && !is_codesigned' -d /Applications --exclude '*.dSYM/*'
```

## Inventory by vendor (Team ID)

```sh
# Find apps from a specific Apple Developer Team ID
file-search-on 'is_codesigned && codesign_team_id == "EQHXZ8M8AV"' -d /Applications

# Anthropic-signed binaries (Q6L2SF6YDW)
file-search-on 'is_codesigned && codesign_team_id == "Q6L2SF6YDW"' -d /Applications

# Group every signed binary by Team ID
file-search-on 'is_codesigned && codesign_team_id != ""' -d /Applications -o json | \
  jq -r '.codesign_team_id' | sort | uniq -c | sort -rn
```

## Apple-signed vs third-party

```sh
# Apple-signed binaries (no Team ID, not adhoc)
file-search-on 'is_apple_signed' -d /usr/lib

# Third-party signed binaries (any Apple Developer ID)
file-search-on 'is_third_party_signed' -d /Applications

# Adhoc-signed binaries (local dev / homebrew / `codesign -s -`)
file-search-on 'is_codesigned && codesign_adhoc' -d /usr/local/bin
```

## Security baseline audits

```sh
# Binaries without hardened runtime — security-baseline regression candidates
file-search-on 'is_codesigned && !codesign_hardened_runtime' -d /Applications

# Binaries with library validation disabled — they can load arbitrary unsigned dylibs
file-search-on 'is_codesigned && codesign_hardened_runtime && !codesign_library_validation' -d /Applications

# Legacy SHA1-signed binaries (modern Apple binaries use SHA256)
file-search-on 'is_codesigned && codesign_hash_type == "sha1"' -d /usr/lib
```

## Entitlement triage

```sh
# Binaries claiming Full Disk Access — they can read every file the user can
file-search-on 'is_codesigned && entitlement_full_disk_access' -d /Applications

# Sandboxed apps with inbound network listen — daemons running inside the sandbox
file-search-on 'entitlement_app_sandbox && entitlement_network_server' -d /Applications

# Apps with arbitrary user-selected file access
file-search-on 'is_codesigned && "com.apple.security.files.user-selected.read-write" in entitlements' -d /Applications

# Unsandboxed apps (Mac App Store apps MUST be sandboxed; non-MAS apps usually aren't)
file-search-on 'is_codesigned && !entitlement_app_sandbox' -d /Applications

# Apps requesting access to the user's contacts
file-search-on 'is_codesigned && "com.apple.security.personal-information.addressbook" in entitlements' -d /Applications

# Apps with the JIT-allowed entitlement (browsers, debuggers, VMs)
file-search-on 'is_codesigned && "com.apple.security.cs.allow-jit" in entitlements' -d /Applications
```

## Cross-family composition

`is_codesigned` composes with every other CEL filter — Info.plist (#185), hashes, time bucketing, sort + top-K.

```sh
# Match an Info.plist's bundle id against its binary's codesign identifier
# (correlation: are they consistent? a re-signed binary might disagree)
file-search-on 'is_codesigned && codesign_identifier == "com.anthropic.claudefordesktop"' -d /Applications

# Pair plist + codesign for full app metadata
file-search-on 'is_plist && plist_bundle_identifier == "com.example.app"' -d /Applications
file-search-on 'is_codesigned && codesign_identifier == "com.example.app"' -d /Applications

# Largest signed binaries on the system
file-search-on 'is_codesigned' -d /Applications --sort size --order desc --limit 10

# Compute hashes alongside signature info for forensic chain-of-custody
file-search-on 'is_codesigned' -d /Applications --with-hashes -o json --limit 5
```

## Known limitations

- **No cert-chain parsing in v1.** `codesign_authority` (the X.509 subject chain) isn't surfaced. The `is_apple_signed` heuristic uses `team_id == "" && !adhoc` — correct for the common case but doesn't validate the actual signer identity. A locally re-signed binary with an empty team_id would surface as Apple-signed.
- **Signature validity isn't checked.** We surface what the binary CLAIMS, not whether the kernel would accept the signature. Run `codesign --verify` for actual validation.
- **Fat binaries: first arch only.** Universal Mach-O carries one signature per architecture; we report the first one. Modern Apple Silicon usually only ships one (arm64 / arm64e) signature anyway.
- **Notarization status isn't surfaced.** Notarized apps require a stapled ticket file alongside the binary OR a live online check against Apple's notarisation servers. Out of scope.
- **No designated requirements.** The `CSMAGIC_REQUIREMENT_SET` blob carries an opaque binary expression language; agents rarely query it.
- **ELF / Windows binaries.** Linux GnuPG signatures and Windows Authenticode are separate format families — `is_codesigned` is Mach-O-specific.
