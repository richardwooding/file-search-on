package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/richardwooding/file-search-on/internal/embed"
)

// PullEmbeddingModelInput is the input shape for `pull_embedding_model`.
type PullEmbeddingModelInput struct {
	// Name is the bare model name Ollama should pull (e.g. "nomic-embed-text").
	// No tag suffix = pulls :latest, same as `ollama pull`.
	Name string `json:"name" jsonschema:"Model name to pull (e.g. nomic-embed-text). Omit tag suffix to pull :latest. Required."`
	// EmbeddingServer overrides the server's default Ollama base URL.
	EmbeddingServer string `json:"embedding_server,omitempty" jsonschema:"Override the server's default Ollama base URL for this call."`
}

// PullEmbeddingModelOutput reports the outcome of a synchronous pull.
// No progress events are surfaced — the handler blocks until Ollama
// returns the terminating success status and then returns this struct.
// For long pulls override the per-tool timeout with timeout_seconds at
// the MCP client layer or via your MCP server's --timeout flag.
type PullEmbeddingModelOutput struct {
	CommonOutput
	Name string `json:"name"`
	// Server is the resolved Ollama base URL the pull ran against.
	Server string `json:"server"`
	// AlreadyPulled is true when the model was found in Ollama's local
	// list BEFORE the pull attempt — a no-op shortcut that completes
	// in sub-second time.
	AlreadyPulled bool `json:"already_pulled"`
	// DurationSeconds is the wall-clock pull duration (0 when AlreadyPulled).
	DurationSeconds float64 `json:"duration_seconds"`
	// TotalBytes is the sum of layer-Total values seen in progress
	// events. Approximate (zero when the model arrived as already-pulled,
	// or when Ollama's events lacked Total fields).
	TotalBytes int64 `json:"total_bytes,omitempty"`
}

func (h *handlers) pullEmbeddingModelHandler(ctx context.Context, _ *mcp.CallToolRequest, in PullEmbeddingModelInput) (*mcp.CallToolResult, PullEmbeddingModelOutput, error) {
	if in.Name == "" {
		return nil, PullEmbeddingModelOutput{}, errors.New("name is required")
	}
	server := in.EmbeddingServer
	if server == "" {
		server = h.defaultEmbeddingServer
	}
	out := PullEmbeddingModelOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Name:         in.Name,
		Server:       server,
	}

	oll := embed.NewOllama(server, "")

	// Shortcut: if the model is already pulled, we're done.
	local, err := oll.ListLocal(ctx)
	if err == nil {
		bareWant := embed.BareName(in.Name)
		for _, m := range local {
			if embed.BareName(m.Name) == bareWant {
				out.AlreadyPulled = true
				return nil, out, nil
			}
		}
	}
	// If ListLocal errored, fall through to the pull anyway — Pull will
	// surface a clean error if Ollama is unreachable.

	var totalBytes int64
	start := time.Now()
	err = oll.Pull(ctx, in.Name, func(p embed.PullProgress) {
		// Approximate total = sum of unique layer totals; Ollama may
		// re-report the same layer multiple times as it streams, but
		// each layer's Total is stable per-layer. We over-count if
		// progress events span layer boundaries — acceptable noise.
		if p.Total > 0 && p.Total > totalBytes {
			totalBytes = p.Total
		}
	})
	if err != nil {
		return nil, out, err
	}
	out.DurationSeconds = time.Since(start).Seconds()
	out.TotalBytes = totalBytes
	return nil, out, nil
}
