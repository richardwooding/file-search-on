package index

import (
	"testing"
	"time"
)

// fuzzInputSizeCap excludes inputs above this size from the fuzz
// target. Known-adversarial gob inputs that send the decoder into
// minute-scale CPU + multi-MB allocation paths are around 160 bytes
// (see TestCodecDecodeBudgetSlowSeeds for the canonical examples).
// The decodeEntry wrapper's 500ms timeout protects production from
// these — the caller unblocks, but the gob.Decode goroutine keeps
// allocating until it finishes naturally.
//
// Under the fuzz framework, many iterations leak many goroutines
// (gob can't be cancelled). Within a worker subprocess, peak RSS
// climbs without bound; CI's 5-minute scheduled run eventually
// OOMs the worker (signal: killed) and the workflow goes red.
// Capping the fuzz input at 128 bytes excludes the known-slow
// class while still letting the fuzzer explore tiny / malformed /
// truncated inputs that exercise other panic classes (unregistered
// types, recursive structure handling, etc.). The slow seeds are
// still exercised — via TestCodecDecodeBudgetSlowSeeds, which
// runs them through decodeEntry once each, verifies the timeout
// catches them, and lets the worker process exit cleanly.
const fuzzInputSizeCap = 128

// FuzzDecodeEntry feeds arbitrary bytes into the gob decoder used by
// the on-disk bbolt index. gob is famously fragile against malicious
// or corrupted input — decoding adversarial bytes has historically
// led to panics on unregistered types and OOMs on huge length
// prefixes (CVE-2022-30631, etc.).
//
// The index file lives on disk and can be hand-edited or corrupted;
// we must never crash a search just because the cache file was
// tampered with.
//
// Contract: never panic. decodeEntry must return either a valid
// (*Entry, nil) or (_, err) for any input.
//
// Slow inputs (~160 bytes of carefully-crafted gob type descriptors)
// are out-of-scope here — see fuzzInputSizeCap. Those are exercised
// directly via TestCodecDecodeBudgetSlowSeeds, which runs each
// known-slow seed exactly once, verifies decodeEntry returns within
// budget, and lets the worker exit before the leaked gob.Decode
// goroutines can accumulate.
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
		if enc, err := encodeEntry(e); err == nil && len(enc) <= fuzzInputSizeCap {
			f.Add(enc)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip inputs above the size cap — they belong to the
		// known-slow class that the production wrapper handles
		// correctly but that leak goroutines and OOM the fuzz
		// worker (see fuzzInputSizeCap doc).
		if len(data) > fuzzInputSizeCap {
			return
		}
		_, _ = decodeEntry(data)
	})
}
