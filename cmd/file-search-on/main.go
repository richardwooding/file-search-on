package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/alecthomas/kong"
	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var CLI struct {
	Search  SearchCmd        `cmd:"" help:"Search for files matching a CEL expression." default:"withargs"`
	Version kong.VersionFlag `short:"V" help:"Print version and exit."`
}

type SearchCmd struct {
	Expr    string `arg:"" help:"CEL expression to match files (e.g. 'is_json && size > 1024')." optional:""`
	Dir     string `short:"d" help:"Directory to search in." default:"."`
	Workers int    `short:"w" help:"Number of parallel workers." default:"0"`
	List    bool   `short:"l" help:"List supported attributes and content types."`
}

func (s *SearchCmd) Run() error {
	if s.List {
		printHelp()
		return nil
	}

	if s.Expr == "" {
		s.Expr = "true"
	}

	ctx := context.Background()
	opts := search.Options{
		Root:    s.Dir,
		Expr:    s.Expr,
		Workers: s.Workers,
	}

	results, err := search.Walk(ctx, opts, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	for _, r := range results {
		ct := r.ContentType
		if ct == "" {
			ct = "unknown"
		}
		fmt.Printf("%s\t[%s]\t%d bytes\n", r.Path, ct, r.Size)
	}

	fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", len(results))
	return nil
}

func printHelp() {
	fmt.Println("Supported CEL attributes:")
	fmt.Println("  name         (string)  - filename")
	fmt.Println("  path         (string)  - full path")
	fmt.Println("  dir          (string)  - parent directory")
	fmt.Println("  size         (int)     - file size in bytes")
	fmt.Println("  ext          (string)  - file extension (e.g. '.md')")
	fmt.Println("  content_type (string)  - detected content type")
	fmt.Println("  is_markdown  (bool)    - true if markdown file")
	fmt.Println("  is_json      (bool)    - true if JSON file")
	fmt.Println("  is_xml       (bool)    - true if XML file")
	fmt.Println("  is_html      (bool)    - true if HTML file")
	fmt.Println("  is_pdf       (bool)    - true if PDF file")
	fmt.Println("  is_image     (bool)    - true if image file")
	fmt.Println()
	fmt.Println("Type-specific attributes:")
	fmt.Println("  title              (string)    - title (front-matter, markdown h1, HTML title, PDF title)")
	fmt.Println("  word_count         (int)       - word count (markdown body, excluding front-matter)")
	fmt.Println("  page_count         (int)       - page count (PDF)")
	fmt.Println("  author             (string)    - author (markdown front-matter, PDF)")
	fmt.Println("  root_element       (string)    - root element name (XML)")
	fmt.Println("  json_kind          (string)    - 'object' or 'array' (JSON)")
	fmt.Println("  img_width          (int)       - image width in pixels")
	fmt.Println("  img_height         (int)       - image height in pixels")
	fmt.Println()
	fmt.Println("Markdown front-matter attributes (YAML ---, TOML +++, JSON {}):")
	fmt.Println("  frontmatter        (map)       - full parsed front-matter, e.g. frontmatter.category")
	fmt.Println("  frontmatter_format (string)    - 'yaml', 'toml', 'json', or '' if none")
	fmt.Println("  tags               (list<str>) - front-matter tags (single string is wrapped)")
	fmt.Println("  categories         (list<str>) - front-matter categories")
	fmt.Println("  draft              (bool)      - front-matter draft flag")
	fmt.Println("  date               (timestamp) - front-matter date")
	fmt.Println()
	fmt.Println("Registered content types:")
	for _, ct := range contentpkg.DefaultRegistry().Types() {
		fmt.Printf("  %-20s %v\n", ct.Name(), ct.Extensions())
	}
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("file-search-on"),
		kong.Description("Content-type aware file search with CEL attribute filtering."),
		kong.UsageOnError(),
		kong.Vars{"version": fmt.Sprintf("file-search-on %s (commit %s, built %s)", version, commit, date)},
	)
	if err := ctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
