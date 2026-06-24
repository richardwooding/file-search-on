package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestValidateCmd_Valid confirms a well-formed expression compiles,
// surfaces its referenced variables, and exits zero.
func TestValidateCmd_Valid(t *testing.T) {
	cmd := &ValidateCmd{Expr: "is_pdf && page_count > 10", Output: "text"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("valid expr should exit 0, got: %v", err)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK in output, got: %q", out)
	}
	if !strings.Contains(out, "page_count") {
		t.Errorf("expected referenced variable page_count, got: %q", out)
	}
}

// TestValidateCmd_TypoExitsNonZero confirms a typo'd identifier fails,
// emits a 'did you mean' suggestion, and requests a non-zero exit code
// so the command works as a CI gate.
func TestValidateCmd_TypoExitsNonZero(t *testing.T) {
	cmd := &ValidateCmd{Expr: "is_pdf && pag_count > 10", Output: "text"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatal("invalid expr should return a non-nil error")
	}
	var ec *exitCodeError
	if !errors.As(err, &ec) || ec.code != 1 {
		t.Fatalf("expected exitCodeError{code:1}, got: %v", err)
	}
	if !strings.Contains(out, "INVALID") {
		t.Errorf("expected INVALID in output, got: %q", out)
	}
	if !strings.Contains(out, "page_count") {
		t.Errorf("expected 'did you mean page_count' suggestion, got: %q", out)
	}
}

// TestValidateCmd_JSON checks the JSON shape mirrors the MCP tool's
// snake_case field names.
func TestValidateCmd_JSON(t *testing.T) {
	cmd := &ValidateCmd{Expr: "is_markdown && word_count > 5", Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got struct {
		OK                  bool     `json:"ok"`
		ReferencedVariables []string `json:"referenced_variables"`
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	if !got.OK {
		t.Errorf("expected ok=true, got false; raw: %q", out)
	}
	if len(got.ReferencedVariables) != 2 {
		t.Errorf("expected 2 referenced variables, got %v", got.ReferencedVariables)
	}
}
