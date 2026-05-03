package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/alecthomas/kong"
	"github.com/richardwooding/file-search-on/internal/celexpr"
	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/mcpserver"
	"github.com/richardwooding/file-search-on/internal/search"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var CLI struct {
	Search  SearchCmd        `cmd:"" help:"Search for files matching a CEL expression." default:"withargs"`
	MCP     MCPCmd           `cmd:"" name:"mcp" help:"Run as a Model Context Protocol server over stdio."`
	Version kong.VersionFlag `short:"V" help:"Print version and exit."`
}

type MCPCmd struct{}

func (m *MCPCmd) Run() error {
	return mcpserver.Run(context.Background(), version)
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
	schema := celexpr.Schema()

	fmt.Println("Supported CEL attributes:")
	printAttrs(schema.Common, 12, 9)
	fmt.Println()
	fmt.Println("Type-specific attributes:")
	printAttrs(schema.TypeSpecific, 18, 11)
	fmt.Println()
	fmt.Println("Markdown front-matter attributes (YAML ---, TOML +++, JSON {}):")
	printAttrs(schema.Frontmatter, 18, 11)
	fmt.Println()
	fmt.Println("Registered content types:")
	for _, ct := range contentpkg.DefaultRegistry().Types() {
		fmt.Printf("  %-20s %v\n", ct.Name(), ct.Extensions())
	}
}

func printAttrs(attrs []celexpr.AttributeDoc, nameWidth, typeWidth int) {
	for _, a := range attrs {
		typeField := "(" + a.Type + ")"
		fmt.Printf("  %-*s %-*s - %s\n", nameWidth, a.Name, typeWidth, typeField, a.Description)
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
