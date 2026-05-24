package content

import "strings"

// browserVendor is one entry in the curated vendor-detection
// registry for browser bookmark files. The discriminator is purely
// path-based — Chromium forks share the JSON shape, so we can't tell
// Chrome from Brave from the file content alone.
//
// First-match wins; order entries with more-specific path tokens
// first (e.g. `Microsoft Edge` before any prefix that might also
// match `Edge`).
type browserVendor struct {
	// PathContains matches when the file's absolute display path
	// contains this substring (case-sensitive — vendor directory
	// names are stable on macOS / Linux / Windows).
	PathContains string
	// Vendor is the canonical short name surfaced as browser_vendor.
	Vendor string
}

// browserVendorRegistry maps directory-name fragments to canonical
// vendor labels. Mirrors the #177 SQLite app-name registry pattern.
// Adding a new browser is a one-line struct literal append.
var browserVendorRegistry = []browserVendor{
	// macOS Library / Application Support paths.
	{PathContains: "/Microsoft Edge/", Vendor: "edge"},
	{PathContains: "/BraveSoftware/", Vendor: "brave"},
	{PathContains: "/Google/Chrome/", Vendor: "chrome"},
	{PathContains: "/Chromium/", Vendor: "chromium"},
	{PathContains: "/com.operasoftware.Opera/", Vendor: "opera"},
	{PathContains: "/Vivaldi/", Vendor: "vivaldi"},
	{PathContains: "/Arc/", Vendor: "arc"},
	// Linux ~/.config layout.
	{PathContains: "/google-chrome/", Vendor: "chrome"},
	{PathContains: "/chromium/", Vendor: "chromium"},
	{PathContains: "/microsoft-edge/", Vendor: "edge"},
	{PathContains: "/BraveSoftware/Brave-Browser/", Vendor: "brave"},
	// Windows AppData layout (case-sensitive — Windows is case-
	// insensitive but we match the canonical casing here for
	// determinism; agents on Windows can lower-case the path before
	// matching if needed).
	{PathContains: `\Microsoft\Edge\`, Vendor: "edge"},
	{PathContains: `\Google\Chrome\`, Vendor: "chrome"},
	{PathContains: `\Chromium\`, Vendor: "chromium"},
	{PathContains: `\BraveSoftware\`, Vendor: "brave"},
	// Safari is the simplest — single canonical location.
	{PathContains: "/Library/Safari/", Vendor: "safari"},
}

// LookupBrowserVendor is the exported celexpr-layer hook called from
// BuildAttributesWith after a browser/bookmarks-* content type's
// Attributes pass. Takes the absolute display path; returns the
// matched vendor short name or "" when no path dimension fires.
//
// Same architecture as content.LookupSQLiteAppName (#177) and
// content.LookupPlistKindFromPath (#185) — path-based dimensions
// need displayPath, which ContentType.Attributes doesn't see.
func LookupBrowserVendor(displayPath string) string {
	for _, entry := range browserVendorRegistry {
		if strings.Contains(displayPath, entry.PathContains) {
			return entry.Vendor
		}
	}
	return ""
}
