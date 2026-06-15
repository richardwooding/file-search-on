package content

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// runWithDeadline runs fn in a goroutine and fails if it does not return
// within d. Used to catch zero-progress infinite loops in the MP4 box
// readers without hanging the whole suite (the fuzz harness passes a
// never-cancelled context, so a non-progressing loop would otherwise spin
// until the -fuzztime deadline).
func runWithDeadline(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("function did not return — suspected infinite loop on a zero-size box")
	}
}

// TestReadVisualSampleEntryChildren_ZeroSizeChildTerminates pins the fix
// for the FuzzReadMP4VideoInfo hang: a child box inside a VisualSampleEntry
// declaring size 0 ("extends to EOF") must not pin next==pos and loop
// forever.
func TestReadVisualSampleEntryChildren_ZeroSizeChildTerminates(t *testing.T) {
	// One box header with size 0 and an unrecognised type; end past it.
	data := []byte{0x00, 0x00, 0x00, 0x00, 'x', 'x', 'x', 'x'}
	runWithDeadline(t, 2*time.Second, func() {
		r := bytes.NewReader(data)
		var info videoInfo
		readVisualSampleEntryChildren(context.Background(), r, int64(len(data)), &info)
	})
}

// TestReadVideoSTSD_ZeroSizeEntryTerminates pins the sibling fix: a stsd
// sample entry with size 0 for a non-video/non-audio track type (so the
// loop continues rather than returning on the first entry) must terminate.
func TestReadVideoSTSD_ZeroSizeEntryTerminates(t *testing.T) {
	// 8-byte stsd preamble (version/flags + entry_count) then a sample
	// entry box header with size 0.
	data := []byte{
		0x00, 0x00, 0x00, 0x00, // version + flags
		0x00, 0x00, 0x00, 0x01, // entry_count = 1
		0x00, 0x00, 0x00, 0x00, 'm', 'p', '4', 's', // size-0 entry, "mp4s" (not vide/soun)
	}
	runWithDeadline(t, 2*time.Second, func() {
		r := bytes.NewReader(data)
		var info videoInfo
		_ = readVideoSTSD(context.Background(), r, int64(len(data)), "hint", &info)
	})
}
