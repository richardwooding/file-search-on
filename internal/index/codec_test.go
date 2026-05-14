package index

import (
	"strings"
	"testing"
	"time"
)

// TestCodecRoundTripPreservesTypes is the regression-defence test that
// guards the gob registration set. JSON would mangle int64→float64; this
// test fails loudly if anyone replaces gob with JSON or forgets to
// register a new concrete type that ContentType.Attributes returns.
func TestCodecRoundTripPreservesTypes(t *testing.T) {
	taken := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	in := &Entry{
		Size:            123,
		ModTimeUnixNano: 456789,
		ContentType:     "image/jpeg",
		Extra: map[string]any{
			"title":         "Hello",
			"word_count":    int64(42),
			"page_count":    int64(7),
			"gps_lat":       37.7749,
			"focal_length":  50.0,
			"is_hdr":        true,
			"taken_at":      taken,
			"tags":          []string{"a", "b", "c"},
			"frontmatter":   map[string]any{"slug": "hi", "draft": false, "n": int64(3)},
			"top_level":     []string{"only-top"},
		},
	}
	enc, err := encodeEntry(in)
	if err != nil {
		t.Fatalf("encodeEntry: %v", err)
	}
	out, err := decodeEntry(enc)
	if err != nil {
		t.Fatalf("decodeEntry: %v", err)
	}
	if out.Size != in.Size || out.ModTimeUnixNano != in.ModTimeUnixNano || out.ContentType != in.ContentType {
		t.Fatalf("scalar fields drifted: in=%+v out=%+v", in, out)
	}

	if v, ok := out.Extra["title"].(string); !ok || v != "Hello" {
		t.Errorf("title: got %#v, want \"Hello\"", out.Extra["title"])
	}
	// Critical: int64 stays int64 (the JSON mistake we are guarding against).
	if v, ok := out.Extra["word_count"].(int64); !ok || v != 42 {
		t.Errorf("word_count: got %#v, want int64(42)", out.Extra["word_count"])
	}
	if v, ok := out.Extra["page_count"].(int64); !ok || v != 7 {
		t.Errorf("page_count: got %#v, want int64(7)", out.Extra["page_count"])
	}
	// And float64 stays float64.
	if v, ok := out.Extra["gps_lat"].(float64); !ok || v != 37.7749 {
		t.Errorf("gps_lat: got %#v, want 37.7749", out.Extra["gps_lat"])
	}
	if v, ok := out.Extra["focal_length"].(float64); !ok || v != 50.0 {
		t.Errorf("focal_length: got %#v, want 50.0", out.Extra["focal_length"])
	}
	if v, ok := out.Extra["is_hdr"].(bool); !ok || !v {
		t.Errorf("is_hdr: got %#v, want true", out.Extra["is_hdr"])
	}
	if v, ok := out.Extra["taken_at"].(time.Time); !ok || !v.Equal(taken) {
		t.Errorf("taken_at: got %#v, want %v", out.Extra["taken_at"], taken)
	}
	if v, ok := out.Extra["tags"].([]string); !ok || len(v) != 3 || v[0] != "a" {
		t.Errorf("tags: got %#v, want [a b c]", out.Extra["tags"])
	}

	fm, ok := out.Extra["frontmatter"].(map[string]any)
	if !ok {
		t.Fatalf("frontmatter: got %T, want map[string]any", out.Extra["frontmatter"])
	}
	if v, ok := fm["slug"].(string); !ok || v != "hi" {
		t.Errorf("frontmatter.slug: got %#v, want \"hi\"", fm["slug"])
	}
	if v, ok := fm["n"].(int64); !ok || v != 3 {
		t.Errorf("frontmatter.n: got %#v, want int64(3)", fm["n"])
	}
}

func TestCodecRejectsOversize(t *testing.T) {
	// Build a payload guaranteed to exceed the cap.
	huge := strings.Repeat("x", maxEntryBytes+1)
	e := &Entry{
		Size:            1,
		ModTimeUnixNano: 1,
		ContentType:     "text",
		Extra:           map[string]any{"title": huge},
	}
	if _, err := encodeEntry(e); err == nil {
		t.Fatalf("expected encodeEntry to reject oversize payload")
	}
}

// TestCodecDecodeBudgetCancelled verifies that adversarial inputs known
// to push gob.Decode into a multi-second compile path are bounded by
// decodeTimeout. The committed regression seed (testdata/fuzz/
// FuzzDecodeEntry/13d945203058feae) is the same shape that hung the
// fuzz workflow before the goroutine + select-on-time.After guard
// landed in #100. This test re-runs that input directly via
// decodeEntry and asserts the call returns within a generous window —
// the goroutine is allowed to keep running afterwards, but the caller
// must unblock.
func TestCodecDecodeBudgetCancelled(t *testing.T) {
	// Bytes copied from testdata/fuzz/FuzzDecodeEntry/13d945203058feae.
	// Keep in sync with that seed; it's the canonical "slow input".
	slow := []byte("S\x7f\x03\x01\x01\x0500000\x01\xff0\x00\x01\x05\x01\x040000\x01\x04\x00\x01\x0f000000000000000\x01\x04\x00\x01\v00000000000\x01\f\x00\x01\x05Extra\x01\xff\x82\x00\x01\x040000\x01\f\x00\x00\x00'\xff\x81\x04\x01\x01\x1700000000000000000000000\x01\xff0\x00\x01\f\x01\x10\x00\x000\xff\x80\x03\n0000000000\x01\xfa\x00\x00\xfa000000\t0000000000000000000000")

	start := time.Now()
	_, err := decodeEntry(slow)
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("decodeEntry should have errored on the known-adversarial seed; got success")
	}
	// Allow up to 3x the timeout for slack (CI runners, GC).
	if elapsed > 3*decodeTimeout {
		t.Errorf("decodeEntry blocked %v, want < %v (timeout=%v)", elapsed, 3*decodeTimeout, decodeTimeout)
	}
}

// TestCodecDecodeOverloaded verifies the concurrent-decode cap rejects
// excess callers when the semaphore is full. Defends against the
// fuzz-OOM scenario where zombie goroutines would otherwise accumulate.
//
// Earlier tests in this package may have spawned still-running zombie
// gob.Decode goroutines that hold semaphore slots; acquire
// non-blockingly so we don't deadlock waiting for them.
func TestCodecDecodeOverloaded(t *testing.T) {
	held := 0
	for range concurrentDecodeLimit {
		select {
		case decodeSem <- struct{}{}:
			held++
		default:
			// Slot already held by an earlier test's zombie — fine,
			// we just need the semaphore full overall.
		}
	}
	defer func() {
		for range held {
			<-decodeSem
		}
	}()

	// Any non-oversize input will do; the call should fast-fail on
	// the semaphore before hitting gob at all.
	_, err := decodeEntry([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected errDecodeOverloaded, got nil")
	}
	if err != errDecodeOverloaded {
		t.Errorf("err=%v, want errDecodeOverloaded", err)
	}
}
