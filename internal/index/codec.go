package index

import (
	"bytes"
	"encoding/gob"
	"runtime"
	"time"
)

// decodeTimeout bounds the wall-clock budget of a single decodeEntry
// call. The gob package has no public size / depth caps and is
// historically prone to crafted-input allocation attacks (CVE-2022-30631
// and friends). Legitimate Entry decodes complete in microseconds —
// 500 ms is generous and tight enough to be a useful DoS guard for
// corrupted or hand-edited index files.
//
// Enforcement is via a goroutine + select-on-time.After: gob.Decode
// runs all its CPU work between Read calls, so a deadline-aware Reader
// can't terminate it externally. We accept that the goroutine keeps
// running until gob.Decode actually returns; the caller unblocks at
// the deadline. concurrentDecodeLimit caps how many such zombies can
// exist at once so a fuzz / DoS workload can't OOM the process.
const decodeTimeout = 500 * time.Millisecond

// errDecodeTimeout signals that gob.Decode exceeded decodeTimeout —
// almost certainly an adversarial / corrupted input rather than a
// legitimate one. Callers in bbolt.go treat it the same as any other
// decode error (Stats.Errors increment, drop the cached entry, treat
// as cache miss).
var errDecodeTimeout = encodingError("gob decode exceeded budget")

// errDecodeOverloaded is returned when too many decodeEntry calls are
// already in-flight with leaked gob.Decode goroutines. The caller
// treats it as a cache miss — the file will simply be re-extracted.
// Defends against the fuzz / DoS scenario where many adversarial
// inputs in quick succession would otherwise spawn unbounded zombie
// goroutines and OOM the process.
var errDecodeOverloaded = encodingError("too many concurrent decodes in flight")

// concurrentDecodeLimit bounds in-flight gob.Decode goroutines. Each
// goroutine that exceeds decodeTimeout keeps running (gob can't be
// cancelled mid-call) until its own decode finishes. With this cap,
// the worst-case memory footprint is concurrentDecodeLimit *
// maxEntryBytes plus gob's internal state per call — a few hundred
// KiB. Without the cap, the fuzzer accumulates zombies and OOMs the
// runner. NumCPU*2 leaves real cache lookups headroom even when half
// the slots are zombies.
var concurrentDecodeLimit = max(8, runtime.NumCPU()*2)

// decodeSem is a counting semaphore via buffered channel — sends
// "acquire", receives "release". Capacity is concurrentDecodeLimit.
// Package-level so the cap is shared across every decodeEntry caller
// in the process (CLI search workers + MCP handlers + tests).
var decodeSem = make(chan struct{}, concurrentDecodeLimit)

// maxEntryBytes is a soft cap on the encoded form of one Entry. Pathological
// frontmatter or absurdly long lists could otherwise bloat the on-disk file.
// Above this we drop the Put and increment Stats.Errors — a cache miss is
// always recoverable, a runaway DB is not.
const maxEntryBytes = 256 * 1024

// gob registry init: the values inside Entry.Extra reach gob via
// `interface{}`, so concrete types must be registered before encode/decode
// or gob refuses to round-trip them. The set below covers everything
// today's content.ContentType.Attributes implementations emit.
//
// Adding a new content-type attribute that introduces a new concrete type
// (custom struct, etc.) requires registering it here. The
// codec_test.go round-trip suite is the canary.
func init() {
	gob.Register(time.Time{})
	gob.Register([]string{})
	gob.Register([]any{})
	gob.Register(map[string]any{})
}

// encodeEntry serialises an Entry to gob. Returns an error if the encoded
// form exceeds maxEntryBytes; callers treat that as a "drop, count, move
// on" condition rather than a hard failure.
func encodeEntry(e *Entry) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	if buf.Len() > maxEntryBytes {
		return nil, errEntryTooLarge
	}
	return buf.Bytes(), nil
}

// decodeEntry reverses encodeEntry. The decoded Entry's Extra map and any
// nested maps/slices are freshly allocated by gob, so the caller may pass
// the value into the CEL activation without risk of cross-call mutation.
//
// Defends against three adversarial-input shapes:
//   - Oversized inputs (> maxEntryBytes) are rejected up-front so a
//     hand-edited / corrupted index file can't tie up the decoder on
//     megabytes of garbage.
//   - Inputs that compile to slow / pathological gob decode paths are
//     bounded by a wall-clock budget (decodeTimeout) enforced via a
//     goroutine. The fuzzer (FuzzDecodeEntry) found a ~161-byte input
//     that consumed >1m of CPU before the test framework killed it;
//     the budget catches that class of failure in production too.
//   - Bursts of adversarial inputs that would otherwise accumulate
//     zombie goroutines (gob.Decode can't be cancelled mid-call, so
//     each timed-out goroutine keeps running until its decode
//     completes) are throttled by a semaphore. When the cap is hit,
//     new callers return errDecodeOverloaded immediately rather than
//     blocking — the caller treats it as a cache miss. Defends
//     against the fuzz / DoS scenario where many slow inputs in
//     quick succession would OOM the process.
func decodeEntry(b []byte) (*Entry, error) {
	if len(b) > maxEntryBytes {
		return nil, errEntryTooLarge
	}
	// Non-blocking acquire — if the cap is hit, the caller takes a
	// cache miss rather than waiting in line behind zombie decodes.
	select {
	case decodeSem <- struct{}{}:
	default:
		return nil, errDecodeOverloaded
	}
	type result struct {
		e   *Entry
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() { <-decodeSem }() // release when decode actually finishes
		var e Entry
		dec := gob.NewDecoder(bytes.NewReader(b))
		ch <- result{e: &e, err: dec.Decode(&e)}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.e, nil
	case <-time.After(decodeTimeout):
		// The goroutine keeps running (gob can't be cancelled
		// mid-call) and holds its semaphore slot until gob.Decode
		// actually returns. The caller unblocks now.
		return nil, errDecodeTimeout
	}
}

// errEntryTooLarge is internal — callers in bbolt.go convert it into a
// Stats.Errors increment and silently drop the Put.
var errEntryTooLarge = encodingError("encoded entry exceeds size cap")

type encodingError string

func (e encodingError) Error() string { return string(e) }
