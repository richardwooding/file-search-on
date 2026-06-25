package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStartProfiling exercises the root --cpuprofile / --memprofile /
// --trace plumbing: setting the flags should produce non-empty profile
// files once the returned stop closure runs. It mutates the global CLI,
// so it must not run in parallel with other tests that read CLI.
func TestStartProfiling(t *testing.T) {
	dir := t.TempDir()
	cpu := filepath.Join(dir, "cpu.prof")
	mem := filepath.Join(dir, "mem.prof")
	trc := filepath.Join(dir, "trace.out")

	saveCPU, saveMem, saveTrace := CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf
	t.Cleanup(func() {
		CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf = saveCPU, saveMem, saveTrace
	})
	CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf = cpu, mem, trc

	stop, err := startProfiling()
	if err != nil {
		t.Fatalf("startProfiling: %v", err)
	}

	// Burn a little CPU so the CPU profile has at least one sample.
	sum := 0
	for i := range 5_000_000 {
		sum += i % 7
	}
	_ = sum

	stop()

	// pprof CPU + heap profiles are gzipped protobufs (gzip magic 1f 8b);
	// the trace file just needs to be non-empty. Checking size + magic is
	// enough — we don't re-implement profile parsing here.
	for _, p := range []string{cpu, mem} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
			t.Errorf("%s: want non-empty gzip pprof, got %d bytes (prefix %x)", p, len(b), b[:min(2, len(b))])
		}
	}
	if fi, err := os.Stat(trc); err != nil || fi.Size() == 0 {
		t.Errorf("trace file %s: err=%v size=%d, want non-empty", trc, err, sizeOf(fi))
	}
}

// TestStartProfiling_Disabled is the no-op path: with no flags set,
// startProfiling returns a usable stop closure and writes nothing.
func TestStartProfiling_Disabled(t *testing.T) {
	saveCPU, saveMem, saveTrace := CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf
	t.Cleanup(func() {
		CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf = saveCPU, saveMem, saveTrace
	})
	CLI.CPUProfile, CLI.MemProfile, CLI.TraceProf = "", "", ""

	stop, err := startProfiling()
	if err != nil {
		t.Fatalf("startProfiling: %v", err)
	}
	stop() // must be safe to call with nothing started
}

// TestStartProfiling_BadPath surfaces an error (not a panic) when a
// profile path is unwritable.
func TestStartProfiling_BadPath(t *testing.T) {
	saveCPU := CLI.CPUProfile
	t.Cleanup(func() { CLI.CPUProfile = saveCPU })
	CLI.CPUProfile = filepath.Join(t.TempDir(), "no-such-dir", "cpu.prof")

	if _, err := startProfiling(); err == nil {
		t.Fatal("startProfiling with unwritable cpuprofile path: want error, got nil")
	}
}

func sizeOf(fi os.FileInfo) int64 {
	if fi == nil {
		return -1
	}
	return fi.Size()
}
