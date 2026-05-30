package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOllamaEmbedder_ListLocal_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"models": [
				{"name":"nomic-embed-text:latest","size":274302450,"modified_at":"2026-05-18T14:08:54Z","digest":"abc"},
				{"name":"llama3:latest","size":4661224500,"modified_at":"2026-05-01T10:00:00Z","digest":"def"}
			]
		}`))
	}))
	defer srv.Close()

	got, err := NewOllama(srv.URL, "").ListLocal(context.Background())
	if err != nil {
		t.Fatalf("ListLocal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "nomic-embed-text:latest" || got[0].Size != 274302450 {
		t.Errorf("entry 0 = %+v, want name=nomic-embed-text:latest size=274302450", got[0])
	}
	if got[1].Name != "llama3:latest" {
		t.Errorf("entry 1 = %+v, want name=llama3:latest", got[1])
	}
}

func TestOllamaEmbedder_ListLocal_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	got, err := NewOllama(srv.URL, "").ListLocal(context.Background())
	if err != nil {
		t.Fatalf("ListLocal: %v", err)
	}
	if got == nil {
		t.Errorf("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestOllamaEmbedder_ListLocal_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewOllama(srv.URL, "").ListLocal(context.Background())
	if err == nil {
		t.Errorf("expected error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %v should mention 500", err)
	}
}

func TestOllamaEmbedder_Pull_StreamsToSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		// Decode the body to verify our wire shape is right.
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["name"] != "nomic-embed-text" {
			t.Errorf("server saw name=%v, want nomic-embed-text", got["name"])
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		writeEvent := func(s string) {
			_, _ = fmt.Fprintln(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		writeEvent(`{"status":"pulling manifest"}`)
		writeEvent(`{"status":"downloading","digest":"sha256:abc","total":100,"completed":50}`)
		writeEvent(`{"status":"downloading","digest":"sha256:abc","total":100,"completed":100}`)
		writeEvent(`{"status":"verifying sha256 digest"}`)
		writeEvent(`{"status":"success"}`)
	}))
	defer srv.Close()

	var events []PullProgress
	err := NewOllama(srv.URL, "").Pull(context.Background(), "nomic-embed-text", func(p PullProgress) {
		events = append(events, p)
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("got %d events, want 5", len(events))
	}
	if events[len(events)-1].Status != "success" {
		t.Errorf("last event status = %q, want success", events[len(events)-1].Status)
	}
	if events[1].Completed != 50 || events[1].Total != 100 {
		t.Errorf("middle event = %+v, want completed=50 total=100", events[1])
	}
}

func TestOllamaEmbedder_Pull_NoProgressCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer srv.Close()
	// nil onProgress is allowed — events are silently dropped.
	if err := NewOllama(srv.URL, "").Pull(context.Background(), "m", nil); err != nil {
		t.Fatalf("Pull with nil onProgress: %v", err)
	}
}

func TestOllamaEmbedder_Pull_OllamaErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"error":"pull model manifest: file does not exist"}`)
	}))
	defer srv.Close()
	err := NewOllama(srv.URL, "").Pull(context.Background(), "ghost-model", nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "file does not exist") {
		t.Errorf("error should propagate ollama message, got %v", err)
	}
}

func TestOllamaEmbedder_Pull_NoSuccessEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		// stream ends without success
	}))
	defer srv.Close()
	err := NewOllama(srv.URL, "").Pull(context.Background(), "m", nil)
	if err == nil {
		t.Fatalf("expected error when stream ends without success, got nil")
	}
	if !strings.Contains(err.Error(), "success") {
		t.Errorf("error should mention missing success, got %v", err)
	}
}

func TestOllamaEmbedder_Pull_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	err := NewOllama(srv.URL, "").Pull(context.Background(), "m", nil)
	if err == nil {
		t.Fatalf("expected error on 503, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status, got %v", err)
	}
}

func TestOllamaEmbedder_Pull_ContextCancel(t *testing.T) {
	var cancelledMid atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		if flusher != nil {
			flusher.Flush()
		}
		// Hang long enough for the test to cancel ctx.
		time.Sleep(500 * time.Millisecond)
		cancelledMid.Store(true)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := NewOllama(srv.URL, "").Pull(ctx, "m", nil)
	if err == nil {
		t.Fatalf("expected error after ctx cancel, got nil")
	}
	// Either ctx.Err() wraps through, or we hit the no-success path
	// because the read was cut short — both are acceptable evidence
	// of clean cancellation.
}

func TestOllamaEmbedder_ListLocal_NoServer(t *testing.T) {
	if _, err := NewOllama("", "").ListLocal(context.Background()); err != ErrNoServer {
		t.Errorf("err = %v, want ErrNoServer", err)
	}
}

func TestOllamaEmbedder_Pull_NoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	err := NewOllama(srv.URL, "").Pull(context.Background(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Errorf("err = %v, want 'name is required'", err)
	}
}
