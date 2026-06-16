package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/richardwooding/projectdetect"
)

// ConfigPathsCmd prints the project-type config search paths for
// the current platform. Pairs with PR #101's auto-discovery — users
// can run this to find out where to drop a ~/Library/Application
// Support/file-search-on/project-types.yaml (macOS) /
// ~/.config/file-search-on/project-types.yaml (Linux) / %AppData%
// equivalent without remembering platform conventions.
type ConfigPathsCmd struct {
	Output string `short:"o" name:"output" enum:"default,bare,json" default:"default" help:"Output format: default (path + scope + existence marker), bare (one path per line — shell-friendly), or json."`
}

func (c *ConfigPathsCmd) Run(_ context.Context) error {
	entries := projectdetect.DiscoveryEntries()
	switch c.Output {
	case "bare":
		for _, e := range entries {
			fpn(os.Stdout, e.Path)
		}
	case "json":
		return printConfigPathsJSON(os.Stdout, entries)
	default:
		printConfigPaths(os.Stdout, entries)
	}
	return nil
}

// DetectProjectCmd inspects a single directory and prints which
// project type(s) it matches. Non-recursive — only the directory's
// own listing is read.
type DetectProjectCmd struct {
	Dir    string `arg:"" optional:"" help:"Directory to inspect. Defaults to '.'." default:"."`
	Output string `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (d *DetectProjectCmd) Run(_ context.Context) error {
	abs, err := filepath.Abs(d.Dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	matches := projectdetect.Detect(nil, abs)
	if d.Output == "json" {
		return printDetectProjectJSON(os.Stdout, abs, matches)
	}
	printDetectProject(os.Stdout, abs, matches)
	return nil
}

// WhichProjectCmd is the path-anchored counterpart to DetectProjectCmd
// and FindProjectsCmd. Given a file (or directory) path it walks up
// the directory chain and reports the nearest enclosing project root.
// Mirrors the MCP `resolve_project_for_path` tool.
type WhichProjectCmd struct {
	Path   string `arg:"" help:"File or directory to anchor on. The walk-up climbs from this path's parent (when a file) or itself (when a directory) until a project root or the filesystem root is reached."`
	Output string `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json (same wire shape as the MCP resolve_project_for_path tool)."`
}

func (w *WhichProjectCmd) Run(_ context.Context) error {
	abs, err := filepath.Abs(w.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	// ResolveForPath walks from Dir(abs). When the caller hands us a
	// directory we want the walk to start AT that directory, not its
	// parent — pretend the caller asked about a sentinel file inside it.
	probe := abs
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		probe = filepath.Join(abs, ".")
	}
	root, matches := projectdetect.ResolveForPath(probe, nil)
	if w.Output == "json" {
		if err := printWhichProjectJSON(os.Stdout, abs, root, matches); err != nil {
			return err
		}
	} else {
		printWhichProject(os.Stdout, abs, root, matches)
	}
	if len(matches) == 0 {
		return &exitCodeError{code: 1}
	}
	return nil
}

// FindProjectsCmd walks a root and prints every project subdirectory
// it finds. Default behaviour: stop at the first match per branch
// (the 'find me all my Go repos' shape). Pass --nested to also surface
// sub-projects inside matched roots.
type FindProjectsCmd struct {
	Dir              string        `arg:"" optional:"" help:"Root directory to walk. Defaults to '.'." default:"."`
	Type             []string      `name:"type" help:"Restrict to specific project types. Repeatable: --type go --type rust."`
	Exclude          []string      `name:"exclude" help:"Basename glob pruned during the walk (e.g. node_modules, .git, target). Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse .gitignore at the walk root and skip matching paths."`
	Nested           bool          `name:"nested" help:"Keep descending into matched project roots so nested sub-projects are also reported."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry, the partial result is still printed and the process exits 124."`
	Output           string        `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (f *FindProjectsCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(f.Dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	result, err := projectdetect.Find(ctx, abs, projectdetect.FindOptions{
		Types:            f.Type,
		Excludes:         f.Exclude,
		RespectGitignore: f.RespectGitignore,
		Nested:           f.Nested,
		Timeout:          f.Timeout,
	})
	if err != nil && !isCancellation(err) {
		return fmt.Errorf("find-projects failed: %w", err)
	}
	if result != nil {
		if f.Output == "json" {
			if err := printFindProjectsJSON(os.Stdout, result); err != nil {
				return err
			}
		} else {
			printFindProjects(os.Stdout, result)
		}
	}
	if result != nil && result.Cancelled {
		switch result.CancellationReason {
		case "client_cancel":
			fmt.Fprintln(os.Stderr, "find-projects interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case "timeout":
			fmt.Fprintf(os.Stderr, "find-projects timed out after %s; results above may be incomplete\n", f.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}
