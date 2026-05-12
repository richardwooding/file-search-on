package index

import (
	"testing"
	"time"
)

// FuzzDecodeEntry feeds arbitrary bytes into the gob decoder used by
// the on-disk bbolt index. gob is famously fragile against malicious
// or corrupted input — decoding adversarial bytes has historically
// led to panics on unregistered types and OOMs on huge length
// prefixes (CVE-2022-30631, CVE-2024-XXXX, etc.).
//
// The index file lives on disk and can be hand-edited or corrupted;
// we must never crash a search just because the cache file was
// tampered with. The codec's gob.RegisterName allow-list is the
// surface this exercises.
//
// Contract: never panic. decodeEntry must return either a valid
// (*Entry, nil) or (_, err) for any input.
func FuzzDecodeEntry(f *testing.F) {
	// Seeds: empty, garbage, and a couple of legit round-trips so
	// the fuzzer has structurally-valid starting points to mutate.
	f.Add([]byte{})
	f.Add([]byte("not gob"))

	for _, e := range []*Entry{
		{Size: 1, ModTimeUnixNano: 1, ContentType: ""},
		{Size: 42, ModTimeUnixNano: time.Now().UnixNano(), ContentType: "markdown", Extra: map[string]any{"word_count": int64(10)}},
		{Size: 0, ModTimeUnixNano: 0, ContentType: "image/jpeg", Extra: map[string]any{"taken_at": time.Time{}, "tags": []string{"a", "b"}}},
	} {
		if enc, err := encodeEntry(e); err == nil {
			f.Add(enc)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeEntry(data)
	})
}
