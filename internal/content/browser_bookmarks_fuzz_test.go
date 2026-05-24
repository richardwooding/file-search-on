package content

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"howett.net/plist"
)

// fuzzBookmarkInputCap bounds the per-input size so a fuzz-mutator
// can't drive a single exec past the worker grace window. 4 KiB is
// enough to exercise nested structures, malformed JSON / plist,
// missing-key paths, and the depth-cap guard — without giving the
// mutator room to construct megabyte-shaped pathological inputs.
const fuzzBookmarkInputCap = 4 * 1024

// FuzzParseChromiumBookmarks targets the Chromium JSON walker. The
// recursive walker has a depth cap + node cap, but the JSON decoder
// itself can do quadratic work on adversarial nesting. Contract:
// never panic, never collect past the URL / title / folder caps.
func FuzzParseChromiumBookmarks(f *testing.F) {
	// Seed 1: valid minimal bookmarks shape.
	minimal := map[string]any{
		"roots": map[string]any{
			"bookmark_bar": map[string]any{
				"type": "folder",
				"name": "Bookmarks bar",
				"children": []any{
					map[string]any{"type": "url", "name": "ex", "url": "https://example.com"},
				},
			},
		},
	}
	if data, err := json.Marshal(minimal); err == nil {
		f.Add(data, "Default/Bookmarks")
	}

	// Seed 2: empty JSON object.
	f.Add([]byte(`{}`), "Default/Bookmarks")

	// Seed 3: malformed JSON.
	f.Add([]byte(`{"roots": not-valid-json`), "x/Bookmarks")

	// Seed 4: noise.
	junk := bytes.Repeat([]byte{0xFF}, 64)
	f.Add(junk, "Bookmarks")

	// Seed 5: very deep nesting with the right shape.
	var deep strings.Builder
	deep.WriteString(`{"roots":{"bookmark_bar":{"type":"folder","name":"x","children":[`)
	for range 80 {
		deep.WriteString(`{"type":"folder","name":"x","children":[`)
	}
	for range 80 {
		deep.WriteString(`]}`)
	}
	deep.WriteString(`]}}}`)
	f.Add([]byte(deep.String()), "Bookmarks")

	f.Fuzz(func(t *testing.T, data []byte, path string) {
		if len(data) > fuzzBookmarkInputCap {
			return
		}
		attrs := parseChromiumBookmarks(data, path)
		// Contract checks.
		if urls, ok := attrs["bookmark_urls"].([]string); ok && len(urls) > bookmarkMaxURLs {
			t.Fatalf("bookmark_urls exceeds cap: %d > %d", len(urls), bookmarkMaxURLs)
		}
		if titles, ok := attrs["bookmark_titles"].([]string); ok && len(titles) > bookmarkMaxTitles {
			t.Fatalf("bookmark_titles exceeds cap: %d > %d", len(titles), bookmarkMaxTitles)
		}
		if folders, ok := attrs["bookmark_folders"].([]string); ok && len(folders) > bookmarkMaxFolders {
			t.Fatalf("bookmark_folders exceeds cap: %d > %d", len(folders), bookmarkMaxFolders)
		}
	})
}

// FuzzParseSafariBookmarks targets the Safari plist walker. Same
// contract as the Chromium target — never panic, never collect past
// the surface caps.
func FuzzParseSafariBookmarks(f *testing.F) {
	// Seed 1: valid minimal bookmarks shape.
	minimal := map[string]any{
		"WebBookmarkType": "WebBookmarkTypeList",
		"Children": []any{
			map[string]any{
				"WebBookmarkType": "WebBookmarkTypeLeaf",
				"URLString":       "https://example.com",
				"URIDictionary":   map[string]any{"title": "ex"},
			},
		},
	}
	var buf bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf, plist.BinaryFormat).Encode(minimal)
	f.Add(buf.Bytes())

	// Seed 2: plist without WebBookmarkType (rejected by discriminator).
	var buf2 bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf2, plist.BinaryFormat).Encode(map[string]any{
		"k": "v",
	})
	f.Add(buf2.Bytes())

	// Seed 3: XML plist variant.
	var buf3 bytes.Buffer
	_ = plist.NewEncoderForFormat(&buf3, plist.XMLFormat).Encode(minimal)
	f.Add(buf3.Bytes())

	// Seed 4: noise.
	f.Add(bytes.Repeat([]byte{0xFF}, 64))

	// Seed 5: bplist00 magic + truncated.
	f.Add([]byte("bplist00\x01\x02\x03"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzBookmarkInputCap {
			return
		}
		attrs := parseSafariBookmarks(data)
		if urls, ok := attrs["bookmark_urls"].([]string); ok && len(urls) > bookmarkMaxURLs {
			t.Fatalf("bookmark_urls exceeds cap: %d > %d", len(urls), bookmarkMaxURLs)
		}
		if titles, ok := attrs["bookmark_titles"].([]string); ok && len(titles) > bookmarkMaxTitles {
			t.Fatalf("bookmark_titles exceeds cap: %d > %d", len(titles), bookmarkMaxTitles)
		}
		if folders, ok := attrs["bookmark_folders"].([]string); ok && len(folders) > bookmarkMaxFolders {
			t.Fatalf("bookmark_folders exceeds cap: %d > %d", len(folders), bookmarkMaxFolders)
		}
	})
}
