package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// captureStdout swaps os.Stdout for a pipe for the duration of fn,
// returning what was written. Used to assert command output without
// having to mock the formatter. Originally introduced in
// attrs_test.go and lifted here for reuse across the CLI test suite.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	runErr := fn()
	_ = w.Close()
	<-done
	return buf.String(), runErr
}

// mustWriteFile writes body to path with 0644 perms, fatally failing
// the test on any I/O error. Used to seed t.TempDir() fixtures.
// Originally introduced in timeout_test.go and lifted here for
// reuse across the CLI test suite.
func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
