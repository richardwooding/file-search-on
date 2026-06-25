package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectProjectCmd_Run_GoModule confirms a directory containing
// `go.mod` is detected as a "go" project. The minimal fixture pins
// the most common project type.
func TestDetectProjectCmd_Run_GoModule(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/foo\n\ngo 1.24\n")

	cmd := &DetectProjectCmd{Dir: tmp, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	types, _ := got["project_types"].([]any)
	var hasGo bool
	for _, t := range types {
		if s, _ := t.(string); s == "go" {
			hasGo = true
		}
	}
	if !hasGo {
		t.Errorf("expected project_types to include \"go\" for a go.mod fixture, got %v", got)
	}
}

// TestDetectProjectCmd_Run_NoMatch confirms an empty directory
// produces zero matches without erroring.
func TestDetectProjectCmd_Run_NoMatch(t *testing.T) {
	tmp := t.TempDir() // empty
	cmd := &DetectProjectCmd{Dir: tmp, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	types, _ := got["project_types"].([]any)
	if len(types) != 0 {
		t.Errorf("expected zero project_types for empty dir, got %d: %v", len(types), types)
	}
}

// TestWhichProjectCmd_Run_WalksUp confirms a file inside a project
// resolves to its enclosing root.
func TestWhichProjectCmd_Run_WalksUp(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/foo\n\ngo 1.24\n")
	sub := filepath.Join(tmp, "internal", "deep")
	if err := mkdirAll(sub); err != nil {
		t.Fatalf("mkdir %s: %v", sub, err)
	}
	mustWriteFile(t, filepath.Join(sub, "thing.go"), "package deep\n")

	cmd := &WhichProjectCmd{Path: filepath.Join(sub, "thing.go"), Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	root, _ := got["project_root"].(string)
	if !strings.HasSuffix(root, filepath.Base(tmp)) {
		t.Errorf("expected project_root to end with %q, got %q (full: %v)", filepath.Base(tmp), root, got)
	}
}

// TestWhichProjectCmd_Run_NoEnclosingProject confirms a file with no
// project ancestor still prints JSON output, then returns exit-code 1
// (the documented "not found" signal — see exitCodeError below).
func TestWhichProjectCmd_Run_NoEnclosingProject(t *testing.T) {
	tmp := t.TempDir() // empty — no project markers anywhere
	mustWriteFile(t, filepath.Join(tmp, "loose.txt"), "stray\n")

	cmd := &WhichProjectCmd{Path: filepath.Join(tmp, "loose.txt"), Output: "json"}
	out, runErr := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	// JSON output still lands on stdout even though Run signals "not
	// found" via the exit-code wrapper.
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	if got["project_root"] != "" && got["project_root"] != nil {
		t.Errorf("expected empty project_root for orphan file, got %v", got["project_root"])
	}
	// And confirm Run signalled the not-found condition. We don't
	// pin the specific exitCode-wrapper type to avoid making the
	// test fragile, but the error must be non-nil.
	if runErr == nil {
		t.Errorf("expected Run to signal not-found (non-nil error), got nil")
	}
}

// TestFindProjectsCmd_Run_FindsGoModules walks a tree containing
// two sibling go.mod dirs and asserts both are reported.
func TestFindProjectsCmd_Run_FindsGoModules(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(tmp, name)
		if err := mkdirAll(dir); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/"+name+"\n\ngo 1.24\n")
	}

	cmd := &FindProjectsCmd{Dir: tmp, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	projects, _ := got["projects"].([]any)
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d: %v", len(projects), projects)
	}
}

// TestFindProjectsCmd_Run_SkipsGitDir verifies projectdetect's walk does not
// descend version-control metadata dirs (pruned by default since
// projectdetect v0.4.0): a go.mod buried inside .git must not be reported,
// while a sibling real project is. Closes the .git-descent gap of #478.
func TestFindProjectsCmd_Run_SkipsGitDir(t *testing.T) {
	tmp := t.TempDir()
	// A real project at the root.
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/real\n\ngo 1.24\n")
	// A decoy project buried inside .git — must be pruned, not reported.
	gitSub := filepath.Join(tmp, ".git", "modules", "vendored")
	if err := mkdirAll(gitSub); err != nil {
		t.Fatalf("mkdir %s: %v", gitSub, err)
	}
	mustWriteFile(t, filepath.Join(gitSub, "go.mod"), "module example.com/decoy\n\ngo 1.24\n")

	cmd := &FindProjectsCmd{Dir: tmp, Nested: true, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got struct {
		Projects []struct {
			Path string `json:"path"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	// Exactly the root project — never the decoy buried under .git.
	if len(got.Projects) != 1 {
		t.Fatalf("expected only the real project, got %d: %+v", len(got.Projects), got.Projects)
	}
	if p := filepath.Clean(got.Projects[0].Path); p != filepath.Clean(tmp) {
		t.Errorf("reported project = %q, want the root %q (a .git-buried project leaked)", p, tmp)
	}
}

// TestFindProjectsCmd_Run_TypeFilter restricts the report to a
// single project type and confirms only matching dirs come back.
func TestFindProjectsCmd_Run_TypeFilter(t *testing.T) {
	tmp := t.TempDir()
	goDir := filepath.Join(tmp, "go-side")
	if err := mkdirAll(goDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(goDir, "go.mod"), "module example.com/g\n\ngo 1.24\n")
	npmDir := filepath.Join(tmp, "node-side")
	if err := mkdirAll(npmDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(npmDir, "package.json"), `{"name":"x","version":"0.0.1"}`)

	cmd := &FindProjectsCmd{Dir: tmp, Type: []string{"go"}, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	projects, _ := got["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("expected 1 project (go only), got %d: %v", len(projects), projects)
	}
	p, _ := projects[0].(map[string]any)
	if path, _ := p["path"].(string); !strings.HasSuffix(path, "go-side") {
		t.Errorf("expected the go-side project to win the type filter, got path=%q", path)
	}
}

// TestFindProjectsCmd_Run_EmptyTree confirms a tree with no project
// markers returns zero projects without erroring.
func TestFindProjectsCmd_Run_EmptyTree(t *testing.T) {
	tmp := t.TempDir() // empty
	cmd := &FindProjectsCmd{Dir: tmp, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	projects, _ := got["projects"].([]any)
	if len(projects) != 0 {
		t.Errorf("expected zero projects in empty tree, got %d", len(projects))
	}
}

// mkdirAll is a thin os.MkdirAll wrapper without test plumbing —
// project_cmd_test.go uses it across a few setups.
func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}
