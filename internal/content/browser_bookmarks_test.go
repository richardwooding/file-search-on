package content

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"howett.net/plist"
)

// buildChromiumBookmarksFixture synthesises a realistic Chromium
// Bookmarks JSON shape with a bookmark_bar containing a mix of URLs
// and a nested folder.
func buildChromiumBookmarksFixture(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"checksum": "abc123",
		"version":  1,
		"roots": map[string]any{
			"bookmark_bar": map[string]any{
				"type": "folder",
				"name": "Bookmarks bar",
				"children": []any{
					map[string]any{
						"type": "url",
						"name": "kubernetes the hard way",
						"url":  "https://github.com/kelseyhightower/kubernetes-the-hard-way",
					},
					map[string]any{
						"type": "url",
						"name": "go.dev",
						"url":  "https://go.dev",
					},
					map[string]any{
						"type": "folder",
						"name": "Reading List",
						"children": []any{
							map[string]any{
								"type": "url",
								"name": "transformer architecture",
								"url":  "https://arxiv.org/abs/1706.03762",
							},
						},
					},
				},
			},
			"other": map[string]any{
				"type":     "folder",
				"name":     "Other bookmarks",
				"children": []any{},
			},
			"synced": map[string]any{
				"type":     "folder",
				"name":     "Mobile bookmarks",
				"children": []any{},
			},
		},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return out
}

func TestChromiumBookmarks_Detection(t *testing.T) {
	data := buildChromiumBookmarksFixture(t)
	fsys := fstest.MapFS{
		"Default/Bookmarks": {Data: data},
	}
	ct := DefaultRegistry().Detect(fsys, "Default/Bookmarks")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "browser/bookmarks-chromium" {
		t.Errorf("ct.Name() = %q, want browser/bookmarks-chromium", ct.Name())
	}
}

func TestChromiumBookmarks_Attributes(t *testing.T) {
	data := buildChromiumBookmarksFixture(t)
	attrs := parseChromiumBookmarks(data, "Default/Bookmarks")

	if got := attrs["bookmark_count"]; got != int64(3) {
		t.Errorf("bookmark_count = %v, want 3", got)
	}
	if got := attrs["bookmark_folder_count"]; got != int64(4) {
		// bookmark_bar, other, synced, Reading List = 4 folders
		t.Errorf("bookmark_folder_count = %v, want 4", got)
	}
	urls, _ := attrs["bookmark_urls"].([]string)
	if len(urls) != 3 {
		t.Errorf("bookmark_urls length = %d, want 3 (%v)", len(urls), urls)
	}
	titles, _ := attrs["bookmark_titles"].([]string)
	if len(titles) != 3 {
		t.Errorf("bookmark_titles length = %d, want 3", len(titles))
	}
	folders, _ := attrs["bookmark_folders"].([]string)
	wantFolders := map[string]bool{
		"Bookmarks bar":    true,
		"Other bookmarks":  true,
		"Mobile bookmarks": true,
		"Reading List":     true,
	}
	if len(folders) != len(wantFolders) {
		t.Errorf("folder set = %v, want %d entries", folders, len(wantFolders))
	}
	for _, f := range folders {
		if !wantFolders[f] {
			t.Errorf("unexpected folder %q", f)
		}
	}
	if got := attrs["bookmark_profile"]; got != "Default" {
		t.Errorf("bookmark_profile = %v, want Default", got)
	}
}

func TestChromiumBookmarks_RejectsNonBookmarkJSON(t *testing.T) {
	// A JSON file named `Bookmarks` but without the roots.bookmark_bar
	// shape — common defensive case (user creates a random file).
	junk := []byte(`{"some": "other", "json": "shape"}`)
	attrs := parseChromiumBookmarks(junk, "Default/Bookmarks")
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for non-bookmarks JSON, got %v", attrs)
	}
}

func TestChromiumBookmarks_MalformedJSON(t *testing.T) {
	attrs := parseChromiumBookmarks([]byte("not json at all"), "Default/Bookmarks")
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for malformed JSON, got %v", attrs)
	}
}

func TestChromiumBookmarks_DepthCap(t *testing.T) {
	// Build a deeply-nested tree: 100 levels.
	root := map[string]any{
		"type": "folder",
		"name": "root",
	}
	current := root
	for i := range 200 {
		child := map[string]any{
			"type": "folder",
			"name": "depth-" + string(rune('a'+i%26)),
		}
		current["children"] = []any{child}
		current = child
	}
	// Bottom URL — should be unreachable past depth cap.
	current["children"] = []any{
		map[string]any{
			"type": "url",
			"name": "unreachable",
			"url":  "https://buried-too-deep.example",
		},
	}

	doc := map[string]any{
		"roots": map[string]any{
			"bookmark_bar": root,
		},
	}
	data, _ := json.Marshal(doc)
	attrs := parseChromiumBookmarks(data, "x/Bookmarks")
	urls, _ := attrs["bookmark_urls"].([]string)
	// The bottom URL is past bookmarkMaxDepth (64) so shouldn't appear.
	for _, u := range urls {
		if strings.Contains(u, "buried-too-deep") {
			t.Error("URL past depth cap should not be collected")
		}
	}
}

func TestChromiumBookmarks_BodyExtraction(t *testing.T) {
	data := buildChromiumBookmarksFixture(t)
	fsys := fstest.MapFS{"Default/Bookmarks": {Data: data}}
	body, err := chromiumBookmarksBody(context.Background(), fsys, "Default/Bookmarks", 1<<20)
	if err != nil {
		t.Fatalf("body error: %v", err)
	}
	for _, want := range []string{
		"kubernetes the hard way",
		"https://github.com/kelseyhightower/kubernetes-the-hard-way",
		"go.dev",
		"transformer architecture",
		"Reading List",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestChromiumProfileFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"Default/Bookmarks", "Default"},
		{"Profile 1/Bookmarks", "Profile 1"},
		{"/Users/x/Library/Application Support/Google/Chrome/Default/Bookmarks", "Default"},
		{"Bookmarks", ""},
	}
	for _, tc := range tests {
		got := chromiumProfileFromPath(tc.path)
		if got != tc.want {
			t.Errorf("chromiumProfileFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// buildSafariBookmarksFixture synthesises a binary plist mirroring the
// real Safari Bookmarks.plist shape: a root WebBookmarkTypeList with
// nested children mixing leaves (URLs) and lists (folders).
func buildSafariBookmarksFixture(t *testing.T) []byte {
	t.Helper()
	root := map[string]any{
		"WebBookmarkType": "WebBookmarkTypeList",
		"Title":           "",
		"Children": []any{
			map[string]any{
				"WebBookmarkType": "WebBookmarkTypeList",
				"Title":           "BookmarksBar",
				"Children": []any{
					map[string]any{
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://apple.com",
						"URIDictionary":   map[string]any{"title": "Apple"},
					},
					map[string]any{
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://developer.apple.com/swift",
						"URIDictionary":   map[string]any{"title": "Swift"},
					},
					map[string]any{
						"WebBookmarkType": "WebBookmarkTypeList",
						"Title":           "Reading List",
						"Children": []any{
							map[string]any{
								"WebBookmarkType": "WebBookmarkTypeLeaf",
								"URLString":       "https://arxiv.org/abs/1706.03762",
								"URIDictionary":   map[string]any{"title": "transformer paper"},
							},
						},
					},
				},
			},
			map[string]any{
				// Proxy entries (Reading List system folder, History) — should be SKIPPED.
				"WebBookmarkType": "WebBookmarkTypeProxy",
				"Title":           "History",
			},
		},
	}
	var buf bytes.Buffer
	enc := plist.NewEncoderForFormat(&buf, plist.BinaryFormat)
	if err := enc.Encode(root); err != nil {
		t.Fatalf("plist encode: %v", err)
	}
	return buf.Bytes()
}

func TestSafariBookmarks_Detection(t *testing.T) {
	data := buildSafariBookmarksFixture(t)
	fsys := fstest.MapFS{"Bookmarks.plist": {Data: data}}
	ct := DefaultRegistry().Detect(fsys, "Bookmarks.plist")
	if ct == nil {
		t.Fatal("Detect returned nil")
	}
	if ct.Name() != "browser/bookmarks-safari" {
		t.Errorf("ct.Name() = %q, want browser/bookmarks-safari", ct.Name())
	}
}

func TestSafariBookmarks_Attributes(t *testing.T) {
	data := buildSafariBookmarksFixture(t)
	attrs := parseSafariBookmarks(data)

	if got := attrs["bookmark_count"]; got != int64(3) {
		t.Errorf("bookmark_count = %v, want 3", got)
	}
	urls, _ := attrs["bookmark_urls"].([]string)
	if len(urls) != 3 {
		t.Errorf("bookmark_urls length = %d, want 3", len(urls))
	}
	titles, _ := attrs["bookmark_titles"].([]string)
	wantTitles := []string{"Apple", "Swift", "transformer paper"}
	if len(titles) != len(wantTitles) {
		t.Fatalf("titles = %v, want %v", titles, wantTitles)
	}
}

func TestSafariBookmarks_SkipsProxyEntries(t *testing.T) {
	data := buildSafariBookmarksFixture(t)
	attrs := parseSafariBookmarks(data)
	folders, _ := attrs["bookmark_folders"].([]string)
	for _, f := range folders {
		if f == "History" {
			t.Error("WebBookmarkTypeProxy folder 'History' should be skipped")
		}
	}
}

func TestSafariBookmarks_RejectsNonBookmarkPlist(t *testing.T) {
	// A plist without WebBookmarkType key — the discriminator check.
	var buf bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf, plist.BinaryFormat).Encode(map[string]any{
		"some": "other plist",
	})
	attrs := parseSafariBookmarks(buf.Bytes())
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for non-bookmarks plist, got %v", attrs)
	}
}

func TestBookmarkBody_AlignsTitlesAndURLs(t *testing.T) {
	attrs := Attributes{
		"bookmark_urls":    []string{"https://a.com", "https://b.com"},
		"bookmark_titles":  []string{"A site", "B site"},
		"bookmark_folders": []string{"Misc"},
	}
	body := bookmarkBody(attrs, 1<<20)
	wantLines := []string{
		"A site\thttps://a.com",
		"B site\thttps://b.com",
		"Misc",
	}
	for _, w := range wantLines {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\nbody:\n%s", w, body)
		}
	}
}

func TestBookmarkBody_RespectsMaxBytes(t *testing.T) {
	attrs := Attributes{
		"bookmark_urls":   []string{"https://very-long-url.example.com/path/1", "https://x.example/2"},
		"bookmark_titles": []string{"title one is somewhat long", "title two"},
	}
	body := bookmarkBody(attrs, 30)
	if len(body) > 30 {
		t.Errorf("body length %d exceeds cap 30", len(body))
	}
}

func TestLookupBrowserVendor(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/Users/x/Library/Application Support/Google/Chrome/Default/Bookmarks", "chrome"},
		{"/Users/x/Library/Application Support/BraveSoftware/Brave-Browser/Default/Bookmarks", "brave"},
		{"/Users/x/Library/Application Support/Microsoft Edge/Default/Bookmarks", "edge"},
		{"/Users/x/Library/Application Support/Chromium/Default/Bookmarks", "chromium"},
		{"/Users/x/Library/Safari/Bookmarks.plist", "safari"},
		{"/Users/x/Library/Application Support/Arc/User Data/Default/Bookmarks", "arc"},
		{"/Users/x/Documents/Bookmarks", ""},
	}
	for _, tc := range tests {
		got := LookupBrowserVendor(tc.path)
		if got != tc.want {
			t.Errorf("LookupBrowserVendor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
