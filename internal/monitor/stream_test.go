package monitor

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- eventBus unit tests ---

func TestEventBus_DeliversToAllSubscribers(t *testing.T) {
	bus := newEventBus()
	ctx := t.Context()

	ch1, _ := bus.Subscribe(ctx)
	ch2, _ := bus.Subscribe(ctx)
	if got := bus.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", got)
	}

	bus.Publish(streamEvent{Kind: "activity", Data: "hello"})

	for i, ch := range []<-chan streamEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Kind != "activity" || got.Data != "hello" {
				t.Errorf("subscriber %d got %+v, want kind=activity data=hello", i, got)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d never received event", i)
		}
	}
}

func TestEventBus_CancelClosesChannel(t *testing.T) {
	bus := newEventBus()
	ctx := t.Context()

	ch, cancel := bus.Subscribe(ctx)
	cancel()
	// Wait for the watcher goroutine to clean up. The channel close is
	// the signal — receive must return ok=false.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if ok {
				continue // bus may have published in flight; keep draining
			}
			if got := bus.SubscriberCount(); got != 0 {
				t.Errorf("SubscriberCount after cancel = %d, want 0", got)
			}
			return
		case <-deadline:
			t.Fatal("channel never closed after cancel")
		}
	}
}

func TestEventBus_DropsOldestOnSlowSubscriber(t *testing.T) {
	bus := newEventBus()
	ctx := t.Context()

	ch, _ := bus.Subscribe(ctx)

	// Flood the bus past the per-subscriber buffer. The slow subscriber
	// (us) doesn't drain — so once the buffer fills, the OLDEST queued
	// item drops in favour of every new Publish.
	const flood = subBufferSize + 10
	for i := range flood {
		bus.Publish(streamEvent{Kind: "activity", Data: i})
	}

	// Drain. We should get exactly subBufferSize items, and the last
	// item we see must be the most-recent published (flood-1) — the
	// drop-oldest contract.
	var got []int
	for {
		select {
		case ev := <-ch:
			got = append(got, ev.Data.(int))
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if len(got) != subBufferSize {
		t.Errorf("received %d events, want %d", len(got), subBufferSize)
	}
	if got[len(got)-1] != flood-1 {
		t.Errorf("last event = %d, want %d (drop-oldest contract violated)", got[len(got)-1], flood-1)
	}
	// First item should be flood - subBufferSize (we dropped 0..9).
	if got[0] != flood-subBufferSize {
		t.Errorf("first event = %d, want %d", got[0], flood-subBufferSize)
	}
}

func TestEventBus_PublishDoesNotBlockOnFullSubscriber(t *testing.T) {
	bus := newEventBus()
	ctx := t.Context()
	bus.Subscribe(ctx) // never drain

	// Publish more than the buffer in a tight loop. Any blocking
	// behaviour would deadlock the test.
	done := make(chan struct{})
	go func() {
		for i := range 1000 {
			bus.Publish(streamEvent{Kind: "k", Data: i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish appears to block on a slow subscriber")
	}
}

// --- Collector listener fan-out ---

func TestCollector_AddListener_FiresPerRecord(t *testing.T) {
	c := NewCollector()
	var mu sync.Mutex
	var got []CallRecord
	c.AddListener(func(r CallRecord) {
		mu.Lock()
		got = append(got, r)
		mu.Unlock()
	})
	c.Record("search", 3*time.Millisecond, OutcomeOK, "", 5)
	c.Record("stats", 1*time.Millisecond, OutcomeError, "boom", 0)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("listener received %d records, want 2", len(got))
	}
	if got[0].Tool != "search" || got[0].Count != 5 {
		t.Errorf("first record = %+v", got[0])
	}
	if got[1].Tool != "stats" || got[1].Outcome != OutcomeError {
		t.Errorf("second record = %+v", got[1])
	}
}

func TestCollector_AddListener_IgnoresNil(t *testing.T) {
	c := NewCollector()
	c.AddListener(nil) // must not panic and must not register
	c.Record("search", time.Millisecond, OutcomeOK, "", 0)
	// No assert beyond "didn't panic" — Record drops past a nil listener
	// silently and any panic would crash the test.
}

// --- SSE handler integration ---

// streamHelper boots an httptest server bound to a real Server and
// returns the SSE reader, the underlying body Close, and the Server.
// Tests use these to assert framing.
func streamHelper(t *testing.T) (*bufio.Reader, func(), *Server, *Collector) {
	t.Helper()
	coll := NewCollector()
	s := NewServer(Config{
		Version:   "test-1.0.0",
		Mode:      "mcp-stdio",
		Collector: coll,
	})
	mux, err := s.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/stream") // nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	return bufio.NewReader(resp.Body), func() { _ = resp.Body.Close() }, s, coll
}

// readNextFrame reads SSE lines until a blank line terminates the
// frame, returning (event, data). Trims the "event: " / "data: "
// prefixes.
func readNextFrame(t *testing.T, r *bufio.Reader) (event, data string) {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			return
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func TestStream_InitialHeartbeat(t *testing.T) {
	r, closer, _, _ := streamHelper(t)
	defer closer()

	kind, data := readNextFrame(t, r)
	if kind != "heartbeat" {
		t.Errorf("first frame kind = %q, want heartbeat", kind)
	}
	if !strings.Contains(data, `"overview"`) || !strings.Contains(data, `"cache"`) || !strings.Contains(data, `"activity"`) {
		t.Errorf("heartbeat data missing one of overview/cache/activity: %s", data)
	}
}

func TestStream_ActivityEventOnRecord(t *testing.T) {
	r, closer, s, coll := streamHelper(t)
	defer closer()

	// Eat the initial heartbeat.
	if k, _ := readNextFrame(t, r); k != "heartbeat" {
		t.Fatalf("expected initial heartbeat, got %q", k)
	}
	// Wait until the bus subscriber has registered — the handler
	// subscribes AFTER writing the initial heartbeat, and the test
	// client's read above doesn't synchronise with that. Spin briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.bus.SubscriberCount() > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if s.bus.SubscriberCount() == 0 {
		t.Fatal("subscriber never registered with bus")
	}

	coll.Record("search", 2*time.Millisecond, OutcomeOK, "", 7)

	// Wait for an activity frame (heartbeats may interleave at 1 s,
	// so loop until we see the right kind or time out).
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		kind, data := readNextFrame(t, r)
		if kind == "activity" {
			if !strings.Contains(data, `"tool":"search"`) {
				t.Errorf("activity data missing tool=search: %s", data)
			}
			if !strings.Contains(data, `"count":7`) {
				t.Errorf("activity data missing count=7: %s", data)
			}
			return
		}
	}
	t.Fatal("never received an activity frame")
}

func TestStream_ClosesOnClientDisconnect(t *testing.T) {
	r, closer, s, _ := streamHelper(t)

	// Wait for subscriber to register.
	if _, _ = readNextFrame(t, r); s.bus.SubscriberCount() == 0 {
		// Heartbeat consumed; subscriber should be live now.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && s.bus.SubscriberCount() == 0 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	if s.bus.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount before close = %d, want 1", s.bus.SubscriberCount())
	}
	closer()
	// Cleanup is async (the handler exits, the deferred cancel fires,
	// the watcher goroutine deregisters). Poll briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.bus.SubscriberCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("SubscriberCount after close = %d, want 0", s.bus.SubscriberCount())
}
