package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildfilesDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantType string
	}{
		{"Dockerfile", "build/dockerfile"},
		{"Containerfile", "build/dockerfile"},
		{"Makefile", "build/makefile"},
		{"GNUmakefile", "build/makefile"},
		{"BSDmakefile", "build/makefile"},
		{"makefile", "build/makefile"},
		{"Justfile", "build/justfile"},
		{"justfile", "build/justfile"},
		{"Rakefile", "build/rakefile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte("# stub\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect: got nil, want %s", tc.wantType)
			}
			if ct.Name() != tc.wantType {
				t.Errorf("Detect: got %q, want %q", ct.Name(), tc.wantType)
			}
		})
	}
}

func TestDockerfileBaseImage(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		body     string
		wantBase string
	}{
		{"alpine.Dockerfile", "FROM alpine:3.20\nRUN apk add curl\n", "alpine:3.20"},
		// Multi-stage: only first FROM is surfaced.
		{"multi.Dockerfile", "FROM golang:1.22 AS builder\nFROM scratch\nCOPY --from=builder /out /app\n", "golang:1.22"},
		// --platform flag should be skipped to find the image.
		{"platform.Dockerfile", "FROM --platform=linux/amd64 alpine\n", "alpine"},
		// Comments + leading whitespace shouldn't trip the parser.
		{"comments.Dockerfile", "# syntax=docker/dockerfile:1\n\n   FROM python:3.12-slim\n", "python:3.12-slim"},
		// Case-insensitive: "from" lowercase per Dockerfile spec.
		{"lowercase.Dockerfile", "from busybox\n", "busybox"},
		// Empty Dockerfile: no base_image attr.
		{"empty.Dockerfile", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a unique subdir so Dockerfile name collisions across
			// cases don't trip the test.
			sub := t.TempDir()
			path := filepath.Join(sub, "Dockerfile")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil || ct.Name() != "build/dockerfile" {
				t.Fatalf("Detect: got %v, want build/dockerfile", ct)
			}
			attrs, err := attributesAt(t.Context(), ct, path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			got, _ := attrs["base_image"].(string)
			if got != tc.wantBase {
				t.Errorf("base_image = %q, want %q", got, tc.wantBase)
			}
		})
	}
	_ = dir
}
