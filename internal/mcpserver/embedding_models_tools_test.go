package mcpserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubOllama mounts /api/tags + /api/pull on an httptest server.
// localOnTags is the JSON body returned for /api/tags; pullScript is
// the NDJSON stream returned for /api/pull. Empty pullScript makes
// /api/pull return a synthetic success event.
func stubOllama(t *testing.T, localOnTags string, pullScript ...string) *httptest.Server {
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

func TestListEmbeddingModelsTool(t *testing.T) {
	srv := stubOllama(t, `{
		"models": [
			{"name":"nomic-embed-text:latest","size":274302450,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"},
			{"name":"llama3:latest","size":4661224500,"modified_at":"2026-05-01T10:00:00Z","digest":"def"}
		]
	}`)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_embedding_models",
		Arguments: ListEmbeddingModelsInput{
			EmbeddingServer: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out ListEmbeddingModelsOutput
	mustDecodeStructured(t, res, &out)

	if out.Server != srv.URL {
		t.Errorf("Server = %q, want %q", out.Server, srv.URL)
	}
	if len(out.Local) != 2 {
		t.Fatalf("Local len = %d, want 2", len(out.Local))
	}

	// nomic-embed-text is catalogued; llama3 isn't.
	var nomic, llama *LocalModelOut
	for i := range out.Local {
		switch out.Local[i].Name {
		case "nomic-embed-text:latest":
			nomic = &out.Local[i]
		case "llama3:latest":
			llama = &out.Local[i]
		}
	}
	if nomic == nil || !nomic.Catalogued {
		t.Errorf("nomic-embed-text should be catalogued: %+v", nomic)
	}
	if nomic != nil && nomic.Dimensions != 768 {
		t.Errorf("nomic dims = %d, want 768", nomic.Dimensions)
	}
	if llama == nil || llama.Catalogued {
		t.Errorf("llama3 should not be catalogued: %+v", llama)
	}

	// Catalog arm: nomic should be Pulled, others not.
	var nomicCat *CatalogOut
	for i := range out.Catalog {
		if out.Catalog[i].Name == "nomic-embed-text" {
			nomicCat = &out.Catalog[i]
		}
	}
	if nomicCat == nil {
		t.Fatalf("nomic-embed-text missing from catalog")
	}
	if !nomicCat.Pulled {
		t.Errorf("nomic catalog entry should be marked Pulled=true")
	}

	// Every catalog entry should be present; at least one (mxbai-embed-large)
	// should be Pulled=false.
	if len(out.Catalog) < 5 {
		t.Errorf("catalog has %d entries, want at least 5", len(out.Catalog))
	}
	var unpulledFound bool
	for _, c := range out.Catalog {
		if !c.Pulled {
			unpulledFound = true
		}
	}
	if !unpulledFound {
		t.Errorf("expected at least one Pulled=false in catalog")
	}
}

func TestListEmbeddingModelsTool_ServerUnreachable(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_embedding_models",
		Arguments: ListEmbeddingModelsInput{
			EmbeddingServer: "http://127.0.0.1:1", // refuses connect
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for unreachable Ollama, got IsError=false")
	}
}

func TestPullEmbeddingModelTool_AlreadyPulledShortcut(t *testing.T) {
	srv := stubOllama(t, `{"models":[{"name":"nomic-embed-text:latest","size":1,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"}]}`)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "pull_embedding_model",
		Arguments: PullEmbeddingModelInput{
			Name:            "nomic-embed-text",
			EmbeddingServer: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out PullEmbeddingModelOutput
	mustDecodeStructured(t, res, &out)

	if !out.AlreadyPulled {
		t.Errorf("AlreadyPulled = false, want true (model was in /api/tags)")
	}
	if out.DurationSeconds != 0 {
		t.Errorf("DurationSeconds = %v, want 0 (shortcut path)", out.DurationSeconds)
	}
}

func TestPullEmbeddingModelTool_FreshPull(t *testing.T) {
	srv := stubOllama(t,
		`{"models":[]}`, // /api/tags: nothing pulled yet
		`{"status":"pulling manifest"}`,
		`{"status":"downloading","digest":"sha256:a","total":1000,"completed":500}`,
		`{"status":"downloading","digest":"sha256:a","total":1000,"completed":1000}`,
		`{"status":"success"}`,
	)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "pull_embedding_model",
		Arguments: PullEmbeddingModelInput{
			Name:            "nomic-embed-text",
			EmbeddingServer: srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out PullEmbeddingModelOutput
	mustDecodeStructured(t, res, &out)

	if out.AlreadyPulled {
		t.Errorf("AlreadyPulled = true, want false (fresh pull)")
	}
	if out.TotalBytes != 1000 {
		t.Errorf("TotalBytes = %d, want 1000", out.TotalBytes)
	}
	if out.Name != "nomic-embed-text" {
		t.Errorf("Name = %q, want nomic-embed-text", out.Name)
	}
}

func TestPullEmbeddingModelTool_MissingName(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "pull_embedding_model",
		Arguments: PullEmbeddingModelInput{}, // no name
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error for empty name, got IsError=false")
	}
}
