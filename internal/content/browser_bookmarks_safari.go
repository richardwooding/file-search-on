package content

import (
	"context"
	"io"
	"io/fs"

	"howett.net/plist"
)

// Safari stores its bookmarks in a single binary plist at
// `~/Library/Safari/Bookmarks.plist`. Layout per Apple's WebKit
// WebBookmarkType conventions:
//
//	{
//	  "WebBookmarkType":     "WebBookmarkTypeList",
//	  "Title":               "BookmarksBar",
//	  "Children":            [ ... nested nodes ... ]
//	}
//
// Each node carries a `WebBookmarkType` discriminator:
//   - `WebBookmarkTypeLeaf` — a bookmark; `URLString` + `URIDictionary.title`
//   - `WebBookmarkTypeList` — a folder; `Title` + `Children`
//   - `WebBookmarkTypeProxy` — system folder (Reading List, History);
//     skipped — we surface user bookmarks only.
//
// Detection mirrors the Chromium type: FilenameMatcher-exact on
// `Bookmarks.plist`. That beats both `.plist` extension dispatch and
// the binary `bplist00` magic. Any random file named `Bookmarks.plist`
// that ISN'T Safari bookmarks will surface empty attrs from the
// content discriminator (root must carry `WebBookmarkType`).

const safariBookmarksReadCap = 16 << 20 // 16 MiB; Safari plist can be larger than Chromium JSON

func init() {
	Register(&safariBookmarksType{})
}

type safariBookmarksType struct{}

func (*safariBookmarksType) Name() string         { return "browser/bookmarks-safari" }
func (*safariBookmarksType) Extensions() []string { return nil }
func (*safariBookmarksType) MagicBytes() [][]byte { return nil }
func (*safariBookmarksType) Filenames() []string  { return []string{"Bookmarks.plist"} }

func (*safariBookmarksType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, safariBookmarksReadCap))
	if err != nil {
		return Attributes{}, nil
	}
	return parseSafariBookmarks(buf), nil
}

// safariBookmarksBody is the ExtractBody entry point for the Safari
// content type. Mirrors chromiumBookmarksBody — one "title<TAB>url"
// line per bookmark, capped at maxBytes.
func safariBookmarksBody(ctx context.Context, fsys fs.FS, path string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, safariBookmarksReadCap))
	if err != nil {
		return "", nil
	}
	return bookmarkBody(parseSafariBookmarks(buf), maxBytes), nil
}

// parseSafariBookmarks decodes the plist via howett.net/plist (the
// existing parser from #185) and walks the recursive Children tree.
// Pure function — exercised by tests + fuzz.
func parseSafariBookmarks(data []byte) Attributes {
	var root any
	if _, err := plist.Unmarshal(data, &root); err != nil {
		return Attributes{}
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return Attributes{}
	}
	// Discriminator: real Safari bookmarks file has WebBookmarkType at
	// the root. Filename-only match would otherwise misclassify any
	// random plist named Bookmarks.plist.
	if _, ok := dict["WebBookmarkType"]; !ok {
		return Attributes{}
	}

	collected := bookmarkCollection{}
	walkSafariNode(dict, 0, &collected)
	return collected.toAttributes()
}

// walkSafariNode is the bounded recursive walker. Reads
// WebBookmarkType to discriminate leaf (URL) from list (folder); skips
// WebBookmarkTypeProxy (Reading List / History — system entries).
func walkSafariNode(node map[string]any, depth int, c *bookmarkCollection) {
	if depth > bookmarkMaxDepth || c.NodeCount >= bookmarkMaxNodes {
		return
	}
	c.NodeCount++

	kind, _ := node["WebBookmarkType"].(string)
	switch kind {
	case "WebBookmarkTypeLeaf":
		url, _ := node["URLString"].(string)
		var title string
		if uri, ok := node["URIDictionary"].(map[string]any); ok {
			title, _ = uri["title"].(string)
		}
		c.addURL(url, title)

	case "WebBookmarkTypeList":
		title, _ := node["Title"].(string)
		// Root-list nodes typically have Title="" or "BookmarksBar".
		// Leave the empty-name filter to addFolder.
		c.addFolder(title)
		children, _ := node["Children"].([]any)
		for _, child := range children {
			childDict, ok := child.(map[string]any)
			if !ok {
				continue
			}
			walkSafariNode(childDict, depth+1, c)
			if c.NodeCount >= bookmarkMaxNodes {
				return
			}
		}
	}
	// WebBookmarkTypeProxy and any unknown discriminator: skip.
}
