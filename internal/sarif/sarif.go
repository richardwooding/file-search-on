// Package sarif renders analysis findings as a SARIF 2.1.0 document — the
// Static Analysis Results Interchange Format that GitHub Code Scanning,
// GitLab, and other CI systems ingest. Each file-search-on analysis command
// (complexity, dead-code, find-matches, …) maps its findings to []Result and
// calls Write; the rule table and document scaffolding are built here. Issue
// #483.
package sarif

import (
	"encoding/json"
	"io"
)

const (
	schemaURL = "https://json.schemastore.org/sarif-2.1.0.json"
	// infoURI is the tool's information URI in the SARIF driver.
	infoURI = "https://github.com/richardwooding/file-search-on"
	// toolName is the SARIF driver name surfaced in Code Scanning.
	toolName = "file-search-on"
)

// Result is one finding: a rule id, severity level, human message, and
// location. A StartLine <= 0 omits the line region, producing a file-level
// result (e.g. dead-code / unused-exports, which have no single line).
type Result struct {
	RuleID    string
	Level     string // "error" | "warning" | "note"; defaults to "warning" when empty
	Message   string
	URI       string // file path, relative to the repo root
	StartLine int
	EndLine   int
}

// Rule describes one rule id for the SARIF tool.driver.rules table. Each
// distinct RuleID emitted in results should have a matching Rule.
type Rule struct {
	ID          string
	Name        string
	Description string
}

// Write encodes results as a SARIF 2.1.0 document to w (pretty-printed).
// rules supplies the rule metadata; version stamps the tool driver. Passing
// an empty results slice still emits a valid (empty-results) run.
func Write(w io.Writer, version string, rules []Rule, results []Result) error {
	d := document{Schema: schemaURL, Version: "2.1.0"}
	r := run{}
	r.Tool.Driver = driver{
		Name:           toolName,
		Version:        version,
		InformationURI: infoURI,
		Rules:          make([]jsonRule, 0, len(rules)),
	}
	for _, rule := range rules {
		jr := jsonRule{ID: rule.ID, Name: rule.Name}
		if rule.Description != "" {
			jr.ShortDescription = &text{Text: rule.Description}
		}
		r.Tool.Driver.Rules = append(r.Tool.Driver.Rules, jr)
	}
	r.Results = make([]jsonResult, 0, len(results))
	for _, res := range results {
		level := res.Level
		if level == "" {
			level = "warning"
		}
		loc := location{}
		loc.PhysicalLocation.ArtifactLocation.URI = res.URI
		if res.StartLine > 0 {
			reg := &region{StartLine: res.StartLine}
			if res.EndLine >= res.StartLine {
				reg.EndLine = res.EndLine
			}
			loc.PhysicalLocation.Region = reg
		}
		r.Results = append(r.Results, jsonResult{
			RuleID:    res.RuleID,
			Level:     level,
			Message:   text{Text: res.Message},
			Locations: []location{loc},
		})
	}
	d.Runs = []run{r}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

type document struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []run  `json:"runs"`
}

type run struct {
	Tool    tool         `json:"tool"`
	Results []jsonResult `json:"results"`
}

type tool struct {
	Driver driver `json:"driver"`
}

type driver struct {
	Name           string     `json:"name"`
	Version        string     `json:"version,omitempty"`
	InformationURI string     `json:"informationUri"`
	Rules          []jsonRule `json:"rules"`
}

type jsonRule struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	ShortDescription *text  `json:"shortDescription,omitempty"`
}

type jsonResult struct {
	RuleID    string     `json:"ruleId"`
	Level     string     `json:"level"`
	Message   text       `json:"message"`
	Locations []location `json:"locations"`
}

type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
	Region           *region          `json:"region,omitempty"`
}

type artifactLocation struct {
	URI string `json:"uri"`
}

type region struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine,omitempty"`
}

type text struct {
	Text string `json:"text"`
}
