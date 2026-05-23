package content

import (
	"testing"
)

func TestLookupAppName(t *testing.T) {
	tests := []struct {
		name  string
		extra Attributes
		path  string
		want  string
	}{
		{
			name:  "firefox-places-by-app-id",
			extra: Attributes{"sqlite_application_id": int64(0x0FACADE0)},
			path:  "/Users/alice/Library/Application Support/Firefox/Profiles/abc/places.sqlite",
			want:  "firefox-places",
		},
		{
			name:  "fossil-scm-by-app-id",
			extra: Attributes{"sqlite_application_id": int64(0x66747261)},
			path:  "/repos/something.fossil",
			want:  "fossil-scm",
		},
		{
			name:  "macos-libcache-user-version-plus-filename",
			extra: Attributes{"sqlite_user_version": int64(203)},
			path:  "/Users/alice/Library/Caches/com.apple.akd/Cache.db",
			want:  "macos-libcache",
		},
		{
			name:  "macos-libcache-needs-both-uv-and-filename",
			extra: Attributes{"sqlite_user_version": int64(203)},
			// Right user_version but wrong filename — must NOT fire.
			path: "/Users/alice/Library/Caches/com.apple.akd/random.db",
			want: "",
		},
		{
			name:  "chrome-history-filename-plus-path",
			extra: Attributes{},
			path:  "/Users/alice/Library/Application Support/Google/Chrome/Default/History",
			want: "chrome-history",
		},
		{
			name:  "chromium-history-different-vendor",
			extra: Attributes{},
			path:  "/Users/alice/Library/Application Support/Chromium/Default/History",
			want:  "chromium-history",
		},
		{
			name:  "edge-history",
			extra: Attributes{},
			path:  "/Users/alice/Library/Application Support/Microsoft Edge/Default/History",
			want:  "edge-history",
		},
		{
			name:  "brave-cookies",
			extra: Attributes{},
			path:  "/Users/alice/Library/Application Support/BraveSoftware/Brave-Browser/Default/Cookies",
			want:  "brave-cookies",
		},
		{
			name:  "history-no-vendor-no-match",
			extra: Attributes{},
			// Filename matches "History" but path doesn't contain any
			// vendor — must NOT classify as chrome / chromium / etc.
			path: "/Users/alice/Documents/History",
			want: "",
		},
		{
			name:  "apple-photos-by-filename",
			extra: Attributes{},
			path:  "/Users/alice/Pictures/Photos Library.photoslibrary/database/Photos.sqlite",
			want:  "apple-photos",
		},
		{
			name:  "apple-imessage-by-filename-and-path",
			extra: Attributes{},
			path:  "/Users/alice/Library/Messages/chat.db",
			want:  "apple-imessage",
		},
		{
			name:  "chat.db-outside-messages-no-match",
			extra: Attributes{},
			// `chat.db` outside the Messages directory shouldn't get
			// the iMessage label — it could be any random chat app.
			path: "/Users/alice/Library/SomeOtherApp/chat.db",
			want: "",
		},
		{
			name:  "apple-keychain-by-extension",
			extra: Attributes{},
			path:  "/Users/alice/Library/Keychains/login.keychain-db",
			want:  "apple-keychain",
		},
		{
			name:  "unknown-app-empty-result",
			extra: Attributes{"sqlite_application_id": int64(0xDEADBEEF)},
			path:  "/Users/alice/Documents/mystery.db",
			want:  "",
		},
		{
			name:  "missing-extras-empty-result",
			extra: nil,
			path:  "/Users/alice/Documents/anonymous.db",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lookupAppName(tc.extra, tc.path)
			if got != tc.want {
				t.Errorf("lookupAppName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRegistryAllEntriesHaveNames is a sanity check against authoring
// errors — an entry without a Name would silently disable itself.
func TestRegistryAllEntriesHaveNames(t *testing.T) {
	for i, entry := range sqliteAppRegistry {
		if entry.Name == "" {
			t.Errorf("sqliteAppRegistry[%d] has empty Name", i)
		}
		// Every entry must have at least one matching dimension —
		// the all-zeros entry would catch every SQLite file.
		if entry.ApplicationID == 0 && entry.UserVersion == 0 &&
			entry.Filename == "" && entry.PathContains == "" {
			t.Errorf("sqliteAppRegistry[%d] (%s) has no match dimensions — would fire for every file",
				i, entry.Name)
		}
	}
}
