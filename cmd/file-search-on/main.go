package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/projectdetect"
	// Enables `cel:` indicators in custom project-type YAML (registers the
	// CEL compiler via init). The projectdetect base package has no cel-go
	// dependency; this blank import opts the binary into it.
	_ "github.com/richardwooding/projectdetect/celindicators"
)

// exitCodeError lets a subcommand request a specific process exit code.
// main() type-switches on it via errors.As; the wrapped msg is used only
// if a code is paired with a non-empty diagnostic, which it usually
// isn't (subcommands typically print their own stderr explanation).
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.msg
}

// isCancellation reports whether err is one of the context-cancellation
// signals (deadline-exceeded / canceled). Used to fork the post-walk
// path between "real error" and "partial results due to ctx".
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// CLI is the kong root. Subcommand structs are defined in their own
// *_cmd.go siblings; only the root grouping + global flags live here.
var CLI struct {
	ProjectTypeConfig string `name:"project-type-config" help:"Path to a YAML config registering custom project types (CEL-driven or file-based indicators). Loaded LAST, after any auto-discovered configs. Loaded before any subcommand runs; the new types appear alongside built-ins in detect-project / find-projects / search results."`
	NoConfigSearch    bool   `name:"no-config-search" help:"Skip automatic discovery of project-type configs at the standard search paths (user-wide UserConfigDir()/file-search-on/project-types.yaml and per-project ./.file-search-on/project-types.yaml). Use for hermetic invocations (tests, CI) where only the explicit --project-type-config should apply."`

	Search             SearchCmd             `cmd:"" help:"Search for files matching a CEL expression." default:"withargs"`
	Preset             PresetCmd             `cmd:"" name:"preset" help:"Run a pre-canned search recipe by name (recent_changes, recent_photos, old_drafts, large_files, large_binaries, suspicious_files, failed_tests, system_metadata). Without args, lists every preset. Each preset bakes a vetted CEL filter + sensible sort / limit defaults; CLI flags override per-call."`
	Attrs              AttrsCmd              `cmd:"" name:"attrs" help:"Print attributes for a single file (no walk, no CEL)."`
	Stats              StatsCmd              `cmd:"" name:"stats" help:"Aggregate content-type counts and total sizes for a directory tree."`
	Lines              LinesCmd              `cmd:"" name:"lines" help:"Print a range of lines from a single file (no walk, no CEL)."`
	Duplicates         DuplicatesCmd         `cmd:"" name:"duplicates" help:"Find groups of byte-identical files by sha256 hash."`
	NearDuplicates     NearDuplicatesCmd     `cmd:"" name:"near-duplicates" help:"Find groups of similar (not identical) files by SimHash fingerprint of their extracted body. Complements 'duplicates' for fuzzy matching — catches files with trailing-newline edits, regenerated headers, typo fixes, template copies."`
	DuplicateFunctions DuplicateFunctionsCmd `cmd:"" name:"duplicate-functions" help:"Find clusters of near-identical FUNCTIONS across the tree (copy-pasted logic the file-level near-duplicates scan misses). SimHashes each function body; reports extract-this-helper candidates with their line ranges."`
	ArchiveContents    ArchiveContentsCmd    `cmd:"" name:"archive-contents" help:"List or filter entries inside a ZIP / TAR / TAR.GZ / GZIP archive. Per-entry CEL evaluation against the SAME vocabulary the top-level search uses — every is_X predicate and per-family attribute applies inside archives."`
	ArchiveRead        ArchiveReadCmd        `cmd:"" name:"archive-read" help:"Read a single file's content out of a ZIP / TAR / TAR.GZ / GZIP archive without extracting. Returns the bytes plus detected content_type + attributes."`
	FindMatches        FindMatchesCmd        `cmd:"" name:"find-matches" help:"Scan text files for an RE2 regex; report line-level hits with optional context windows (combines CEL type-pruning with grep-style output)."`
	ImportedBy         ImportedByCmd         `cmd:"" name:"imported-by" help:"Cross-file reverse-dependency lookup: list every source file that imports a given module. e.g. imported-by github.com/spf13/cobra -d ."`
	FindDefinition     FindDefinitionCmd     `cmd:"" name:"find-definition" help:"Locate where a function or type is defined across a tree (symbol-aware; complements find-matches). e.g. find-definition ServeHTTP --kind function."`
	CodeGraph          CodeGraphCmd          `cmd:"" name:"code-graph" help:"Project-wide code-structure overview: import hubs (most-depended-on modules), high-fan-out files, duplicate definitions, language breakdown. Built from the cross-file import + definition graph."`
	WhoCalls           WhoCallsCmd           `cmd:"" name:"who-calls" help:"List every source file that calls/references a given function or method name (the call-site leg of the code graph). e.g. who-calls ServeHTTP."`
	DeadCode           DeadCodeCmd           `cmd:"" name:"dead-code" help:"List candidate unreferenced definitions (functions/types nothing calls). Heuristic — exported API / entry points / dynamic dispatch may be false positives; for review, not a delete list."`
	TestGaps           TestGapsCmd           `cmd:"" name:"test-gaps" help:"List production source files whose functions are never referenced from a test — candidate untested code. Heuristic (direct test references only); no coverage instrumentation required."`
	CoverageGaps       CoverageGapsCmd       `cmd:"" name:"coverage-gaps" help:"Report functions below a coverage threshold from a Go coverage profile (go test -coverprofile). The precise complement to test-gaps — catches partially-tested functions. e.g. coverage-gaps cover.out --threshold 0.8."`
	Calls              CallsCmd              `cmd:"" name:"calls" help:"List the distinct functions a given function calls (forward call lookup; complements who-calls). e.g. calls BuildCodeGraph."`
	Impact             ImpactCmd             `cmd:"" name:"impact" help:"Transitive reverse-dependency closure for a function — every function that (in)directly calls it, the blast radius of changing it. Extends who-calls (one hop) to the full closure with depth. e.g. impact BuildCodeGraph."`
	CallPath           CallPathCmd           `cmd:"" name:"call-path" help:"Show the shortest call path from one function to another — HOW A reaches B through the call graph (the route, where impact gives the closure). e.g. call-path Run BuildCodeGraph."`
	References          ReferencesCmd         `cmd:"" name:"references" help:"Find every usage of a symbol with file + line — calls, type usages, and Go value-passing — the IDE 'find references' primitive (complement to find-definition). e.g. references BuildCodeGraph --kind type."`
	Complexity         ComplexityCmd         `cmd:"" name:"complexity" help:"List functions ranked by cyclomatic complexity (worst-first) — find maintenance hotspots. Go + tree-sitter languages. e.g. complexity 'is_source && language==\"go\"' --top 20."`
	Diff               DiffCmd               `cmd:"" name:"diff" help:"Cross-tree set operations by sha256 content hash — what's in tree A but not B, the intersection, drift between same-named files, etc. Read-only discovery; never mutates either tree. e.g. diff ~/Pictures /Volumes/Backup/Pictures --op a-minus-b."`
	APIDiff            APIDiffCmd            `cmd:"" name:"api-diff" help:"Detect breaking changes to the exported public API surface between two trees — removed exported functions/types (breaking) vs added. A release gate. e.g. api-diff ./v1 ./v2 --expr 'is_source && language==\"go\"'."`
	ChurnOwners        ChurnOwnersCmd        `cmd:"" name:"churn-owners" help:"Ownership / bus-factor per directory — which subtrees are effectively single-maintainer, ranked by risk. Aggregates git authorship over tracked files. e.g. churn-owners --expr is_source -d ."`
	Coupling           CouplingCmd           `cmd:"" name:"coupling" help:"Afferent/efferent coupling + instability (Martin metrics) — the fragile-hub seams (high Ca + high I) a refactor is riskiest near. Granularity is picked by the manifest at the root: Go (go.mod) → packages, Rust (Cargo.toml) → crates, JVM — Java / Kotlin / Scala (Maven / Gradle / sbt) → packages, C# (.sln / .csproj) → namespaces, Python (pyproject.toml / setup.py) → packages, JS/TS (package.json / tsconfig.json) → directory modules, PHP (composer.json) → namespaces. More languages tracked in #467. e.g. coupling -d . --top 20."`
	UnusedExports      UnusedExportsCmd      `cmd:"" name:"unused-exports" help:"Exported symbols referenced only within their own package — candidates to unexport and shrink the public API surface. The subtler complement to dead-code. Go / Python / Rust / TS / JS / Java / C# / Kotlin / Scala. e.g. unused-exports -d ."`
	Circular           CircularCmd           `cmd:"" name:"circular" help:"Detect circular dependencies — strongly-connected components in the first-party import graph (Tarjan SCC). Same manifest-based language selection as coupling, so it's multi-language (Go / Rust / JVM / C# / Python / JS-TS / PHP). e.g. circular -d ."`
	Validate           ValidateCmd           `cmd:"" name:"validate" help:"Compile-check a CEL expression without walking the filesystem — fast pre-flight or CI gate. Reports referenced variables/functions and a 'did you mean' hint on typos. Exits non-zero when invalid. e.g. validate 'is_pdf && page_count > 10'."`
	IndexStats         IndexStatsCmd         `cmd:"" name:"index-stats" help:"Inspect the on-disk attribute/body/embedding cache without running a search — entry counts, bytes used, and cache hit/miss counters. Counterpart to the search footer and the MCP index_stats tool."`
	Organize           OrganizeCmd           `cmd:"" name:"organize" help:"Build a templated symlink (or copy) tree from search results — a virtual organized view of a flat directory without moving the originals. e.g. organize 'is_raw_photo' --link-into '~/sorted/{raw_vendor}/{mtime_year}/{basename}'."`
	Watch              WatchCmd              `cmd:"" name:"watch" help:"Continuously watch directories and emit each new / changed file that matches a CEL expression (the inverse of search — 'tell me when X appears'). Runs until Ctrl-C. e.g. watch 'is_image && body.contains(\"error\")' --ocr -d ~/Desktop."`
	Detect             DetectProjectCmd      `cmd:"" name:"detect-project" help:"Identify project type(s) (go / node / rust / …) for a directory by checking canonical indicator files."`
	Projects           FindProjectsCmd       `cmd:"" name:"find-projects" help:"Walk a root and list every project subdirectory under it."`
	WhichProject       WhichProjectCmd       `cmd:"" name:"which-project" help:"Given a file or directory path, walk up the chain and identify the nearest enclosing project root and type(s)."`
	ConfigPaths        ConfigPathsCmd        `cmd:"" name:"config-paths" help:"Print the project-type config search paths for this platform. Use to discover where to drop your user-wide config (mkdir -p \"$(file-search-on config-paths -o bare | head -1 | xargs dirname)\")."`
	Monitors           MonitorsCmd           `cmd:"" name:"monitors" help:"List the monitoring-dashboard URLs of every currently-running file-search-on instance (mcp / watch started with --monitor). Reads the shared peer registry and prunes dead entries. Pipe -o bare into a browser opener, e.g. file-search-on monitors -o bare | head -1 | xargs open."`
	HashSet            HashSetCmd            `cmd:"" name:"hash-set" help:"Manage hash allowlist / denylist files used by --hash-allowlist / --hash-denylist. Subcommands: build (compile text or NSRL CSV into bbolt format), info (print per-algorithm counts)."`
	Embed              EmbedCmd              `cmd:"" name:"embed" help:"Manage Ollama embedding models for the search_semantic tool. Subcommands: list (what's installed + what's recommended), pull (download a model from Ollama)."`
	Playground         PlaygroundCmd         `cmd:"" name:"playground" help:"Interactive CEL-filtering TUI — type a CEL expression and watch a snapshot of a directory's files filter live as you type, over the same attribute vocabulary as search. Prints the final expression on exit so it's reusable. e.g. playground -d ./internal 'is_source'."`
	MCP                MCPCmd                `cmd:"" name:"mcp" help:"Run as a Model Context Protocol server (stdio, http, or sse)."`
	Version            kong.VersionFlag      `short:"V" help:"Print version and exit."`
}

// openIndex returns an index.Index honouring the new
// default-on-disk behaviour. With noIndex=false and path=="", the
// per-cwd default file is used (see defaultIndexPath). With
// noIndex=true OR a lock-timeout from another running instance, the
// caller transparently falls back to in-memory.
//
// On schema mismatch it surfaces a helpful "delete or re-point" error.
//
// bodyCap controls the body-cache total-size cap and opt-out for the
// bodies_v1 bucket. Zero-value uses defaults (256 MiB cap, body cache
// enabled). Subcommands that don't expose body-cache flags pass the
// zero value.
//
// The IndexBackend return carries diagnostic info (which backend was
// chosen and why) — used by mcp_cmd / watch_cmd to feed the dashboard
// + monitor_info MCP tool. CLI subcommands without a dashboard can
// discard it.
func openIndex(path string, noIndex bool, bodyCap index.BodyCacheCap) (index.Index, IndexBackend, error) {
	return resolveIndexBackend("", path, noIndex, bodyCap)
}

func main() {
	// Bridge OS signals into a cancellable ctx so subcommands shut down
	// cleanly: HTTP server gets graceful Shutdown, walker workers exit,
	// etc. Stop the relay on return so a second Ctrl-C falls through to
	// the default runtime handler and abruptly kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Stamp this binary's version as the on-disk index cache identity so a
	// cache written by a different version is discarded rather than served
	// stale — guards the #418 class of bug (attributes added between
	// releases silently missing for unchanged files).
	index.SetSchemaID(version)

	kctx := kong.Parse(&CLI,
		kong.Name("file-search-on"),
		kong.Description("Content-type aware file search with CEL attribute filtering."),
		kong.UsageOnError(),
		kong.Vars{"version": fmt.Sprintf("file-search-on %s (commit %s, built %s)", version, commit, date)},
		kong.BindTo(ctx, (*context.Context)(nil)),
	)
	// Load custom project types before the subcommand runs so they
	// appear in every project-aware surface (detect-project,
	// find-projects, --resolve-projects search, MCP tools when the
	// mcp subcommand wires the same path). Precedence (later layers
	// register on top of earlier):
	//   1. Auto-discovered configs from standard paths (gated by
	//      --no-config-search; default on).
	//   2. Explicit --project-type-config path.
	if !CLI.NoConfigSearch {
		if _, err := projectdetect.LoadDiscovered(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
	if CLI.ProjectTypeConfig != "" {
		if _, err := projectdetect.LoadFromFile(CLI.ProjectTypeConfig); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
	if err := kctx.Run(); err != nil {
		var ece *exitCodeError
		if errors.As(err, &ece) {
			// The subcommand has already printed its own diagnostic
			// to stderr; surface only the exit code.
			os.Exit(ece.code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
