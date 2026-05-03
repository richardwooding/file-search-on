package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/template"

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
	Expr         string `arg:"" help:"CEL expression to match files (e.g. 'is_json && size > 1024')." optional:""`
	Dir          string `short:"d" help:"Directory to search in." default:"."`
	Workers      int    `short:"w" help:"Number of parallel workers." default:"0"`
	List         bool   `short:"l" help:"List supported attributes and content types."`
	MaxLineBytes int    `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default." default:"0"`
	Output       string `short:"o" name:"output" enum:"bare,default,verbose,json" default:"default" help:"Output format: bare | default | verbose | json."`
	Format       string `name:"format" help:"Custom Go text/template applied per match (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`
}

func (s *SearchCmd) Run() error {
	if s.List {
		printHelp()
		return nil
	}

	if s.Expr == "" {
		s.Expr = "true"
	}

	// --format implies attribute access; same for verbose/json presets.
	includeAttrs := s.Format != "" || s.Output == "verbose" || s.Output == "json"

	// Parse the template up front so a bad template fails before we walk.
	var tmpl *template.Template
	if s.Format != "" {
		var err error
		tmpl, err = parseFormatTemplate(s.Format)
		if err != nil {
			return fmt.Errorf("parse --format template: %w", err)
		}
	}

	ctx := context.Background()
	opts := search.Options{
		Root:              s.Dir,
		Expr:              s.Expr,
		Workers:           s.Workers,
		MaxLineBytes:      s.MaxLineBytes,
		IncludeAttributes: includeAttrs,
	}

	results, err := search.Walk(ctx, opts, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	switch {
	case tmpl != nil:
		if err := printTemplate(os.Stdout, results, tmpl); err != nil {
			return err
		}
	case s.Output == "bare":
		printBare(os.Stdout, results)
	case s.Output == "verbose":
		printVerbose(os.Stdout, results)
		fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", len(results))
	case s.Output == "json":
		if err := printJSON(os.Stdout, results); err != nil {
			return err
		}
	default: // "" or "default"
		printDefault(os.Stdout, results)
		fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", len(results))
	}
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
