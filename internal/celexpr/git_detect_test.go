package celexpr

import "testing"

func TestNeedsGit(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		want  bool
	}{
		{"empty", []string{}, false},
		{"all empty strings", []string{"", "", ""}, false},
		{"plain non-git expr", []string{"is_source && language == \"go\""}, false},
		{"sort by size", []string{"is_source", "size", ""}, false},

		// Single-part hits — one for each git attribute.
		{"expr references commit time", []string{"git_last_commit_time > timestamp(\"2026-05-01T00:00:00Z\")"}, true},
		{"expr references author", []string{"git_last_commit_author == \"Alice\""}, true},
		{"expr references subject", []string{"git_last_commit_subject.startsWith(\"fix:\")"}, true},
		{"expr references first_seen", []string{"git_first_seen > timestamp(\"2026-01-01T00:00:00Z\")"}, true},
		{"expr references commit_count", []string{"git_commit_count > 50"}, true},
		{"expr references is_git_tracked", []string{"is_git_tracked && is_source"}, true},
		{"expr references is_git_ignored", []string{"is_git_ignored"}, true},

		// Sort key alone.
		{"sort by git_commit_count", []string{"", "git_commit_count", ""}, true},
		{"sort by git_last_commit_time", []string{"", "git_last_commit_time", ""}, true},

		// Rank expression alone.
		{"rank uses git_commit_count", []string{"", "", "git_commit_count * 0.5 + size * 0.5"}, true},

		// Combined — expr is plain but sort triggers.
		{"plain expr + git sort", []string{"is_source", "git_last_commit_time", ""}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsGit(tc.parts...); got != tc.want {
				t.Errorf("NeedsGit(%v) = %v, want %v", tc.parts, got, tc.want)
			}
		})
	}
}

// TestNeedsGit_FalsePositiveAcceptable documents the known limitation:
// a body-content filter that happens to embed a git attribute name as
// a literal string will trigger auto-enable. This is deliberate —
// fixing it requires CEL-AST inspection, and the cost of the false
// positive is one unnecessary gitmeta.New pass per walk (cheap, pooled
// on the MCP path). False negatives — git filters returning empty
// silently — would be a worse UX.
func TestNeedsGit_FalsePositiveAcceptable(t *testing.T) {
	expr := `body.contains("git_commit_count")` // user is searching FOR the literal, not USING the attr
	if !NeedsGit(expr) {
		t.Errorf("NeedsGit should match the literal — documented false-positive contract")
	}
}
