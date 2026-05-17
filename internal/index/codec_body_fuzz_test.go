package index

import (
	"strings"
	"testing"
)

// FuzzDecodeBody is the body-codec counterpart of FuzzDecodeEntry.
// Bodies live in a separate bucket with a larger per-entry cap
// (bodyMaxBytes = 8 MiB vs entry's 256 KiB), but the gob-decoder
// failure mode is the same: malicious / corrupted input must never
// panic, and oversized / pathological input must be caught by the
// shared decodeTimeout + decodeSem defences.
//
// Contract: never panic. decodeBody returns either (*BodyEntry, nil)
// or (_, err) for any input.
//
// Like FuzzDecodeEntry, the fuzz input is capped at fuzzInputSizeCap
// to keep the worker subprocess from accumulating leaked gob.Decode
// goroutines (gob can't be cancelled mid-call). The production
// wrapper still handles oversized adversarial input correctly — this
// fuzz target just doesn't exercise it under the fuzzer's
// minimisation runaway.
func FuzzDecodeBody(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("not gob"))

	// A few structurally-valid seeds.
	for _, b := range []*BodyEntry{
		{Size: 1, ModTimeUnixNano: 1, Body: ""},
		{Size: 100, ModTimeUnixNano: 1700000000_000000000, CreatedUnixNano: 1, Body: "hello"},
		{Size: 5_000, ModTimeUnixNano: 1, CreatedUnixNano: 2, Body: strings.Repeat("x", 64)},
	} {
		if enc, err := encodeBody(b); err == nil && len(enc) <= fuzzInputSizeCap {
			f.Add(enc)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzInputSizeCap {
			return
		}
		_, _ = decodeBody(data)
	})
}
