package main

import (
	"os"
	"testing"

	"github.com/alecthomas/kong"
)

// unsetenvForTest deletes envVar for the duration of the test, restoring
// the previous value (or absence) on cleanup. Needed because t.Setenv("",
// "") *sets* the var to empty rather than unsetting it.
func unsetenvForTest(t *testing.T, envVar string) {
	t.Helper()
	prev, had := os.LookupEnv(envVar)
	if err := os.Unsetenv(envVar); err != nil {
		t.Fatalf("unset %s: %v", envVar, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(envVar, prev)
		} else {
			_ = os.Unsetenv(envVar)
		}
	})
}

// TestEmbeddingServer_OllamaHostEnv asserts kong's env-var fallback on
// the `embedding-server` flag — both SearchCmd and MCPCmd should pick
// up OLLAMA_HOST when no --embedding-server flag is passed. Guards
// against future regression if someone strips the `env:"OLLAMA_HOST"`
// tag.
func TestEmbeddingServer_OllamaHostEnv(t *testing.T) {
	const wantURL = "http://gpu-box.local:11434"
	t.Setenv("OLLAMA_HOST", wantURL)

	t.Run("search subcommand picks up OLLAMA_HOST", func(t *testing.T) {
		var cli struct {
			Search SearchCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"search"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := cli.Search.EmbeddingServer; got != wantURL {
			t.Errorf("EmbeddingServer = %q, want %q (from $OLLAMA_HOST)", got, wantURL)
		}
	})

	t.Run("mcp subcommand picks up OLLAMA_HOST", func(t *testing.T) {
		var cli struct {
			MCP MCPCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"mcp"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := cli.MCP.EmbeddingServer; got != wantURL {
			t.Errorf("EmbeddingServer = %q, want %q (from $OLLAMA_HOST)", got, wantURL)
		}
	})

	t.Run("explicit --embedding-server flag overrides $OLLAMA_HOST", func(t *testing.T) {
		const flagURL = "http://override.example:11434"
		var cli struct {
			Search SearchCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"search", "--embedding-server", flagURL}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got := cli.Search.EmbeddingServer; got != flagURL {
			t.Errorf("EmbeddingServer = %q, want %q (from --embedding-server flag)", got, flagURL)
		}
	})
}

// TestEmbeddingServer_DefaultFallback asserts the hardcoded
// http://localhost:11434 default still kicks in when neither
// --embedding-server nor $OLLAMA_HOST is set.
func TestEmbeddingServer_DefaultFallback(t *testing.T) {
	unsetenvForTest(t, "OLLAMA_HOST")

	var cli struct {
		Search SearchCmd `cmd:""`
	}
	parser, err := kong.New(&cli)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err := parser.Parse([]string{"search"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	const wantDefault = "http://localhost:11434"
	if got := cli.Search.EmbeddingServer; got != wantDefault {
		t.Errorf("EmbeddingServer = %q, want default %q", got, wantDefault)
	}
}
