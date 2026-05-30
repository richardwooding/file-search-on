package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubOllama matches the helper in mcpserver — same shape, copied to
// keep the CLI test package self-contained.
func stubOllamaCLI(t *testing.T, localOnTags string, pullScript ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(localOnTags))
		case "/api/pull":
			w.Header().Set("Content-Type", "application/x-ndjson")
			flusher, _ := w.(http.Flusher)
			if len(pullScript) == 0 {
				_, _ = fmt.Fprintln(w, `{"status":"success"}`)
				return
			}
			for _, line := range pullScript {
				_, _ = fmt.Fprintln(w, line)
				if flusher != nil {
					flusher.Flush()
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmbedListCmd_Default(t *testing.T) {
	srv := stubOllamaCLI(t, `{
		"models": [
			{"name":"nomic-embed-text:latest","size":274302450,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"},
			{"name":"llama3:latest","size":4661224500,"modified_at":"2026-05-01T10:00:00Z","digest":"def"}
		]
	}`)

	c := &EmbedListCmd{Server: srv.URL, Output: "default"}
	out, err := captureStdout(t, func() error { return c.Run(context.Background()) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "LOCALLY PULLED") {
		t.Errorf("missing LOCALLY PULLED header; got: %s", out)
	}
	if !strings.Contains(out, "nomic-embed-text:latest") {
		t.Errorf("missing nomic local entry; got: %s", out)
	}
	if !strings.Contains(out, "[catalogued]") {
		t.Errorf("nomic should be marked catalogued; got: %s", out)
	}
	if !strings.Contains(out, "llama3:latest") {
		t.Errorf("missing llama3 local entry; got: %s", out)
	}
	if !strings.Contains(out, "NOT YET PULLED") {
		t.Errorf("missing NOT YET PULLED header; got: %s", out)
	}
	if !strings.Contains(out, "mxbai-embed-large") {
		t.Errorf("missing recommended model; got: %s", out)
	}
	// nomic is pulled, so it should NOT appear under NOT YET PULLED.
	pulledSection := strings.SplitN(out, "NOT YET PULLED", 2)
	if len(pulledSection) == 2 && strings.Contains(pulledSection[1], "nomic-embed-text  ") {
		t.Errorf("nomic should not appear in NOT YET PULLED section")
	}
}

func TestEmbedListCmd_JSON(t *testing.T) {
	srv := stubOllamaCLI(t, `{
		"models": [
			{"name":"nomic-embed-text:latest","size":274302450,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"}
		]
	}`)

	c := &EmbedListCmd{Server: srv.URL, Output: "json"}
	out, err := captureStdout(t, func() error { return c.Run(context.Background()) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var resp embedListJSON
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("decode JSON: %v\noutput: %s", err, out)
	}
	if resp.Server != srv.URL {
		t.Errorf("Server = %q, want %q", resp.Server, srv.URL)
	}
	if len(resp.Local) != 1 {
		t.Errorf("Local len = %d, want 1", len(resp.Local))
	}
	if !resp.Local[0].Catalogued {
		t.Errorf("nomic should be Catalogued in JSON output")
	}
	if len(resp.Catalog) < 5 {
		t.Errorf("Catalog should have ≥ 5 entries, got %d", len(resp.Catalog))
	}
	var nomicCat *catalogOut
	for i := range resp.Catalog {
		if resp.Catalog[i].Name == "nomic-embed-text" {
			nomicCat = &resp.Catalog[i]
		}
	}
	if nomicCat == nil || !nomicCat.Pulled {
		t.Errorf("nomic catalog entry should have Pulled=true")
	}
}

func TestEmbedListCmd_UnreachableServer(t *testing.T) {
	c := &EmbedListCmd{Server: "http://127.0.0.1:1", Output: "json"}
	_, err := captureStdout(t, func() error { return c.Run(context.Background()) })
	if err == nil {
		t.Errorf("expected error for unreachable Ollama, got nil")
	}
}

func TestEmbedPullCmd_AlreadyPulled(t *testing.T) {
	srv := stubOllamaCLI(t, `{"models":[{"name":"nomic-embed-text:latest","size":1,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"}]}`)

	c := &EmbedPullCmd{Name: "nomic-embed-text", Server: srv.URL, Quiet: true}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEmbedPullCmd_FreshPull(t *testing.T) {
	srv := stubOllamaCLI(t,
		`{"models":[]}`,
		`{"status":"pulling manifest"}`,
		`{"status":"downloading","digest":"sha256:a","total":1000,"completed":500}`,
		`{"status":"success"}`,
	)
	c := &EmbedPullCmd{Name: "nomic-embed-text", Server: srv.URL, Quiet: true}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEmbedPullCmd_PullError(t *testing.T) {
	srv := stubOllamaCLI(t,
		`{"models":[]}`,
		`{"error":"pull model manifest: file does not exist"}`,
	)
	c := &EmbedPullCmd{Name: "ghost-model", Server: srv.URL, Quiet: true}
	err := c.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error for ollama in-stream error, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-model") {
		t.Errorf("error should mention model name, got %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{500, "500 B"},
		{1500, "1.5 KB"},
		{1024 * 1024 * 2, "2.0 MB"},
		{1024 * 1024 * 1024 * 3, "3.00 GB"},
	} {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
