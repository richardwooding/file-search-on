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

// TestCodecChunkedVectorsPersist is the regression for issue #346: a
// full-length book embedded with a 768-d model fills the 64-chunk cap,
// and the resulting Entry must encode under maxEntryBytes and round-trip
// — otherwise the Put is silently dropped and the vector re-embeds on
// every run (defeating the vector cache + the #335 warm index). The
// original 256 KiB cap rejected this ~393 KiB entry; the raised cap fits
// it. Also covers a 1024-d model (~544 KiB).
func TestCodecChunkedVectorsPersist(t *testing.T) {
	for _, dim := range []int{384, 768, 1024} {
		const chunks = 64 // defaultEmbedMaxChunks
		cv := make([][]float32, chunks)
		for i := range cv {
			v := make([]float32, dim)
			for d := range v {
				// Non-trivial mantissas so gob can't compress the floats —
				// the realistic worst case for encoded size.
				v[d] = float32(i*7+d) * 0.0137
			}
			cv[i] = v
		}
		e := &Entry{
			Size:            1234,
			ModTimeUnixNano: 5678,
			ContentType:     "epub",
			EmbedModel:      "model",
			ChunkVectors:    cv,
			Extra:           map[string]any{"title": "A Book", "author": "Someone"},
		}
		enc, err := encodeEntry(e)
		if err != nil {
			t.Fatalf("dim=%d: encodeEntry rejected a legitimate %d-chunk vector entry (#346): %v", dim, chunks, err)
		}
		out, err := decodeEntry(enc)
		if err != nil {
			t.Fatalf("dim=%d: decodeEntry: %v", dim, err)
		}
		if len(out.ChunkVectors) != chunks || len(out.ChunkVectors[chunks-1]) != dim {
			t.Fatalf("dim=%d: round-trip lost chunk vectors: got %d chunks", dim, len(out.ChunkVectors))
		}
		t.Logf("dim=%d: %d-chunk entry encoded to %d KiB (cap %d KiB)", dim, chunks, len(enc)/1024, maxEntryBytes/1024)
	}
}

// TestCodecDecodeBudgetSlowSeeds verifies decodeEntry's wrapper
// catches the known-adversarial gob inputs that send the decoder
// into multi-second CPU + multi-MB allocation paths. Both seeds
// below were discovered by FuzzDecodeEntry runs and lock in the
// regression: production must unblock within 3× decodeTimeout
// regardless of how long the leaked gob.Decode goroutine keeps
// running.
//
// These inputs are NOT in the fuzz seed corpus (testdata/fuzz/...
// was removed when fuzzInputSizeCap landed) because exercising
// them under the fuzz framework — across many mutation iterations
// — leaks enough gob.Decode goroutine memory to OOM the worker.
// Running each input once via decodeEntry, then letting the test
// process exit, is the only way to verify the wrapper protection
// without poisoning subsequent iterations.
func TestCodecDecodeBudgetSlowSeeds(t *testing.T) {
	// Bytes are gob streams that trigger an exponentially-deep type
	// descriptor compile path. The first was discovered in #100,
	// the second on 2026-05-16. Both stay under maxEntryBytes so
	// they reach gob.Decode (above that, decodeEntry rejects on
	// length up-front).
	seeds := map[string][]byte{
		"13d945203058feae": []byte("S\x7f\x03\x01\x01\x0500000\x01\xff0\x00\x01\x05\x01\x040000\x01\x04\x00\x01\x0f000000000000000\x01\x04\x00\x01\v00000000000\x01\f\x00\x01\x05Extra\x01\xff\x82\x00\x01\x040000\x01\f\x00\x00\x00'\xff\x81\x04\x01\x01\x1700000000000000000000000\x01\xff0\x00\x01\f\x01\x10\x00\x000\xff\x80\x03\n0000000000\x01\xfa\x00\x00\xfa000000\t0000000000000000000000"),
		"3e390c27c4f55cde": []byte("x\x7f\x03\x01\x01\x05Entry\x01\xff\x80\x00\x01\a\x01\x04Siz\x00\x01\x05Extra\x01\xff\x82\x00\x01\x04Hash\x01\f\x00\x01\vFingerprint\x01\x06\x00\x01\x0fEntryAttributes\x01\xff\x86\x00\x00\x00'\xff\x81\x04\x01\x01\x17map[string]interface {}\x01\xff\x82\x00\x01\f\x01\x10\x00\x00\"\xff\x85\x02\x01\x01\x13[]index.EntryRecord\x01\xff\x86\x00\x01\xff\x84\x00\x00Z\xff\x83\x03\x01\x01\vEntryRecord\x01\xff\x84\x00\x01\x05\x01\x04Name\x01\f\x00\x01\x04Size\x01\x04\x00\x01\x0fModTimeUnixNano\x01\x04\x00\x01\vContentType\x01\f\x00\x01\x05Extra\x01\xff\x82\x00\x00\x003\xff\x80\x03\nimage/jpeg\x01\x02\btaken_at\ttime.Time\x87\xffng\xff\x89\x02\x01\x02\xff\x8a\x00\x01\f\x00g"),
	}
	for name, data := range seeds {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, err := decodeEntry(data)
			elapsed := time.Since(start)
			if err == nil {
				t.Errorf("decodeEntry should have errored on adversarial seed %s; got success", name)
			}
			// Allow up to 3× the timeout for slack (CI runners, GC).
			if elapsed > 3*decodeTimeout {
				t.Errorf("decodeEntry blocked %v, want < %v (timeout=%v) on seed %s",
					elapsed, 3*decodeTimeout, decodeTimeout, name)
			}
		})
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
