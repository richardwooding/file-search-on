package content

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestChatIterators_Cancellation verifies the streaming JSON iterators
// stop on a cancelled context instead of running over every element of a
// huge export (the ctx-cancellation audit gap).
func TestChatIterators_Cancellation(t *testing.T) {
	// A large top-level array and an NDJSON stream.
	arr := "[" + strings.Repeat(`{"x":1},`, 5000) + `{"x":1}]`
	ndjson := strings.Repeat(`{"x":1}`+"\n", 5001)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	count := 0
	forEachArrayElement(ctx, []byte(arr), func(json.RawMessage) bool { count++; return true })
	if count != 0 {
		t.Errorf("forEachArrayElement: processed %d elements under cancelled ctx, want 0", count)
	}

	count = 0
	forEachJSONValue(ctx, []byte(ndjson), func(json.RawMessage) bool { count++; return true })
	if count != 0 {
		t.Errorf("forEachJSONValue: processed %d values under cancelled ctx, want 0", count)
	}

	// Sanity: a live ctx processes everything.
	count = 0
	forEachArrayElement(context.Background(), []byte(arr), func(json.RawMessage) bool { count++; return true })
	if count != 5001 {
		t.Errorf("forEachArrayElement live: processed %d, want 5001", count)
	}
}
