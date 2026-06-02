package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func writeTextFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestStripLeadingBoilerplate_License(t *testing.T) {
	syntax, _ := commentSyntaxFor("go")
	body := `// Copyright 2026 Example Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package foo

func Hello() {}
`
	got := stripLeadingBoilerplate(body, syntax)
	if strings.Contains(got, "Copyright") {
		t.Errorf("license header survived: %q", got)
	}
	if !strings.Contains(got, "func Hello") {
		t.Errorf("function body should remain: %q", got)
	}
}

func TestStripLeadingBoilerplate_BlockComment(t *testing.T) {
	syntax, _ := commentSyntaxFor("go")
	body := `/*
 * Multi-line block license header.
 * Several lines of legal text.
 */
package foo
`
	got := stripLeadingBoilerplate(body, syntax)
	if strings.Contains(got, "Multi-line block") {
		t.Errorf("block-comment header survived: %q", got)
	}
}

func TestStripLeadingBoilerplate_NoHeader(t *testing.T) {
	syntax, _ := commentSyntaxFor("go")
	body := `package foo

func Hello() {}
`
	got := stripLeadingBoilerplate(body, syntax)
	// First "real" line is `package foo`, so nothing to strip.
	if !strings.HasPrefix(got, "package foo") {
		t.Errorf("non-comment first line should survive: %q", got)
	}
}

func TestStripGoPackageImports_Parenthesised(t *testing.T) {
	body := `package foo

import (
	"context"
	"fmt"
	myalias "github.com/x/y"
)

func Hello() {}
`
	got := stripGoPackageImports(body)
	// All import-block content gone.
	for _, banned := range []string{"package foo", "import (", "\"context\"", "\"fmt\"", "myalias", "github.com/x/y"} {
		if strings.Contains(got, banned) {
			t.Errorf("stripGoPackageImports left %q in output: %q", banned, got)
		}
	}
	if !strings.Contains(got, "func Hello") {
		t.Errorf("function body should survive: %q", got)
	}
}

func TestStripGoPackageImports_SingleLine(t *testing.T) {
	body := `package foo
import "context"
import "fmt"

func Hello() {}
`
	got := stripGoPackageImports(body)
	if strings.Contains(got, "context") || strings.Contains(got, "fmt") {
		t.Errorf("single-line imports survived: %q", got)
	}
	if !strings.Contains(got, "func Hello") {
		t.Errorf("function body should survive: %q", got)
	}
}

func TestStripPythonImports(t *testing.T) {
	body := `import os
from pathlib import Path
import json

def hello():
    pass
`
	got := stripPythonImports(body)
	for _, banned := range []string{"import os", "from pathlib", "import json"} {
		if strings.Contains(got, banned) {
			t.Errorf("python import survived: %q", banned)
		}
	}
	if !strings.Contains(got, "def hello") {
		t.Errorf("function body should survive: %q", got)
	}
}

func TestStripJavaStyleImports(t *testing.T) {
	body := `package com.example.foo;

import java.util.List;
import java.util.Map;

public class Hello {
}
`
	got := stripJavaStyleImports(body)
	if strings.Contains(got, "package com.example") || strings.Contains(got, "import java") {
		t.Errorf("java boilerplate survived: %q", got)
	}
	if !strings.Contains(got, "class Hello") {
		t.Errorf("class body should survive: %q", got)
	}
}

func TestStripRustUse(t *testing.T) {
	body := `use std::collections::HashMap;
extern crate serde;

fn main() {}
`
	got := stripRustUse(body)
	if strings.Contains(got, "use std") || strings.Contains(got, "extern crate") {
		t.Errorf("rust use survived: %q", got)
	}
	if !strings.Contains(got, "fn main") {
		t.Errorf("function body should survive: %q", got)
	}
}

// TestPreprocessForFingerprint_GoFilesWithSameImportsDiffer is the
// integration-shape regression test for #274. Two Go files with
// identical license headers + imports but different function bodies
// should produce DIFFERENT preprocessed bodies (and therefore
// different SimHashes).
func TestPreprocessForFingerprint_GoFilesWithSameImportsDiffer(t *testing.T) {
	header := `// Copyright 2026 Example Inc.
// Licensed under Apache 2.0

package foo

import (
	"context"
	"fmt"
	"io"
)

`
	bodyA := header + `func ProcessUserRecord(ctx context.Context, id int) error {
	return fmt.Errorf("user %d not found", id)
}
`
	bodyB := header + `func SerialiseAuditLog(ctx context.Context, w io.Writer) error {
	return fmt.Errorf("audit log buffer full")
}
`
	gotA := preprocessForFingerprint(bodyA, "source/go")
	gotB := preprocessForFingerprint(bodyB, "source/go")
	// Headers + imports stripped from both.
	if strings.Contains(gotA, "Copyright") || strings.Contains(gotB, "Copyright") {
		t.Error("headers should be stripped")
	}
	if strings.Contains(gotA, "package foo") || strings.Contains(gotB, "package foo") {
		t.Error("package decl should be stripped")
	}
	if strings.Contains(gotA, "\"context\"") || strings.Contains(gotB, "\"context\"") {
		t.Error("imports should be stripped")
	}
	// Distinct function bodies survive.
	if !strings.Contains(gotA, "ProcessUserRecord") {
		t.Errorf("bodyA's function lost: %q", gotA)
	}
	if !strings.Contains(gotB, "SerialiseAuditLog") {
		t.Errorf("bodyB's function lost: %q", gotB)
	}
	// The two preprocessed strings must differ.
	if gotA == gotB {
		t.Error("preprocessed bodies should differ when functions differ")
	}
}

func TestPreprocessForFingerprint_NonSourcePassthrough(t *testing.T) {
	body := "# Title\n\nSome content with TODO inline.\n"
	got := preprocessForFingerprint(body, "markdown")
	if got != body {
		t.Errorf("markdown should pass through unchanged: in=%q out=%q", body, got)
	}
}

func TestPreprocessForFingerprint_EmptyBody(t *testing.T) {
	if got := preprocessForFingerprint("", "source/go"); got != "" {
		t.Errorf("empty body should pass through unchanged, got %q", got)
	}
}

func TestPreprocessForFingerprint_UnknownLanguage(t *testing.T) {
	body := `; assembly maybe
mov eax, 1
`
	// "source/whatever" → languageFromContentType strips "source/" but
	// "whatever" isn't in languageSyntax; preprocess returns unchanged.
	got := preprocessForFingerprint(body, "source/whatever")
	if got != body {
		t.Errorf("unknown language should pass through, got %q", got)
	}
}

// TestFindNearDuplicates_AutoBumpThresholdForSourceTree confirms the
// engine picks 0.92 (tighter) when most candidates have a known
// source language, and 0.85 (looser) for prose / markdown. Issue #274.
func TestFindNearDuplicates_AutoBumpThresholdForSourceTree(t *testing.T) {
	dir := t.TempDir()
	// Three minimal Go files — different bodies but identical
	// scaffolding. Without the auto-bump they'd group at 0.85.
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		body := "package foo\n\nimport \"context\"\n\nfunc " + name[:1] + "() {}\n_ = " + name[:1]
		writeTextFile(t, dir, name, body)
	}
	out, err := FindNearDuplicates(context.Background(), Options{
		Root:    dir,
		Expr:    `is_source && language == "go"`,
		Workers: 1,
		// SimilarityThreshold: 0 → engine picks
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if out.Threshold != 0.92 {
		t.Errorf("expected auto-bumped threshold 0.92 for source tree; got %g", out.Threshold)
	}
}

func TestFindNearDuplicates_NoBumpForMarkdownTree(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		body := "# Notes\n\nSome distinct paragraph about " + name[:1] + ".\n"
		writeTextFile(t, dir, name, body)
	}
	out, err := FindNearDuplicates(context.Background(), Options{
		Root:    dir,
		Expr:    `is_markdown`,
		Workers: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if out.Threshold != 0.85 {
		t.Errorf("expected unchanged threshold 0.85 for markdown tree; got %g", out.Threshold)
	}
}

func TestFindNearDuplicates_ExplicitThresholdWins(t *testing.T) {
	dir := t.TempDir()
	body := "package foo\nfunc x() {}\n"
	for _, name := range []string{"a.go", "b.go"} {
		writeTextFile(t, dir, name, body)
	}
	out, err := FindNearDuplicates(context.Background(), Options{
		Root:                dir,
		Expr:                `is_source`,
		SimilarityThreshold: 0.5, // operator explicitly lowered
		Workers:             1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if out.Threshold != 0.5 {
		t.Errorf("explicit threshold should win; got %g, want 0.5", out.Threshold)
	}
}
