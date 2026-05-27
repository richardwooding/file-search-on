package content

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Chromium-family browser bookmarks live in a single JSON file named
// `Bookmarks` (no extension) per profile directory. Same shape across
// Chrome, Chromium, Edge, Brave, Opera, and any other Chromium fork.
// Tree layout (per chromium/src
// components/bookmarks/browser/bookmark_codec.cc):
//
//	{
//	  "checksum": "...",
//	  "version": 1,
//	  "roots": {
//	    "bookmark_bar": { ... bookmark / folder tree ... },
//	    "other":        { ... },
//	    "synced":       { ... }    (mobile bookmarks)
//	  }
//	}
//
// Each node has a `type` ("url" or "folder"), `name`, and for URL
// nodes a `url`; folders have a `children` array of nested nodes.
//
// Detection is FilenameMatcher-exact on basename `Bookmarks` — beats
// the JSON magic at offset 0 (`{`) and the `.json` extension dispatch
// because the detector runs exact-basename before extension or magic.
// Files literally named `Bookmarks` that AREN'T Chromium bookmarks
// (rare; user would need to create one by hand) detect as this type
// but produce empty attrs gracefully — Attributes returns zero-shape
// if the `roots.bookmark_bar` structure isn't present.

const (
	// chromiumBookmarksReadCap bounds disk reads. Bookmarks files are
	// typically a few hundred KB at most; the largest in the wild
	// (heavy users with thousands of bookmarks) rarely exceed 5 MB.
	// 8 MiB is generous; above that the walk degrades silently.
	chromiumBookmarksReadCap = 8 << 20

	// bookmarkMaxURLs / bookmarkMaxTitles cap the surface lists so
	// JSON wire shape stays predictable for sort / limit composition.
	// Capped at 1000 per the issue's spec.
	bookmarkMaxURLs    = 1000
	bookmarkMaxTitles  = 1000
	bookmarkMaxFolders = 100

	// bookmarkMaxDepth defends the recursive walker against adversarial
	// nesting. Real browser bookmark trees rarely exceed 10 levels;
	// 64 is generous and stops a fuzz mutator from blowing the stack.
	bookmarkMaxDepth = 64

	// bookmarkMaxNodes is a total-work cap independent of depth. A
	// pathological flat tree with millions of children would still
	// be bounded.
	bookmarkMaxNodes = 100_000
)

func init() {
	Register(&chromiumBookmarksType{})
}

type chromiumBookmarksType struct{}

func (*chromiumBookmarksType) Name() string         { return "browser/bookmarks-chromium" }
func (*chromiumBookmarksType) Extensions() []string { return nil }
func (*chromiumBookmarksType) MagicBytes() [][]byte { return nil }
func (*chromiumBookmarksType) Filenames() []string  { return []string{"Bookmarks"} }

func (*chromiumBookmarksType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, chromiumBookmarksReadCap))
	if err != nil {
		return Attributes{}, nil
	}
	return parseChromiumBookmarks(buf, path), nil
}

// parseChromiumBookmarks decodes the JSON, walks the `roots.*` trees,
// and assembles the CEL attribute map. Pure function — fuzz target
// exercises it directly. Returns an empty Attributes map (not nil) on
// any decoding failure or missing `roots.bookmark_bar` marker so the
// walker contract holds: a malformed file doesn't fail the walk.
func parseChromiumBookmarks(data []byte, path string) Attributes {
	var root chromiumBookmarksRoot
	if err := json.Unmarshal(data, &root); err != nil {
		return Attributes{}
	}
	if len(root.Roots) == 0 {
		return Attributes{}
	}
	// Discriminator: real Chromium files always carry `bookmark_bar`.
	if _, ok := root.Roots["bookmark_bar"]; !ok {
		return Attributes{}
	}

	collected := bookmarkCollection{}
	for _, treeRoot := range root.Roots {
		walkChromiumNode(&treeRoot, 0, &collected)
		if collected.NodeCount >= bookmarkMaxNodes {
			break
		}
	}

	out := collected.toAttributes()
	if profile := chromiumProfileFromPath(path); profile != "" {
		out["bookmark_profile"] = profile
	}
	return out
}

// chromiumBookmarksRoot is the top-level JSON shape. Decoded keys we
// don't use (checksum, version, sync_metadata, etc.) deliberately fall
// through to json.Unmarshal's ignore-unknown behaviour.
type chromiumBookmarksRoot struct {
	Roots map[string]chromiumNode `json:"roots"`
}

// chromiumNode is the recursive bookmark / folder node shape.
type chromiumNode struct {
	Type     string         `json:"type"`     // "url" or "folder"
	Name     string         `json:"name"`     // title for URLs, folder name for folders
	URL      string         `json:"url"`      // only set when type == "url"
	Children []chromiumNode `json:"children"` // only set when type == "folder"
}

// walkChromiumNode is the bounded recursive walker. Collects URLs,
// titles, and folder names; honours depth + total-node caps to defend
// against adversarial nesting / flat-tree fuzz input.
func walkChromiumNode(n *chromiumNode, depth int, c *bookmarkCollection) {
	if depth > bookmarkMaxDepth || c.NodeCount >= bookmarkMaxNodes {
		return
	}
	c.NodeCount++

	switch n.Type {
	case "url":
		c.addURL(n.URL, n.Name)
	case "folder":
		c.addFolder(n.Name)
		for i := range n.Children {
			walkChromiumNode(&n.Children[i], depth+1, c)
			if c.NodeCount >= bookmarkMaxNodes {
				return
			}
		}
	}
}

// chromiumBookmarksBody is the ExtractBody entry point for the
// Chromium content type. Emits one "title<TAB>url" line per bookmark
// so `body.contains("kubernetes")` matches when EITHER the URL or the
// title contains the substring. Capped at maxBytes; folder names
// included on their own lines for "Reading List"-style folder
// searches.
func chromiumBookmarksBody(ctx context.Context, fsys fs.FS, path string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, chromiumBookmarksReadCap))
	if err != nil {
		return "", nil
	}
	attrs := parseChromiumBookmarks(buf, path)
	return bookmarkBody(attrs, maxBytes), nil
}

// chromiumProfileFromPath extracts the Chromium profile directory name
// (`Default`, `Profile 1`, `Profile 2`, etc.) from the file's path.
// The Bookmarks file lives at `<vendor>/<profile>/Bookmarks`; the
// immediate parent directory is the profile. Returns "" when the
// path doesn't follow the expected shape (e.g. file lives at the
// search-root).
func chromiumProfileFromPath(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// bookmarkCollection aggregates walker output. Shared between the
// Chromium and Safari walkers — both formats collapse to the same
// attribute surface.
type bookmarkCollection struct {
	URLs        []string
	Titles      []string
	Folders     map[string]struct{}
	FolderCount int64
	URLCount    int64
	NodeCount   int
}

func (c *bookmarkCollection) addURL(url, title string) {
	c.URLCount++
	url = strings.TrimSpace(url)
	if url != "" && len(c.URLs) < bookmarkMaxURLs {
		c.URLs = append(c.URLs, url)
	}
	title = strings.TrimSpace(title)
	if title != "" && len(c.Titles) < bookmarkMaxTitles {
		c.Titles = append(c.Titles, title)
	}
}

func (c *bookmarkCollection) addFolder(name string) {
	c.FolderCount++
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if c.Folders == nil {
		c.Folders = make(map[string]struct{})
	}
	if len(c.Folders) >= bookmarkMaxFolders {
		return
	}
	c.Folders[name] = struct{}{}
}

// bookmarkBody is the shared body-extractor formatter used by both
// Chromium and Safari extractors. Emits one line per bookmark in
// `<title>\t<url>` shape (or `<title>` alone when URL is missing,
// `<url>` alone when title is missing) plus folder names on their
// own lines. The newline-separated text feeds straight into
// body.contains(...) / body.matches(...) — `body.contains("kubernetes")`
// matches when ANY bookmark's title or URL contains the substring.
//
// Builds from the already-parsed Attributes so the body extractor
// reuses the same walker output rather than re-walking the tree.
func bookmarkBody(attrs Attributes, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	urls, _ := attrs["bookmark_urls"].([]string)
	titles, _ := attrs["bookmark_titles"].([]string)
	folders, _ := attrs["bookmark_folders"].([]string)

	var sb strings.Builder
	// Bookmark lines: pair titles with URLs by index where possible.
	// addURL keeps the two slices loosely aligned (both push when the
	// respective field is non-empty), so the index alignment IS the
	// pairing — same logic as Chromium's bookmark_codec emits them.
	n := max(len(titles), len(urls))
	for i := range n {
		if sb.Len() >= maxBytes {
			break
		}
		var t, u string
		if i < len(titles) {
			t = titles[i]
		}
		if i < len(urls) {
			u = urls[i]
		}
		switch {
		case t != "" && u != "":
			sb.WriteString(t)
			sb.WriteByte('\t')
			sb.WriteString(u)
		case t != "":
			sb.WriteString(t)
		case u != "":
			sb.WriteString(u)
		default:
			continue
		}
		sb.WriteByte('\n')
	}
	for _, f := range folders {
		if sb.Len() >= maxBytes {
			break
		}
		sb.WriteString(f)
		sb.WriteByte('\n')
	}
	out := sb.String()
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}

func (c *bookmarkCollection) toAttributes() Attributes {
	out := Attributes{
		"bookmark_count":        c.URLCount,
		"bookmark_folder_count": c.FolderCount,
	}
	if len(c.URLs) > 0 {
		out["bookmark_urls"] = c.URLs
	}
	if len(c.Titles) > 0 {
		out["bookmark_titles"] = c.Titles
	}
	if len(c.Folders) > 0 {
		names := make([]string, 0, len(c.Folders))
		for n := range c.Folders {
			names = append(names, n)
		}
		sort.Strings(names)
		out["bookmark_folders"] = names
	}
	return out
}
