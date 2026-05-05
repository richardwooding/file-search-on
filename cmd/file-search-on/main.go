package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
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
	Attrs   AttrsCmd         `cmd:"" name:"attrs" help:"Print attributes for a single file (no walk, no CEL)."`
	MCP     MCPCmd           `cmd:"" name:"mcp" help:"Run as a Model Context Protocol server (stdio, http, or sse)."`
	Version kong.VersionFlag `short:"V" help:"Print version and exit."`
}

type AttrsCmd struct {
	Path   string `arg:"" help:"File to inspect."`
	Output string `short:"o" name:"output" enum:"default,verbose,json" default:"verbose" help:"Output format: default | verbose | json."`
	Format string `name:"format" help:"Custom Go text/template applied to the record (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`
}

func (a *AttrsCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(a.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; use the search subcommand to walk a tree", abs)
	}

	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	attrs, err := celexpr.BuildAttributes(ctx, os.DirFS(dir), base, abs, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("read attributes: %w", err)
	}

	result := search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	}
	results := []search.Result{result}

	if a.Format != "" {
		tmpl, err := parseFormatTemplate(a.Format)
		if err != nil {
			return fmt.Errorf("parse --format template: %w", err)
		}
		return printTemplate(os.Stdout, results, tmpl)
	}
	switch a.Output {
	case "json":
		return printJSON(os.Stdout, results)
	case "default":
		printDefault(os.Stdout, results)
	default: // "" or "verbose"
		printVerbose(os.Stdout, results)
	}
	return nil
}

type MCPCmd struct {
	Transport string `name:"transport" enum:"stdio,http,sse" default:"stdio" help:"Transport: stdio (default; for desktop clients), http (Streamable HTTP, MCP 2025-03-26), or sse (DEPRECATED — HTTP+SSE, MCP 2024-11-05)."`
	Addr      string `name:"addr" default:":8080" help:"host:port to bind for http or sse transports. Ignored for stdio."`
	Path      string `name:"path" default:"/" help:"URL path prefix the handler is mounted at. Ignored for stdio."`
}

func (m *MCPCmd) Run(ctx context.Context) error {
	switch m.Transport {
	case "http":
		return mcpserver.RunHTTP(ctx, version, m.Addr, m.Path)
	case "sse":
		fmt.Fprintln(os.Stderr, "warning: --transport sse is DEPRECATED (MCP 2024-11-05); prefer --transport http for new clients.")
		return mcpserver.RunSSE(ctx, version, m.Addr, m.Path)
	default:
		return mcpserver.Run(ctx, version)
	}
}

type SearchCmd struct {
	Expr         string `arg:"" help:"CEL expression to match files (e.g. 'is_json && size > 1024')." optional:""`
	Dir          string `short:"d" help:"Directory to search in." default:"."`
	Workers      int    `short:"w" help:"Number of parallel workers." default:"0"`
	List         bool   `short:"l" help:"List supported attributes and content types."`
	MaxLineBytes int    `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default." default:"0"`
	Output       string `short:"o" name:"output" enum:"bare,default,verbose,json" default:"default" help:"Output format: bare | default | verbose | json."`
	Format       string `name:"format" help:"Custom Go text/template applied per match (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`
	Unsorted     bool   `name:"unsorted" help:"Stream matches in walk order instead of buffering+sorting. Default and verbose modes still emit the count footer; bare/json/template are streamed and unsorted regardless."`
}

func (s *SearchCmd) Run(ctx context.Context) error {
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

	opts := search.Options{
		Root:              s.Dir,
		Expr:              s.Expr,
		Workers:           s.Workers,
		MaxLineBytes:      s.MaxLineBytes,
		IncludeAttributes: includeAttrs,
	}

	// Streaming-friendly modes (bare / json / template) always stream —
	// first result lands on stdout immediately rather than waiting for
	// the full walk. Default and verbose stream too when --unsorted is
	// set; otherwise they buffer for sort+footer (the historical UX).
	streaming := tmpl != nil || s.Output == "bare" || s.Output == "json" || s.Unsorted
	if streaming {
		return streamSearch(ctx, opts, tmpl, s.Output)
	}
	return bufferedSearch(ctx, opts, s.Output)
}

// streamSearch drives WalkStream and prints each match as it arrives.
// For default and verbose modes, counts records as they flow through
// and emits the "N file(s) found" footer to stderr after the stream
// closes — preserves the count UX even in streaming mode.
func streamSearch(ctx context.Context, opts search.Options, tmpl *template.Template, mode string) error {
	out := make(chan search.Result, 64)
	var walkErr error
	done := make(chan struct{})
	go func() {
		walkErr = search.WalkStream(ctx, opts, contentpkg.DefaultRegistry(), out)
		close(done)
	}()

	var printErr error
	var count int64
	switch {
	case tmpl != nil:
		printErr = printTemplateStream(os.Stdout, out, tmpl)
	case mode == "json":
		printErr = printJSONStream(os.Stdout, out)
	case mode == "bare":
		printBareStream(os.Stdout, out)
	case mode == "verbose":
		count = printVerboseStream(os.Stdout, out)
	default: // "default"
		count = printDefaultStream(os.Stdout, out)
	}
	<-done

	if walkErr != nil {
		return fmt.Errorf("search failed: %w", walkErr)
	}
	if mode == "default" || mode == "verbose" {
		fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", count)
	}
	return printErr
}

// bufferedSearch keeps the historical Walk + sort + print + footer flow
// for default and verbose modes, both of which emit a "N file(s) found"
// footer that requires the full result set.
func bufferedSearch(ctx context.Context, opts search.Options, mode string) error {
	results, err := search.Walk(ctx, opts, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	if mode == "verbose" {
		printVerbose(os.Stdout, results)
	} else { // "" or "default"
		printDefault(os.Stdout, results)
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
	fmt.Println("Built-in functions:")
	printFuncs(schema.Functions)
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

func printFuncs(funcs []celexpr.FunctionDoc) {
	for _, f := range funcs {
		fmt.Printf("  %s\n      %s\n", f.Signature, f.Description)
		if f.Example != "" {
			fmt.Printf("      e.g. %s\n", f.Example)
		}
	}
}

func main() {
	// Bridge OS signals into a cancellable ctx so subcommands shut down
	// cleanly: HTTP server gets graceful Shutdown, walker workers exit,
	// etc. Stop the relay on return so a second Ctrl-C falls through to
	// the default runtime handler and abruptly kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kctx := kong.Parse(&CLI,
		kong.Name("file-search-on"),
		kong.Description("Content-type aware file search with CEL attribute filtering."),
		kong.UsageOnError(),
		kong.Vars{"version": fmt.Sprintf("file-search-on %s (commit %s, built %s)", version, commit, date)},
		kong.BindTo(ctx, (*context.Context)(nil)),
	)
	if err := kctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
