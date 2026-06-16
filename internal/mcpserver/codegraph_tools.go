package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// codeGraphWalkInput is the shared walk-scoping surface for the three
// cross-file code-graph tools (imported_by / find_definition /
// code_graph). Embedded into each tool's input struct so the JSON
// schema is identical across them.
type codeGraphWalkInput struct {
	Expr                string   `json:"expr,omitempty" jsonschema:"CEL pre-filter scoping which files enter the graph. Defaults to 'is_source' (every detected source file). Narrow it to cut the walk: 'is_source && language == \"go\"' for a Go-only graph. Same vocabulary as the search tool."`
	Dir                 string   `json:"dir,omitempty" jsonschema:"Directory to analyse. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs                []string `json:"dirs,omitempty" jsonschema:"Multiple directories analysed as one graph. When non-empty, takes precedence over 'dir'."`
	Workers             int      `json:"workers,omitempty" jsonschema:"Parallel walk workers. Defaults to runtime.NumCPU()."`
	Excludes            []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against basenames; matches are pruned. Same semantics as search."`
	RespectGitignore    bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks      bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, prune canonical build-artefact dirs (vendor / node_modules / target / __pycache__ / …). Unioned with 'excludes'."`
	TimeoutSeconds      *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Positive = seconds, 0 = no timeout. On expiry a partial graph over the files seen so far is returned with cancelled=true (not an error)."`
}

// codeGraphOptions expands + validates the walk inputs and builds the
// search.Options shared by all three handlers. Mirrors the boilerplate
// in findMatchesHandler.
func (h *handlers) codeGraphOptions(in codeGraphWalkInput) (search.Options, error) {
	expr := in.Expr
	if expr == "" {
		expr = "is_source"
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return search.Options{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return search.Options{}, fmt.Errorf("expand dirs: %w", err)
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return search.Options{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return search.Options{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return search.Options{}, err
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}
	return search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                expr,
		Workers:             in.Workers,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
	}, nil
}

// --- imported_by ---------------------------------------------------------

// ImportedByInput is the JSON-schema input for the `imported_by` tool.
type ImportedByInput struct {
	codeGraphWalkInput
	Module string `json:"module" jsonschema:"The import string to look up — exactly as it appears in source (e.g. 'github.com/spf13/cobra' for Go, 'react' for JS, 'numpy' for Python from-imports). Required."`
	Mode   string `json:"mode,omitempty" jsonschema:"Match mode: 'exact' (default — the import string equals module), 'prefix' (module is a leading substring of the import — useful for a package path that owns several sub-imports), or 'regex' (module is an RE2 pattern matched against each import string)."`
}

// ImportedByOutput lists every file that imports the queried module.
type ImportedByOutput struct {
	CommonOutput
	Module             string            `json:"module"`
	Mode               string            `json:"mode"`
	Importers          []search.Importer `json:"importers"`
	Count              int               `json:"count"`
	TotalFiles         int64             `json:"total_files"`
	Cancelled          bool              `json:"cancelled,omitempty"`
	CancellationReason string            `json:"cancellation_reason,omitempty"`
}

func (h *handlers) importedByHandler(ctx context.Context, _ *mcp.CallToolRequest, in ImportedByInput) (*mcp.CallToolResult, ImportedByOutput, error) {
	if in.Module == "" {
		return nil, ImportedByOutput{}, fmt.Errorf("module is required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, ImportedByOutput{}, err
	}
	mode := in.Mode
	if mode == "" {
		mode = "exact"
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, ImportedByOutput{}, fmt.Errorf("imported_by: %w", err)
	}
	importers, err := g.ImportedBy(in.Module, mode)
	if err != nil {
		return nil, ImportedByOutput{}, fmt.Errorf("imported_by: %w", err)
	}

	out := ImportedByOutput{
		Module:             in.Module,
		Mode:               mode,
		Importers:          importers,
		Count:              len(importers),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- find_definition -----------------------------------------------------

// FindDefinitionInput is the JSON-schema input for the `find_definition` tool.
type FindDefinitionInput struct {
	codeGraphWalkInput
	Symbol string `json:"symbol" jsonschema:"The exact function or type name to locate (e.g. 'ServeHTTP', 'BuildAttributesWith', 'OrderService'). Required. Matching is exact, not substring — use find_matches for fuzzy/textual search."`
	Kind   string `json:"kind,omitempty" jsonschema:"Filter to a symbol class: 'function' or 'type'. Empty returns both. (Functions covers methods; types covers class / interface / struct / trait / enum, per language.)"`
}

// Each definition carries an optional 'owner' (the type a method belongs
// to, e.g. 'Buffer' for (*Buffer).String) so same-named methods on
// different types are distinguishable — Go plus the class-based
// tree-sitter languages (#445).

// FindDefinitionOutput lists every file that defines the queried symbol.
type FindDefinitionOutput struct {
	CommonOutput
	Symbol             string             `json:"symbol"`
	Kind               string             `json:"kind,omitempty"`
	Definitions        []search.SymbolDef `json:"definitions"`
	Count              int                `json:"count"`
	TotalFiles         int64              `json:"total_files"`
	Cancelled          bool               `json:"cancelled,omitempty"`
	CancellationReason string             `json:"cancellation_reason,omitempty"`
}

func (h *handlers) findDefinitionHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindDefinitionInput) (*mcp.CallToolResult, FindDefinitionOutput, error) {
	if in.Symbol == "" {
		return nil, FindDefinitionOutput{}, fmt.Errorf("symbol is required")
	}
	if in.Kind != "" && in.Kind != "function" && in.Kind != "type" {
		return nil, FindDefinitionOutput{}, fmt.Errorf(`kind must be "function", "type", or empty`)
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, FindDefinitionOutput{}, err
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, FindDefinitionOutput{}, fmt.Errorf("find_definition: %w", err)
	}
	defs := g.FindDefinition(in.Symbol, in.Kind)

	out := FindDefinitionOutput{
		Symbol:             in.Symbol,
		Kind:               in.Kind,
		Definitions:        defs,
		Count:              len(defs),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- code_graph ----------------------------------------------------------

// CodeGraphInput is the JSON-schema input for the `code_graph` tool.
type CodeGraphInput struct {
	codeGraphWalkInput
	Top int `json:"top,omitempty" jsonschema:"Cap on each ranked list (import hubs, high-fan-out files, duplicate definitions). Defaults to 20."`
}

// CodeGraphOutput is the project-wide overview.
type CodeGraphOutput struct {
	CommonOutput
	Overview           search.CodeGraphOverview `json:"overview"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
}

func (h *handlers) codeGraphHandler(ctx context.Context, _ *mcp.CallToolRequest, in CodeGraphInput) (*mcp.CallToolResult, CodeGraphOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, CodeGraphOutput{}, err
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, CodeGraphOutput{}, fmt.Errorf("code_graph: %w", err)
	}

	out := CodeGraphOutput{
		Overview:           g.Overview(in.Top),
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- who_calls ----------------------------------------------------------

// WhoCallsInput is the JSON-schema input for the `who_calls` tool.
type WhoCallsInput struct {
	codeGraphWalkInput
	Symbol string `json:"symbol" jsonschema:"The exact function / method name to find callers of (e.g. 'ServeHTTP', 'process'). Required. Name-based: a call pkg.Foo() or x.Method() is keyed by 'Foo' / 'Method'."`
}

// WhoCallsOutput lists every file that calls/references the symbol.
type WhoCallsOutput struct {
	CommonOutput
	Symbol string `json:"symbol"`
	// DefinedOn lists the types the queried symbol is a method on (#445),
	// when any — a hint that these name-based caller results may mix calls
	// to same-named methods on different types. Go plus the class-based tree-sitter languages (#445).
	DefinedOn          []string          `json:"defined_on,omitempty"`
	Callers            []search.Importer `json:"callers"`
	Count              int               `json:"count"`
	TotalFiles         int64             `json:"total_files"`
	Cancelled          bool              `json:"cancelled,omitempty"`
	CancellationReason string            `json:"cancellation_reason,omitempty"`
}

func (h *handlers) whoCallsHandler(ctx context.Context, _ *mcp.CallToolRequest, in WhoCallsInput) (*mcp.CallToolResult, WhoCallsOutput, error) {
	if in.Symbol == "" {
		return nil, WhoCallsOutput{}, fmt.Errorf("symbol is required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, WhoCallsOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, WhoCallsOutput{}, fmt.Errorf("who_calls: %w", err)
	}
	callers := g.WhoCalls(in.Symbol)
	out := WhoCallsOutput{
		Symbol:             in.Symbol,
		DefinedOn:          g.OwnersOf(in.Symbol),
		Callers:            callers,
		Count:              len(callers),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- dead_code ----------------------------------------------------------

// DeadCodeInput is the JSON-schema input for the `dead_code` tool.
type DeadCodeInput struct {
	codeGraphWalkInput
}

// DeadCodeOutput lists candidate unreferenced definitions.
type DeadCodeOutput struct {
	CommonOutput
	Candidates         []search.SymbolDef `json:"candidates"`
	Count              int                `json:"count"`
	Hint               string             `json:"hint,omitempty"`
	TotalFiles         int64              `json:"total_files"`
	Cancelled          bool               `json:"cancelled,omitempty"`
	CancellationReason string             `json:"cancellation_reason,omitempty"`
}

func (h *handlers) deadCodeHandler(ctx context.Context, _ *mcp.CallToolRequest, in DeadCodeInput) (*mcp.CallToolResult, DeadCodeOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, DeadCodeOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, DeadCodeOutput{}, fmt.Errorf("dead_code: %w", err)
	}
	candidates := g.DeadCode()
	dcPaths := make([]string, len(candidates))
	for i, c := range candidates {
		dcPaths[i] = c.Path
	}
	out := DeadCodeOutput{
		Candidates:         candidates,
		Count:              len(candidates),
		Hint:               g.GeneratedHint(dcPaths),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- impact -------------------------------------------------------------

// ImpactInput is the JSON-schema input for the `impact` tool.
type ImpactInput struct {
	codeGraphWalkInput
	Symbol   string `json:"symbol" jsonschema:"The exact function/method name to assess. Returns every function that transitively calls it — the blast radius of changing it. Required."`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"Cap the number of call hops in the closure. 0 (default) is unbounded. 1 = direct callers only (same set as who_calls, but symbol-level)."`
}

// ImpactOutput is the transitive caller closure of the queried symbol.
type ImpactOutput struct {
	CommonOutput
	Symbol             string              `json:"symbol"`
	Dependents         []search.ImpactNode `json:"dependents"`
	Count              int                 `json:"count"`
	MaxDepthReached    int                 `json:"max_depth_reached"`
	TotalFiles         int64               `json:"total_files"`
	Cancelled          bool                `json:"cancelled,omitempty"`
	CancellationReason string              `json:"cancellation_reason,omitempty"`
}

func (h *handlers) impactHandler(ctx context.Context, _ *mcp.CallToolRequest, in ImpactInput) (*mcp.CallToolResult, ImpactOutput, error) {
	if in.Symbol == "" {
		return nil, ImpactOutput{}, fmt.Errorf("symbol is required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, ImpactOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, ImpactOutput{}, fmt.Errorf("impact: %w", err)
	}
	deps := g.Impact(in.Symbol, in.MaxDepth)
	maxDepth := 0
	for _, d := range deps {
		if d.Depth > maxDepth {
			maxDepth = d.Depth
		}
	}
	out := ImpactOutput{
		Symbol:             in.Symbol,
		Dependents:         deps,
		Count:              len(deps),
		MaxDepthReached:    maxDepth,
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- call_path ----------------------------------------------------------

// CallPathInput is the JSON-schema input for the `call_path` tool.
type CallPathInput struct {
	codeGraphWalkInput
	From     string `json:"from" jsonschema:"The exact name of the calling function to start from. Required."`
	To       string `json:"to" jsonschema:"The exact name of the target function to reach. Required."`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"Cap the call hops searched. 0 (default) is unbounded."`
}

// CallPathOutput is the shortest call path from `from` to `to`.
type CallPathOutput struct {
	CommonOutput
	From               string                `json:"from"`
	To                 string                `json:"to"`
	Reachable          bool                  `json:"reachable"`
	Length             int                   `json:"length"`
	Path               []search.CallPathStep `json:"path"`
	TotalFiles         int64                 `json:"total_files"`
	Cancelled          bool                  `json:"cancelled,omitempty"`
	CancellationReason string                `json:"cancellation_reason,omitempty"`
}

func (h *handlers) callPathHandler(ctx context.Context, _ *mcp.CallToolRequest, in CallPathInput) (*mcp.CallToolResult, CallPathOutput, error) {
	if in.From == "" || in.To == "" {
		return nil, CallPathOutput{}, fmt.Errorf("both from and to are required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, CallPathOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, CallPathOutput{}, fmt.Errorf("call_path: %w", err)
	}
	path := g.CallPath(in.From, in.To, in.MaxDepth)
	out := CallPathOutput{
		From:               in.From,
		To:                 in.To,
		Reachable:          len(path) > 0,
		Path:               path,
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	if len(path) > 0 {
		out.Length = len(path) - 1 // hops, not nodes
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- test_gaps ----------------------------------------------------------

// TestGapsInput is the JSON-schema input for the `test_gaps` tool.
type TestGapsInput struct {
	codeGraphWalkInput
}

// TestGapsOutput lists production files with functions no test references.
type TestGapsOutput struct {
	CommonOutput
	Gaps               []search.TestGap `json:"gaps"`
	Count              int              `json:"count"`
	Hint               string           `json:"hint,omitempty"`
	TotalFiles         int64            `json:"total_files"`
	Cancelled          bool             `json:"cancelled,omitempty"`
	CancellationReason string           `json:"cancellation_reason,omitempty"`
}

func (h *handlers) testGapsHandler(ctx context.Context, _ *mcp.CallToolRequest, in TestGapsInput) (*mcp.CallToolResult, TestGapsOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, TestGapsOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, TestGapsOutput{}, fmt.Errorf("test_gaps: %w", err)
	}
	gaps := g.TestGaps()
	tgPaths := make([]string, len(gaps))
	for i, gp := range gaps {
		tgPaths[i] = gp.Path
	}
	out := TestGapsOutput{
		Gaps:               gaps,
		Count:              len(gaps),
		Hint:               g.GeneratedHint(tgPaths),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}

// --- calls --------------------------------------------------------------

// CallsInput is the JSON-schema input for the `calls` tool.
type CallsInput struct {
	codeGraphWalkInput
	Symbol string `json:"symbol" jsonschema:"The exact function/method name whose callees you want — 'what does Y call?'. Required."`
}

// CallsOutput lists the distinct callees of the queried function.
type CallsOutput struct {
	CommonOutput
	Symbol             string   `json:"symbol"`
	Callees            []string `json:"callees"`
	Count              int      `json:"count"`
	TotalFiles         int64    `json:"total_files"`
	Cancelled          bool     `json:"cancelled,omitempty"`
	CancellationReason string   `json:"cancellation_reason,omitempty"`
}

func (h *handlers) callsHandler(ctx context.Context, _ *mcp.CallToolRequest, in CallsInput) (*mcp.CallToolResult, CallsOutput, error) {
	if in.Symbol == "" {
		return nil, CallsOutput{}, fmt.Errorf("symbol is required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, CallsOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, CallsOutput{}, fmt.Errorf("calls: %w", err)
	}
	callees := g.Calls(in.Symbol)
	out := CallsOutput{
		Symbol:             in.Symbol,
		Callees:            callees,
		Count:              len(callees),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
