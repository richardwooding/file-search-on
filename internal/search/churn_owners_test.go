package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/gitmeta"

	"github.com/richardwooding/file-search-on/internal/search"
)

// commitAs writes a file under root (relative path), then commits it
// authored by the given name/email.
func commitAs(t *testing.T, root, rel, body, name, email string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	mustGit(t, root, "add", rel)
	mustGit(t, root, "-c", "user.name="+name, "-c", "user.email="+email,
		"commit", "-q", "-m", "add "+rel)
}

func TestChurnOwners(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	root := t.TempDir()
	mustGit(t, root, "init", "-q", "-b", "main")
	mustGit(t, root, "config", "commit.gpgsign", "false")

	// solo/ — every file last committed by Alice → single-maintainer.
	commitAs(t, root, "solo/a.md", "# A\n", "Alice", "alice@example.com")
	commitAs(t, root, "solo/b.md", "# B\n", "Alice", "alice@example.com")
	// shared/ — Alice + Bob → two authors.
	commitAs(t, root, "shared/c.md", "# C\n", "Alice", "alice@example.com")
	commitAs(t, root, "shared/d.md", "# D\n", "Bob", "bob@example.com")

	res, err := search.ChurnOwners(context.Background(), search.Options{
		Root:    root,
		Workers: 1,
	}, 1, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("ChurnOwners: %v", err)
	}

	dirs := map[string]search.ChurnOwnerDir{}
	for _, d := range res.Dirs {
		dirs[filepath.Base(d.Dir)] = d
	}

	solo, ok := dirs["solo"]
	if !ok {
		t.Fatalf("solo dir missing from %+v", res.Dirs)
	}
	if solo.DistinctAuthors != 1 || solo.TopAuthor != "Alice" || solo.Files != 2 {
		t.Errorf("solo = %+v, want 1 author Alice over 2 files", solo)
	}
	if solo.TopAuthorShare != 1.0 {
		t.Errorf("solo TopAuthorShare = %v, want 1.0", solo.TopAuthorShare)
	}

	shared, ok := dirs["shared"]
	if !ok {
		t.Fatalf("shared dir missing from %+v", res.Dirs)
	}
	if shared.DistinctAuthors != 2 {
		t.Errorf("shared distinct_authors = %d, want 2", shared.DistinctAuthors)
	}
	if shared.TopAuthorShare != 0.5 {
		t.Errorf("shared TopAuthorShare = %v, want 0.5", shared.TopAuthorShare)
	}

	// Ranking: single-author dirs come first.
	if len(res.Dirs) >= 2 && res.Dirs[0].DistinctAuthors > res.Dirs[len(res.Dirs)-1].DistinctAuthors {
		t.Errorf("dirs not ranked by ascending distinct_authors: %+v", res.Dirs)
	}
}

func TestChurnOwners_MinFiles(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	root := t.TempDir()
	mustGit(t, root, "init", "-q", "-b", "main")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	commitAs(t, root, "big/a.md", "# A\n", "Alice", "alice@example.com")
	commitAs(t, root, "big/b.md", "# B\n", "Alice", "alice@example.com")
	commitAs(t, root, "small/c.md", "# C\n", "Bob", "bob@example.com")

	res, err := search.ChurnOwners(context.Background(), search.Options{Root: root, Workers: 1}, 2, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("ChurnOwners: %v", err)
	}
	for _, d := range res.Dirs {
		if d.Files < 2 {
			t.Errorf("min_files=2 should drop %s (%d files)", d.Dir, d.Files)
		}
	}
	// total_files still counts everything walked, including dropped dirs.
	if res.TotalFiles != 3 {
		t.Errorf("TotalFiles = %d, want 3", res.TotalFiles)
	}
}
