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
