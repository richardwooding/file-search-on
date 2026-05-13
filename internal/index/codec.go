package index

import (
	"bytes"
	"encoding/gob"
	"time"
)

// decodeTimeout bounds the wall-clock budget of a single decodeEntry
// call. The gob package has no public size / depth caps and is
// historically prone to crafted-input allocation attacks (CVE-2022-30631
// and friends). Legitimate Entry decodes complete in microseconds —
// 500 ms is generous and tight enough to be a useful DoS guard for
// corrupted or hand-edited index files. A goroutine that exceeds the
// budget keeps running until gob.Decode returns (the input is bounded
// to maxEntryBytes so it can't allocate unboundedly); the caller's
// request unblocks immediately.
const decodeTimeout = 500 * time.Millisecond

// errDecodeTimeout signals that gob.Decode exceeded decodeTimeout —
// almost certainly an adversarial / corrupted input rather than a
// legitimate one. Callers in bbolt.go treat it the same as any other
// decode error (Stats.Errors increment, drop the cached entry, treat
// as cache miss).
var errDecodeTimeout = encodingError("gob decode exceeded budget")

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
// Defends against two adversarial-input shapes:
//   - Oversized inputs (> maxEntryBytes) are rejected up-front so a
//     hand-edited / corrupted index file can't tie up the decoder on
//     megabytes of garbage.
//   - Inputs that compile to slow / pathological gob decode paths are
//     bounded by a wall-clock budget (decodeTimeout). The fuzzer
//     (FuzzDecodeEntry) found a ~161-byte input that consumed >1m of
//     CPU before the test framework killed it; the budget catches that
//     class of failure in production too.
func decodeEntry(b []byte) (*Entry, error) {
	if len(b) > maxEntryBytes {
		return nil, errEntryTooLarge
	}
	type result struct {
		e   *Entry
		err error
	}
	ch := make(chan result, 1)
	go func() {
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
		// Goroutine leaks until gob.Decode returns. Bounded by
		// the maxEntryBytes input cap above — worst case is one
		// transient memory spike, not an unbounded leak. Acceptable.
		return nil, errDecodeTimeout
	}
}

// errEntryTooLarge is internal — callers in bbolt.go convert it into a
// Stats.Errors increment and silently drop the Put.
var errEntryTooLarge = encodingError("encoded entry exceeds size cap")

type encodingError string

func (e encodingError) Error() string { return string(e) }
