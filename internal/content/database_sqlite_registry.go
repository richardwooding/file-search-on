package content

import (
	"path/filepath"
	"strings"
)

// sqliteApp is one entry in the curated known-application registry.
// All non-zero / non-empty fields are required to match — entries
// combine the fields (AND, not OR) so a strict (ApplicationID +
// PathContains) pattern fires only when both are present.
//
// Field semantics:
//
//   - ApplicationID — matches the SQLite header's `PRAGMA application_id`
//     stamp. Zero means "ignore this dimension"; SQLite reserves 0 for
//     unstamped DBs so this is unambiguous.
//   - UserVersion — matches `PRAGMA user_version`. Zero means ignore.
//     Apps that use user_version as a real schema version (incrementing
//     over time) shouldn't be pinned via this field alone; pair with
//     Filename or PathContains.
//   - Filename — matches the file's basename exactly (case-sensitive).
//     Used for stamped-by-convention DBs like Chrome "History" / "Cookies"
//     that don't bother with application_id but ALWAYS have the same name.
//   - PathContains — matches when the file's absolute path contains this
//     substring (case-sensitive). Useful for disambiguating common names
//     like "History" — pair with PathContains: "Chrome" / "Chromium".
//
// Empty Name is a registry author error and would silently exclude the
// entry; package-init sanity check guards against that.
type sqliteApp struct {
	ApplicationID uint32
	UserVersion   uint32
	Filename      string
	PathContains  string
	Name          string
}

// sqliteAppRegistry is the curated map from SQLite stamps + filename
// conventions to human-readable app names. Order matters: the FIRST
// matching entry wins, so more-specific entries (with more fields set)
// must appear before catch-alls.
//
// Adding an entry is a one-line literal append. Keep entries in rough
// vendor order: browsers, then OS-bundled stores, then dev tools.
//
// Magic-number sources:
//
//   - Firefox `places.sqlite` — application_id 0x0FACADE0 is documented
//     in mozilla-central toolkit/components/places/Database.cpp.
//   - Fossil SCM — application_id 0x66747261 is the ASCII bytes "fossil"'s
//     first 4 chars in big-endian; documented in fossil-scm/src.
//   - macOS libcache — user_version 203 with basename Cache.db is observed
//     consistently across com.apple.* /Library/Caches directories.
//   - Chrome History / Cookies — Chromium does NOT stamp application_id;
//     identified by canonical filenames inside a Chrome / Chromium profile.
//   - Apple Keychain — `.keychain-db` extension is the macOS Sierra+
//     SQLite-backed keychain format.
//   - iMessage `chat.db` — Apple does NOT stamp application_id; identified
//     by the canonical filename inside the Messages directory.
//   - iOS / macOS Photos — `Photos.sqlite` is the canonical filename in
//     Photo Library packages.
var sqliteAppRegistry = []sqliteApp{
	// Browsers — application_id-stamped.
	{ApplicationID: 0x0FACADE0, Name: "firefox-places"},

	// Browsers — filename-stamped (Chromium family).
	{Filename: "History", PathContains: "Chrome", Name: "chrome-history"},
	{Filename: "History", PathContains: "Chromium", Name: "chromium-history"},
	{Filename: "History", PathContains: "Edge", Name: "edge-history"},
	{Filename: "History", PathContains: "Brave", Name: "brave-history"},
	{Filename: "Cookies", PathContains: "Chrome", Name: "chrome-cookies"},
	{Filename: "Cookies", PathContains: "Chromium", Name: "chromium-cookies"},
	{Filename: "Cookies", PathContains: "Edge", Name: "edge-cookies"},
	{Filename: "Cookies", PathContains: "Brave", Name: "brave-cookies"},

	// macOS-bundled stores.
	{UserVersion: 203, Filename: "Cache.db", Name: "macos-libcache"},
	{Filename: "Photos.sqlite", Name: "apple-photos"},
	{Filename: "chat.db", PathContains: "Messages", Name: "apple-imessage"},
	// Apple Keychain Services SQLite stores — keychain-2.db plus the
	// TrustedPeersHelper.db sub-service stores under
	// `~/Library/Keychains/<UUID>/`. The user login keychain
	// (`login.keychain-db`) is still the legacy binary format on
	// most systems and reaches this lookup only if Apple ever migrates
	// it to SQLite, so the extension-based fallback at the bottom
	// of lookupAppName is kept too.
	{PathContains: "/Library/Keychains/", Name: "apple-keychain"},

	// Developer tools.
	{ApplicationID: 0x66747261, Name: "fossil-scm"},
}

// lookupAppName returns the canonical name for a SQLite file given its
// parsed header extras and absolute path, or "" when no registry entry
// matches. Pure function — exercised directly by unit tests; the
// header parser threads (extras, path) through after the 100-byte
// header walk completes.
func lookupAppName(extras Attributes, path string) string {
	var appID, userVersion uint32
	if v, ok := extras["sqlite_application_id"].(int64); ok && v > 0 {
		appID = uint32(v)
	}
	if v, ok := extras["sqlite_user_version"].(int64); ok && v > 0 {
		userVersion = uint32(v)
	}
	basename := filepath.Base(path)

	for _, entry := range sqliteAppRegistry {
		if entry.ApplicationID != 0 && entry.ApplicationID != appID {
			continue
		}
		if entry.UserVersion != 0 && entry.UserVersion != userVersion {
			continue
		}
		if entry.Filename != "" && entry.Filename != basename {
			continue
		}
		if entry.PathContains != "" && !strings.Contains(path, entry.PathContains) {
			continue
		}
		return entry.Name
	}
	// Apple Keychain: detect by extension (`.keychain-db`) rather than
	// by adding the extension as a generic dimension to sqliteApp —
	// extensions are a per-OS oddity that doesn't generalise.
	if strings.HasSuffix(basename, ".keychain-db") {
		return "apple-keychain"
	}
	return ""
}
