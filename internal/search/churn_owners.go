package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"github.com/richardwooding/file-search-on/internal/content"
)

// ChurnOwnerDir is the ownership profile of one directory.
type ChurnOwnerDir struct {
	Dir             string  `json:"dir"`
	Files           int     `json:"files"`
	DistinctAuthors int     `json:"distinct_authors"`
	TopAuthor       string  `json:"top_author"`
	TopAuthorShare  float64 `json:"top_author_share"` // fraction of the dir's files last touched by TopAuthor (0..1)
	TotalCommits    int64   `json:"total_commits"`
}

// ChurnOwnersResult is the directory-ownership report (issue #407).
type ChurnOwnersResult struct {
	Dirs               []ChurnOwnerDir `json:"dirs"`
	TotalFiles         int             `json:"total_files"`
	Cancelled          bool            `json:"cancelled,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
}

// ChurnOwners aggregates git authorship per directory to surface
// ownership concentration and bus-factor risk — directories effectively
// maintained by a single author. It walks with git metadata forced on
// (Options.WithGit), groups tracked files by their parent directory, and
// for each reports the distinct-author count, the dominant author + their
// share of files, and total commits. Directories with fewer than minFiles
// files are dropped (minFiles <= 0 means 1). Ranked by bus-factor risk:
// fewest authors first, then highest churn.
//
// Ownership is approximate: it keys on git_last_commit_author (the last
// committer per file), not a full blame — accurate enough to flag
// single-maintainer subtrees, not a substitute for blame-level ownership.
func ChurnOwners(ctx context.Context, opts Options, minFiles int, registry *content.Registry) (*ChurnOwnersResult, error) {
	opts.WithGit = true
	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	if opts.Expr == "" {
		opts.Expr = "is_git_tracked"
	}
	if minFiles <= 0 {
		minFiles = 1
	}

	results, walkErr := Walk(ctx, opts, registry)

	type acc struct {
		files       int
		commits     int64
		authorFiles map[string]int
	}
	byDir := map[string]*acc{}
	for _, r := range results {
		dir := filepath.Dir(r.Path)
		if r.Attrs != nil && r.Attrs.Dir != "" {
			dir = r.Attrs.Dir
		}
		a := byDir[dir]
		if a == nil {
			a = &acc{authorFiles: map[string]int{}}
			byDir[dir] = a
		}
		a.files++
		if r.Attrs != nil {
			a.commits += r.Attrs.GitCommitCount
			if author := r.Attrs.GitLastCommitAuthor; author != "" {
				a.authorFiles[author]++
			}
		}
	}

	res := &ChurnOwnersResult{Dirs: []ChurnOwnerDir{}}
	for dir, a := range byDir {
		res.TotalFiles += a.files
		if a.files < minFiles {
			continue
		}
		top, topN := "", 0
		for author, n := range a.authorFiles {
			// Deterministic tie-break: higher count wins, then name.
			if n > topN || (n == topN && author < top) {
				top, topN = author, n
			}
		}
		share := 0.0
		if a.files > 0 {
			share = float64(topN) / float64(a.files)
		}
		res.Dirs = append(res.Dirs, ChurnOwnerDir{
			Dir:             dir,
			Files:           a.files,
			DistinctAuthors: len(a.authorFiles),
			TopAuthor:       top,
			TopAuthorShare:  share,
			TotalCommits:    a.commits,
		})
	}

	// Bus-factor ranking: fewest distinct authors first (1 = highest
	// risk), then highest churn, then dir for stability.
	sort.Slice(res.Dirs, func(i, j int) bool {
		di, dj := res.Dirs[i], res.Dirs[j]
		if di.DistinctAuthors != dj.DistinctAuthors {
			return di.DistinctAuthors < dj.DistinctAuthors
		}
		if di.TotalCommits != dj.TotalCommits {
			return di.TotalCommits > dj.TotalCommits
		}
		return di.Dir < dj.Dir
	})

	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			res.Cancelled = true
			res.CancellationReason = "client_cancel"
			return res, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			res.Cancelled = true
			res.CancellationReason = "timeout"
			return res, nil
		}
		return res, walkErr
	}
	return res, nil
}
