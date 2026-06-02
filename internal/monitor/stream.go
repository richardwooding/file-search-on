package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// streamEvent is one message the SSE handler emits to a subscribed
// client. The Kind field maps 1:1 to the EventSource `event` field on
// the browser side; Data is JSON-serialised verbatim into the SSE frame.
// Keep Data shallow — the marshalling cost lands on the publishing path
// (Collector.Record + heartbeat) and is paid per subscriber.
type streamEvent struct {
	Kind string
	Data any
}

// subscriber is one client's view of the bus. ch is the per-client
// outbox; it MUST be bounded so a stalled browser can't back-pressure
// the publishing path (Collector.Record runs under the collector's
// mutex). When ch is full at publish time, the publisher drops the
// oldest event in the buffer to make room for the new one — see
// eventBus.Publish.
type subscriber struct {
	ch     chan streamEvent
	cancel context.CancelFunc
}

// eventBus is the monitor dashboard's read-only fan-out. A single
// publishing goroutine (the collector listener, the heartbeat ticker,
// or anything else that needs to push) calls Publish; subscribers
// receive via the channel returned from Subscribe.
//
// The bus does NOT block on slow subscribers. If a subscriber's outbox
// is full, the bus discards the oldest queued event for that
// subscriber and writes the new one in its place — preferring fresh
// state over a strict ordering guarantee. The dashboard is observability,
// not a ledger; dropping an old "in-flight=3" frame in favour of a
// newer "in-flight=4" is the right trade.
type eventBus struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newEventBus() *eventBus {
	return &eventBus{subs: make(map[*subscriber]struct{})}
}

// subBufferSize is the per-subscriber outbox capacity. Sized for ~1
// second of bursty activity (a tight loop of fast tool calls can easily
// hit 50–100 records/s) so transient lag on the SSE writer doesn't
// immediately start dropping events. Memory cost is trivial — a
// streamEvent is two words + the boxed Data interface.
const subBufferSize = 64

// Subscribe registers a new subscriber and returns its receive-only
// channel. The returned cancel function MUST be called (typically via
// defer) when the subscriber is done — it deregisters and closes the
// channel so the receive loop can exit.
//
// ctx is honoured as a parent for the cancel — if ctx is cancelled
// (client disconnect, server shutdown), the subscription is torn down
// automatically by a watcher goroutine and the channel is closed.
func (b *eventBus) Subscribe(ctx context.Context) (<-chan streamEvent, context.CancelFunc) {
	subCtx, cancel := context.WithCancel(ctx)
	s := &subscriber{
		ch:     make(chan streamEvent, subBufferSize),
		cancel: cancel,
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	// Watcher: when ctx (or the returned cancel) fires, deregister and
	// close the channel. Doing this in a goroutine keeps the public
	// Subscribe API non-blocking.
	go func() {
		<-subCtx.Done()
		b.mu.Lock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(s.ch)
		}
		b.mu.Unlock()
	}()

	return s.ch, cancel
}

// Publish fans out ev to every live subscriber. If a subscriber's
// buffer is full, the OLDEST queued event is discarded to make room
// for ev — see the eventBus type doc for the rationale.
func (b *eventBus) Publish(ev streamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.ch <- ev:
			// Fast path: room in the buffer.
		default:
			// Slow subscriber. Drop one queued event and try again. The
			// non-blocking outer select ensures we never wait on the
			// channel; the inner ones drain at most one item before a
			// retry, so this loop terminates in O(1) per Publish.
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- ev:
			default:
				// Subscriber drained itself between the read and the
				// write somehow; give up on this event for this client.
				// The next Publish will catch them up.
			}
		}
	}
}

// SubscriberCount returns the current live subscriber count. Exposed
// for tests and dashboard self-reporting; not part of the SSE protocol.
func (b *eventBus) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// heartbeatInterval is how often the SSE handler emits a snapshot
// frame carrying overview + cache + activity. 1 s halves the latency
// floor of the prior 2 s poll loop while staying cheap on a localhost
// connection. Per-tool-call activity events ride independently —
// they're emitted within ~milliseconds of Collector.Record.
const heartbeatInterval = 1 * time.Second

// handleStream serves /api/stream as an SSE channel. Two streams of
// events flow to each subscribed client:
//
//   - "activity"  — one per Collector.Record, carrying the CallRecord.
//     Latency from tool-call completion to client receipt is bounded by
//     the bus's per-subscriber buffer (drop-oldest at subBufferSize).
//   - "heartbeat" — one every heartbeatInterval, carrying current
//     overview + cache + activity snapshots. Replaces the dashboard's
//     2 s panel poll for the three live-changing sections; capabilities
//     and peers continue to be polled (they change rarely).
//
// The handler exits when ctx is cancelled (client disconnect, server
// shutdown) OR when a write to the response fails (client closed mid-
// frame). The non-blocking bus design means a stalled writer here
// cannot back-pressure Collector.Record.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx etc.). Loopback-only today, but
	// the header is cheap and prevents a footgun if someone reverse-
	// proxies the dashboard later.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, cancel := s.bus.Subscribe(r.Context())
	defer cancel()

	// Initial heartbeat so the client paints the dashboard immediately
	// without waiting up to heartbeatInterval for the first tick.
	if !writeSSEEvent(w, flusher, "heartbeat", s.heartbeatPayload()) {
		return
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				// Bus closed our channel — ctx must have fired the
				// watcher goroutine. Exit cleanly.
				return
			}
			if !writeSSEEvent(w, flusher, ev.Kind, ev.Data) {
				return
			}
		case <-ticker.C:
			if !writeSSEEvent(w, flusher, "heartbeat", s.heartbeatPayload()) {
				return
			}
		}
	}
}

// heartbeatPayload bundles the three live-changing snapshots into one
// frame so the UI updates them atomically on each tick.
func (s *Server) heartbeatPayload() map[string]any {
	return map[string]any{
		"overview": s.overviewPayload(),
		"cache":    s.cachePayload(),
		"activity": s.activityPayload(),
	}
}

// writeSSEEvent encodes data as JSON and writes one SSE frame. Returns
// false on any write or flush error — the caller exits the handler in
// that case, which lets ctx cleanup deregister the subscriber.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, kind string, data any) bool {
	payload, err := json.Marshal(data)
	if err != nil {
		// Marshalling failure is a programmer error in the payload
		// helpers — skip the frame rather than killing the stream.
		// Tests cover the happy path; misshapen data here would show
		// up in CI long before a user sees it.
		return true
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
