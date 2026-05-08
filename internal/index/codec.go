package index

import (
	"bytes"
	"encoding/gob"
	"time"
)

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
func decodeEntry(b []byte) (*Entry, error) {
	var e Entry
	dec := gob.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// errEntryTooLarge is internal — callers in bbolt.go convert it into a
// Stats.Errors increment and silently drop the Put.
var errEntryTooLarge = encodingError("encoded entry exceeds size cap")

type encodingError string

func (e encodingError) Error() string { return string(e) }
