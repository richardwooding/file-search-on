package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestComplexitySARIF runs the complexity command with --output sarif over a
// fixture containing a deliberately-branchy function and asserts the output is
// a valid SARIF 2.1.0 document with a located result. Covers the CLI wiring
// end-to-end (the sarif package itself is unit-tested separately).
func TestComplexitySARIF(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	mustWriteFile(t, filepath.Join(tmp, "branchy.go"), "package m\n\n"+
		"func Branchy(x int) int {\n"+
		"\tif x > 0 { x++ } else { x-- }\n"+
		"\tfor i := 0; i < x; i++ { if i%2 == 0 { x += i } }\n"+
		"\tswitch x { case 1: return 1; case 2: return 2; default: return x }\n"+
		"}\n")

	cmd := &ComplexityCmd{Top: 50, Output: "sarif"}
	cmd.Dir = []string{tmp}
	cmd.NoIndex = true

	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if derr := json.NewDecoder(strings.NewReader(out)).Decode(&doc); derr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", derr, out)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", doc.Version)
	}
	if len(doc.Runs) != 1 || doc.Runs[0].Tool.Driver.Name != "file-search-on" {
		t.Fatalf("unexpected runs/driver: %+v", doc.Runs)
	}
	results := doc.Runs[0].Results
	if len(results) == 0 {
		t.Fatal("expected at least one complexity result for Branchy")
	}
	r0 := results[0]
	if r0.RuleID != "complexity" {
		t.Errorf("ruleId = %q, want complexity", r0.RuleID)
	}
	loc := r0.Locations[0].PhysicalLocation
	if !strings.HasSuffix(loc.ArtifactLocation.URI, "branchy.go") {
		t.Errorf("uri = %q, want .../branchy.go", loc.ArtifactLocation.URI)
	}
	if loc.Region.StartLine <= 0 {
		t.Errorf("startLine = %d, want > 0", loc.Region.StartLine)
	}
}

// sarifDoc is the minimal SARIF shape the CLI SARIF tests decode against.
type sarifDoc struct {
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct {
				Name  string `json:"name"`
				Rules []struct {
					ID string `json:"id"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
			RuleID    string `json:"ruleId"`
			Level     string `json:"level"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

func decodeSARIF(t *testing.T, out string) sarifDoc {
	t.Helper()
	var doc sarifDoc
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", doc.Version)
	}
	if len(doc.Runs) != 1 || doc.Runs[0].Tool.Driver.Name != "file-search-on" {
		t.Fatalf("unexpected runs/driver: %+v", doc.Runs)
	}
	return doc
}

// TestCoverageGapsSARIF runs coverage-gaps -o sarif over a tiny module + profile
// with a partially-covered function and asserts a located coverage-gap result
// (with a line region).
func TestCoverageGapsSARIF(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	mustWriteFile(t, filepath.Join(tmp, "foo.go"), "package m\n\n"+
		"func Foo(x int) int {\n"+
		"\tif x > 0 {\n\t\treturn x\n\t}\n"+
		"\treturn -x\n"+
		"}\n")
	profile := "mode: set\n" +
		"example.com/m/foo.go:3.20,4.11 1 1\n" +
		"example.com/m/foo.go:4.11,6.3 1 0\n"
	mustWriteFile(t, filepath.Join(tmp, "cover.out"), profile)

	cmd := &CoverageGapsCmd{Profile: filepath.Join(tmp, "cover.out"), Dir: tmp, Output: "sarif"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	doc := decodeSARIF(t, out)
	results := doc.Runs[0].Results
	if len(results) == 0 {
		t.Fatal("expected at least one coverage-gap result")
	}
	r0 := results[0]
	if r0.RuleID != "coverage-gap" {
		t.Errorf("ruleId = %q, want coverage-gap", r0.RuleID)
	}
	loc := r0.Locations[0].PhysicalLocation
	if !strings.HasSuffix(loc.ArtifactLocation.URI, "foo.go") {
		t.Errorf("uri = %q, want .../foo.go", loc.ArtifactLocation.URI)
	}
	if loc.Region.StartLine <= 0 {
		t.Errorf("startLine = %d, want > 0", loc.Region.StartLine)
	}
}

// TestUnusedExportsSARIF runs unused-exports -o sarif over a module whose
// exported symbol is referenced only intra-package and asserts a file-level
// unused-export result (no line region).
func TestUnusedExportsSARIF(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	mustWriteFile(t, filepath.Join(tmp, "lib.go"), "package m\n\n"+
		"func Helper() int { return 42 }\n\n"+
		"func use() int { return Helper() }\n")

	cmd := &UnusedExportsCmd{Dir: tmp, Output: "sarif"}
	cmd.NoIndex = true
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	doc := decodeSARIF(t, out)
	results := doc.Runs[0].Results
	if len(results) == 0 {
		t.Fatal("expected at least one unused-export result for Helper")
	}
	r0 := results[0]
	if r0.RuleID != "unused-export" {
		t.Errorf("ruleId = %q, want unused-export", r0.RuleID)
	}
	if r0.Level != "note" {
		t.Errorf("level = %q, want note", r0.Level)
	}
	loc := r0.Locations[0].PhysicalLocation
	if !strings.HasSuffix(loc.ArtifactLocation.URI, "lib.go") {
		t.Errorf("uri = %q, want .../lib.go", loc.ArtifactLocation.URI)
	}
	if loc.Region.StartLine != 0 {
		t.Errorf("startLine = %d, want 0 (file-level, no region)", loc.Region.StartLine)
	}
}

// TestDuplicateFunctionsSARIF runs duplicate-functions -o sarif over two
// identical functions in different packages and asserts one located
// duplicate-function result per cluster member.
func TestDuplicateFunctionsSARIF(t *testing.T) {
	tmp := t.TempDir()
	dup := "func process(items []int) int {\n" +
		"\ttotal := 0\n\tfor _, v := range items {\n\t\ttotal += v\n\t}\n" +
		"\tif total > 100 {\n\t\ttotal = 100\n\t}\n\treturn total\n}\n"
	mustWriteFile(t, filepath.Join(tmp, "a.go"), "package a\n\n"+dup)
	mustWriteFile(t, filepath.Join(tmp, "b.go"), "package b\n\n"+dup)

	cmd := &DuplicateFunctionsCmd{Output: "sarif"}
	cmd.Dir = []string{tmp}
	cmd.NoIndex = true
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	doc := decodeSARIF(t, out)
	results := doc.Runs[0].Results
	if len(results) < 2 {
		t.Fatalf("expected >=2 duplicate-function results (one per member), got %d", len(results))
	}
	r0 := results[0]
	if r0.RuleID != "duplicate-function" {
		t.Errorf("ruleId = %q, want duplicate-function", r0.RuleID)
	}
	loc := r0.Locations[0].PhysicalLocation
	if loc.Region.StartLine <= 0 {
		t.Errorf("startLine = %d, want > 0", loc.Region.StartLine)
	}
}
