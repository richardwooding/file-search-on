package embed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LocalModel is one entry from Ollama's GET /api/tags response.
type LocalModel struct {
	Name       string    `json:"name"`        // e.g. "nomic-embed-text:latest"
	Size       int64     `json:"size_bytes"`  // payload size in bytes
	ModifiedAt time.Time `json:"modified_at"` // last-modified timestamp Ollama stamps
	Digest     string    `json:"digest"`      // sha256 of the manifest
}

// PullProgress is one event from Ollama's POST /api/pull NDJSON stream.
// Ollama emits multiple events per pull: status lines ("pulling manifest",
// "downloading sha256:abc..."), layer-progress events with Completed/Total
// counters, and a terminating "status: success" event. Callers receive
// every event verbatim; throttling is Ollama's responsibility.
type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// ListLocal returns every model present on the Ollama server (chat AND
// embedding mixed — Ollama doesn't surface a classification, and we
// don't try to guess). The caller annotates which ones match the
// curated catalog via CatalogLookup.
//
// Returns an empty slice (not nil) when Ollama responds with an empty
// model list. Returns an error wrapped from one of: network failure
// reaching Ollama, non-200 status, malformed JSON.
func (o *OllamaEmbedder) ListLocal(ctx context.Context) ([]LocalModel, error) {
	if o.server == "" {
		return nil, ErrNoServer
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.server+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama at %s: %w", o.server, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d on /api/tags", resp.StatusCode)
	}
	var raw struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			ModifiedAt time.Time `json:"modified_at"`
			Digest     string    `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode /api/tags: %w", err)
	}
	out := make([]LocalModel, 0, len(raw.Models))
	for _, m := range raw.Models {
		out = append(out, LocalModel{
			Name:       m.Name,
			Size:       m.Size,
			ModifiedAt: m.ModifiedAt,
			Digest:     m.Digest,
		})
	}
	return out, nil
}

// Pull downloads the named model via POST /api/pull and streams Ollama's
// NDJSON progress events to onProgress (which may be nil to discard).
// Returns nil when Ollama emits the terminating "success" status, or
// an error from one of:
//
//   - missing config (ErrNoServer)
//   - network failure reaching Ollama
//   - non-200 HTTP status on the pull request
//   - an in-stream "error" field on a progress event (Ollama signals
//     mid-pull failures this way, e.g. unknown model)
//   - ctx cancellation (returns ctx.Err() wrapped)
//
// Cancellation aborts the streaming response read; the partial download
// is NOT lost because Ollama's pull is layer-based and resumable — a
// subsequent Pull call against the same name picks up where the
// cancelled one left off.
//
// Uses a fresh *http.Client with no timeout (separate from the
// 60s-bounded Embed client) because pulls of large models can take
// many minutes. ctx is the only deadline.
func (o *OllamaEmbedder) Pull(ctx context.Context, name string, onProgress func(PullProgress)) error {
	if o.server == "" {
		return ErrNoServer
	}
	if name == "" {
		return errors.New("embed: model name is required")
	}
	body, err := json.Marshal(map[string]any{
		"name":   name,
		"stream": true,
	})
	if err != nil {
		return fmt.Errorf("marshal pull request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.server+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	streamingClient := &http.Client{} // no timeout — pull can take minutes; ctx is the deadline
	resp, err := streamingClient.Do(req)
	if err != nil {
		return fmt.Errorf("call ollama pull at %s: %w", o.server, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d on /api/pull (model %q)", resp.StatusCode, name)
	}
	scanner := bufio.NewScanner(resp.Body)
	// Pull progress events can carry layer digests + ample status text;
	// raise the line cap so a very long Ollama line doesn't truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Ollama emits an "error" field on the same NDJSON shape when
		// a pull fails mid-stream (e.g. unknown model name). Detect
		// both that path and the regular progress path.
		var ev struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("decode pull progress %q: %w", line, err)
		}
		if ev.Error != "" {
			return fmt.Errorf("ollama pull failed: %s", ev.Error)
		}
		if onProgress != nil {
			onProgress(PullProgress{
				Status:    ev.Status,
				Digest:    ev.Digest,
				Total:     ev.Total,
				Completed: ev.Completed,
			})
		}
		if ev.Status == "success" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		// io.EOF without a "success" event would land here too.
		if errors.Is(err, io.EOF) {
			return errors.New("ollama pull ended without success status")
		}
		return fmt.Errorf("read pull stream: %w", err)
	}
	// Scanner returned cleanly (EOF) without seeing the success event.
	return errors.New("ollama pull ended without success status")
}
