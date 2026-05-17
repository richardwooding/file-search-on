package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// TestBuildAttributesWith_CreatedAtPopulated confirms that
// `created_at` (btime) lands on a freshly-written file on a
// modern OS / filesystem. The test is skipped on platforms /
// filesystems where btime isn't tracked — rare, but possible
// (older kernels, network-mounted filesystems).
func TestBuildAttributesWith_CreatedAtPopulated(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	mustWrite(t, path, "hello\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.CreatedAt.IsZero() {
		t.Skipf("btime not reported on this filesystem (skip rather than fail)")
	}
	// Sanity: the file we just wrote should have a btime within
	// the last minute. Generous bound to avoid flaky CI clocks.
	if time.Since(a.CreatedAt) > time.Minute {
		t.Errorf("CreatedAt=%v is more than a minute old for a fresh file", a.CreatedAt)
	}
	// IsBtimeAnomaly should be false on a freshly-written file
	// (btime == mtime, give or take a nanosecond).
	if a.IsBtimeAnomaly {
		t.Errorf("IsBtimeAnomaly true on a fresh file; CreatedAt=%v ModTime=%v", a.CreatedAt, a.ModTime)
	}
}

// TestIsBtimeAnomaly_PredicateLogic exercises the predicate logic
// directly on a synthesised FileAttributes. We can't construct the
// anomaly via os.Chtimes on macOS APFS (the kernel clamps btime to
// mtime when mtime is set to an earlier value), so the unit test
// pokes the fields directly and checks the predicate fires.
func TestIsBtimeAnomaly_PredicateLogic(t *testing.T) {
	now := time.Now().UTC()

	cases := []struct {
		name      string
		modTime   time.Time
		createdAt time.Time
		want      bool
	}{
		{
			name:      "fresh file: btime == mtime → no anomaly",
			modTime:   now,
			createdAt: now,
			want:      false,
		},
		{
			name:      "normal aging: btime < mtime → no anomaly",
			modTime:   now,
			createdAt: now.Add(-24 * time.Hour),
			want:      false,
		},
		{
			name:      "btime > mtime → anomaly (restored / copied / planted)",
			modTime:   now.Add(-24 * time.Hour),
			createdAt: now,
			want:      true,
		},
		{
			name:      "btime zero → no anomaly (filesystem doesn't track)",
			modTime:   now.Add(-24 * time.Hour),
			createdAt: time.Time{},
			want:      false,
		},
		{
			name:      "mtime zero → no anomaly (defensive)",
			modTime:   time.Time{},
			createdAt: now,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs := &celexpr.FileAttributes{ModTime: tc.modTime}
			celexpr.ApplyFileTimesForTest(attrs, tc.createdAt, time.Time{})
			if attrs.IsBtimeAnomaly != tc.want {
				t.Errorf("IsBtimeAnomaly=%v want %v; CreatedAt=%v ModTime=%v",
					attrs.IsBtimeAnomaly, tc.want, attrs.CreatedAt, tc.modTime)
			}
		})
	}
}

// TestEvaluate_CreatedAtFilter confirms the `created_at` CEL variable
// is addressable from expressions and behaves like the other
// timestamp variables.
func TestEvaluate_CreatedAtFilter(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	mustWrite(t, path, "hi\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.CreatedAt.IsZero() {
		t.Skipf("btime not reported on this filesystem")
	}

	// A filter pinned to a past timestamp should match (the file
	// was created AFTER the past).
	past := time.Now().Add(-365 * 24 * time.Hour).UTC().Format(time.RFC3339)
	ev, err := celexpr.New(`created_at > timestamp("` + past + `")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	match, err := ev.Evaluate(a)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !match {
		t.Errorf("expected created_at > one-year-ago to match; got false (CreatedAt=%v)", a.CreatedAt)
	}
}

// TestEvaluate_IsBtimeAnomalyFilter exercises the predicate from CEL
// using a synthesised FileAttributes (see TestIsBtimeAnomaly_PredicateLogic
// for the why-not-os.Chtimes rationale).
func TestEvaluate_IsBtimeAnomalyFilter(t *testing.T) {
	now := time.Now().UTC()
	ev, err := celexpr.New(`is_btime_anomaly`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Fresh-file attrs — predicate must NOT fire.
	fresh := &celexpr.FileAttributes{ModTime: now}
	celexpr.ApplyFileTimesForTest(fresh, now, time.Time{})
	if match, err := ev.Evaluate(fresh); err != nil || match {
		t.Errorf("is_btime_anomaly fired on fresh file: match=%v err=%v", match, err)
	}

	// Anomaly attrs (btime > mtime) — predicate must fire.
	anomaly := &celexpr.FileAttributes{ModTime: now.Add(-time.Hour)}
	celexpr.ApplyFileTimesForTest(anomaly, now, time.Time{})
	match, err := ev.Evaluate(anomaly)
	if err != nil {
		t.Fatalf("evaluate anomaly: %v", err)
	}
	if !match {
		t.Errorf("is_btime_anomaly did NOT fire for CreatedAt=%v > ModTime=%v",
			anomaly.CreatedAt, anomaly.ModTime)
	}
}
