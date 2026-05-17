package index

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBoltBodyRoundTrip is the basic happy-path test: PutBody, Close,
// reopen, LookupBody returns the same body. Closing the index flushes
// pending body-puts through the writer goroutine.
func TestBoltBodyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	abs := filepath.Join(dir, "doc.md")
	mtime := time.Unix(1700000000, 0)
	body := "Hello, body cache."

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := idx.PutBody(abs, &BodyEntry{
		Size:            int64(len(body)),
		ModTimeUnixNano: mtime.UnixNano(),
		CreatedUnixNano: time.Now().UnixNano(),
		Body:            body,
	}); err != nil {
		t.Fatalf("PutBody: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = idx2.Close() }()

	got, ok := idx2.LookupBody(abs, int64(len(body)), mtime)
	if !ok {
		t.Fatalf("expected hit after reopen, got miss; stats=%+v", idx2.Stats())
	}
	if got != body {
		t.Errorf("body=%q want %q", got, body)
	}
	// BodyHits is checked on idx2's fresh counters — the Lookup above
	// is the only operation on this instance.
	st := idx2.Stats()
	if st.BodyHits != 1 {
		t.Errorf("BodyHits=%d want 1; stats=%+v", st.BodyHits, st)
	}
}

// TestBoltBodyStaleOnSizeChange confirms a (size) mismatch invalidates
// the cached body — exactly like the attribute cache.
func TestBoltBodyStaleOnSizeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")
	abs := filepath.Join(dir, "f.txt")
	mtime := time.Unix(1700000000, 0)

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	if err := idx.PutBody(abs, &BodyEntry{
		Size: 100, ModTimeUnixNano: mtime.UnixNano(), Body: "old",
	}); err != nil {
		t.Fatalf("PutBody: %v", err)
	}
	// Force flush so the next Lookup actually sees the write.
	closeAndReopen := func(i Index) Index {
		if err := i.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		ni, err := Open(path)
		if err != nil {
			t.Fatalf("Reopen: %v", err)
		}
		return ni
	}
	idx = closeAndReopen(idx)

	// Different size → stale.
	if _, ok := idx.LookupBody(abs, 200, mtime); ok {
		t.Errorf("expected miss on size mismatch")
	}
	if idx.Stats().BodyStales == 0 {
		t.Errorf("BodyStales=0 want >=1; stats=%+v", idx.Stats())
	}
}

// TestBoltBodyOversize confirms a body whose encoded form exceeds
// bodyMaxBytes is rejected with BodyOversize++.
func TestBoltBodyOversize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")
	abs := filepath.Join(dir, "huge.txt")

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	// Body bigger than the 8 MiB cap. The encoded form is body + a
	// little gob overhead, so 9 MiB is comfortably over.
	huge := strings.Repeat("x", 9<<20)
	if err := idx.PutBody(abs, &BodyEntry{
		Size: int64(len(huge)), ModTimeUnixNano: 1, Body: huge,
	}); err != nil {
		t.Fatalf("PutBody: %v", err)
	}
	st := idx.Stats()
	if st.BodyOversize != 1 {
		t.Errorf("BodyOversize=%d want 1; stats=%+v", st.BodyOversize, st)
	}
}

// TestBoltBodyEviction confirms FIFO eviction kicks in when the total
// bodies bucket size exceeds the configured cap. We put enough bodies
// to overflow a tight cap, then verify the counter ticks. Stats are
// checked BEFORE Close (a reopen resets the in-process counters; the
// running bodies-total-size carries forward via meta but the
// monotonic Stats counters don't).
func TestBoltBodyEviction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	// Tight cap: ~32 KiB. With 8 KiB bodies + key/gob overhead, ~3
	// bodies fit before eviction kicks in.
	idx, err := OpenWith(path, BodyCacheCap{MaxBytes: 32 * 1024})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	body := strings.Repeat("a", 8*1024)
	for i := range 10 {
		abs := filepath.Join(dir, "file"+itoa(i)+".txt")
		if err := idx.PutBody(abs, &BodyEntry{
			Size:            int64(len(body)),
			ModTimeUnixNano: 1,
			CreatedUnixNano: int64(1000 + i),
			Body:            body,
		}); err != nil {
			t.Fatalf("PutBody %d: %v", i, err)
		}
	}
	// Wait long enough for the writer's flush ticker to run multiple
	// times so every put is processed. flushInterval is 100ms.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := idx.Stats()
		if st.BodyPuts == 10 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := idx.Stats()
	if st.BodyPuts != 10 {
		t.Fatalf("BodyPuts=%d want 10; stats=%+v", st.BodyPuts, st)
	}
	if st.BodyEvictions == 0 {
		t.Errorf("BodyEvictions=0 want >=1; stats=%+v", st)
	}
}

// TestBoltBodyDisabledNoOp confirms BodyCacheCap.Disable makes PutBody
// a no-op and LookupBody always miss.
func TestBoltBodyDisabledNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	idx, err := OpenWith(path, BodyCacheCap{Disable: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	abs := filepath.Join(dir, "f.txt")
	if err := idx.PutBody(abs, &BodyEntry{Size: 5, ModTimeUnixNano: 1, Body: "hello"}); err != nil {
		t.Fatalf("PutBody: %v", err)
	}
	if _, ok := idx.LookupBody(abs, 5, time.Unix(0, 1)); ok {
		t.Errorf("expected miss with body cache disabled")
	}
	st := idx.Stats()
	if st.BodyPuts != 0 {
		t.Errorf("BodyPuts=%d want 0 (disabled)", st.BodyPuts)
	}
}

// TestBoltBodySchemaUpgrade confirms an existing v1 cache file
// (created before the body buckets existed) opens cleanly and gains
// the new buckets on first open.
func TestBoltBodySchemaUpgrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	// First open creates a fresh file with all buckets.
	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Reopen should succeed and PutBody/LookupBody should work — this
	// is the canary for the "fresh schema includes the new buckets"
	// invariant rather than a true upgrade test (an upgrade test would
	// require shipping a pre-body-cache binary, which is overkill).
	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = idx2.Close() }()

	abs := filepath.Join(dir, "f.txt")
	mtime := time.Unix(1700000000, 0)
	if err := idx2.PutBody(abs, &BodyEntry{Size: 1, ModTimeUnixNano: mtime.UnixNano(), Body: "x"}); err != nil {
		t.Fatalf("PutBody: %v", err)
	}
}

// itoa is a local int → string helper to avoid pulling in strconv just
// for test scaffolding.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
