// Package gitmeta resolves per-file git metadata (last-commit time +
// author + subject, first-seen, churn, tracked/ignored status) for a
// single working tree by shelling out to the system `git` binary once
// per walk.
//
// One Cache scans the entire repository up front (`git ls-files`,
// `git ls-files --others --ignored`, and a single `git log` pass
// keyed by HEAD), then answers per-path Lookup / IsTracked / IsIgnored
// in constant time. The batch architecture is dramatically cheaper
// than per-file `git log -1 -- <path>` on any non-trivial repo — a
// 10k-file tree with 5k commits costs one git invocation (~500ms),
// not 10k (~100s).
//
// Cache returns nil from New when the supplied root isn't inside a
// git working tree. Callers MUST handle nil — the search-side
// integration uses this as the "no git data; leave fields zero"
// signal.
package gitmeta

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileGitInfo carries the per-file metadata Cache resolves for every
// tracked path in a working tree. Zero values are meaningful: a fresh
// commit's CommitCount==1 and FirstSeen==LastCommitTime.
type FileGitInfo struct {
	LastCommitTime    time.Time
	LastCommitAuthor  string
	LastCommitSubject string
	FirstSeen         time.Time
	CommitCount       int
}

// Cache is the per-repository scan result. Build via New; consult via
// Lookup / IsTracked / IsIgnored. All read methods are safe for
// concurrent use (Cache is effectively immutable after construction).
type Cache struct {
	// repoRoot is git's canonical view of the working-tree root (the
	// output of `git rev-parse --show-toplevel`). On Darwin this is
	// the realpath form (e.g. /private/tmp/...) which differs from
	// the symlinked /tmp/... form a caller might pass.
	repoRoot string

	// repoRootAlt is the as-supplied form of the working-tree root,
	// pre-symlink-resolution. Used by toRel as a fallback prefix so
	// callers can pass absolute paths derived from the walk root
	// (which often retains the symlinked form on macOS) without
	// having to EvalSymlinks first.
	repoRootAlt string

	headSHA string

	// files is keyed by repo-relative forward-slash path (the form
	// `git ls-files` emits). Lookup callers pass an absolute path;
	// the conversion happens inside.
	files map[string]FileGitInfo

	// tracked / ignored are set membership: a path's presence in
	// `tracked` means it's in the git index; presence in `ignored`
	// means it's matched by .gitignore (untracked + ignored).
	tracked map[string]struct{}
	ignored map[string]struct{}
}

// RepoRoot returns the repository's top-level absolute directory (the
// output of `git rev-parse --show-toplevel`). Exposed so callers can
// rebase a walk root against it.
func (c *Cache) RepoRoot() string { return c.repoRoot }

// HeadSHA returns the repository's HEAD commit SHA at the time the
// Cache was built. Useful for invalidation when the caller persists
// Cache results across processes.
func (c *Cache) HeadSHA() string { return c.headSHA }

// New scans the git working tree containing root and returns a Cache.
// Returns nil, nil when root is not inside any git working tree
// (silent skip — the caller treats this as "no git data available").
// Returns nil, err only on hard failures (git binary missing,
// subprocess crash, ctx cancellation).
//
// The scan runs three git invocations concurrently after the initial
// rev-parse and waits for all of them. ctx propagates to each
// subprocess via exec.CommandContext, so a cancelled walk tears the
// git processes down promptly.
func New(ctx context.Context, root string) (*Cache, error) {
	repoRoot, err := repoToplevel(ctx, root)
	if err != nil {
		// Not a git repo (the common case for non-repo trees) is
		// silent. Any other error — git binary missing, permission
		// denied — also degrades to "no cache" rather than failing
		// the walk; the symptom (empty git_* attrs) is clearer than
		// an aborted search.
		return nil, nil //nolint:nilerr // silent skip is intentional
	}
	headSHA, err := revParseHead(ctx, repoRoot)
	if err != nil {
		// HEAD missing (empty repo, freshly initialised) — still a
		// valid git tree, but no commits to walk. Build an empty
		// cache so is_git_tracked / is_git_ignored still work.
		return buildEmptyHeadCache(ctx, repoRoot)
	}

	tracked, err := lsFilesTracked(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("gitmeta: ls-files: %w", err)
	}
	ignored, err := lsFilesIgnored(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("gitmeta: ls-files --ignored: %w", err)
	}
	files, err := logFiles(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("gitmeta: log: %w", err)
	}

	return &Cache{
		repoRoot:    repoRoot,
		repoRootAlt: altRoot(root, repoRoot),
		headSHA:     headSHA,
		files:       files,
		tracked:     setFromSlice(tracked),
		ignored:     setFromSlice(ignored),
	}, nil
}

// altRoot returns the user-supplied root as an absolute path when it
// differs from git's canonical view (typically the macOS /tmp →
// /private/tmp symlink case). Used by toRel as a fallback prefix.
// Returns "" when the user-supplied form matches the canonical form.
func altRoot(userRoot, canonical string) string {
	abs, err := filepath.Abs(userRoot)
	if err != nil {
		return ""
	}
	if abs == canonical {
		return ""
	}
	return abs
}

// Lookup returns git metadata for absPath. ok=false means absPath is
// not tracked by git in this working tree (either untracked, ignored,
// or outside the repo entirely). The caller should leave git_* CEL
// fields at their zero values in that case.
func (c *Cache) Lookup(absPath string) (FileGitInfo, bool) {
	if c == nil {
		return FileGitInfo{}, false
	}
	rel, ok := c.toRel(absPath)
	if !ok {
		return FileGitInfo{}, false
	}
	info, ok := c.files[rel]
	return info, ok
}

// IsTracked is the boolean-only form of Lookup: true when absPath is
// in git's index for this working tree.
func (c *Cache) IsTracked(absPath string) bool {
	if c == nil {
		return false
	}
	rel, ok := c.toRel(absPath)
	if !ok {
		return false
	}
	_, ok = c.tracked[rel]
	return ok
}

// IsIgnored returns true when absPath is matched by .gitignore (or one
// of the other standard ignore-rule sources git consults) but not in
// the index. Tracked files are never reported as ignored even if a
// later .gitignore rule would have excluded them — that matches git's
// own `check-ignore` semantics.
func (c *Cache) IsIgnored(absPath string) bool {
	if c == nil {
		return false
	}
	rel, ok := c.toRel(absPath)
	if !ok {
		return false
	}
	_, ok = c.ignored[rel]
	return ok
}

// toRel converts absPath to a forward-slash repo-relative key. Returns
// ok=false when absPath isn't inside the repo, with the rationale that
// paths outside the working tree can't possibly have git metadata for
// THIS cache.
//
// We try repoRoot first (git's canonical view) then repoRootAlt (the
// as-supplied root). The second covers the macOS /tmp ↔ /private/tmp
// symlink case where a caller's filepath.Walk emits /tmp/... paths but
// git reports /private/tmp/... as the toplevel. Doing the dual-prefix
// check here is one alloc-free path comparison; an EvalSymlinks
// fallback would be one stat per lookup, which we'd be paying on
// every file in the walk.
func (c *Cache) toRel(absPath string) (string, bool) {
	if absPath == "" {
		return "", false
	}
	if rel, ok := relUnder(c.repoRoot, absPath); ok {
		return rel, true
	}
	if c.repoRootAlt != "" {
		if rel, ok := relUnder(c.repoRootAlt, absPath); ok {
			return rel, true
		}
	}
	return "", false
}

// relUnder is the inner check: filepath.Rel + reject ".." results so
// paths sibling-but-not-under base don't sneak through.
func relUnder(base, absPath string) (string, bool) {
	if base == "" {
		return "", false
	}
	rel, err := filepath.Rel(base, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	// git emits forward slashes on every platform; normalise so
	// Windows callers (and Windows-style absolute paths) match.
	return filepath.ToSlash(rel), true
}

// --- subprocess helpers ---

func repoToplevel(ctx context.Context, root string) (string, error) {
	out, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func revParseHead(ctx context.Context, root string) (string, error) {
	out, err := runGit(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func lsFilesTracked(ctx context.Context, root string) ([]string, error) {
	out, err := runGit(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

func lsFilesIgnored(ctx context.Context, root string) ([]string, error) {
	out, err := runGit(ctx, root, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

// logFiles parses one big `git log` invocation that emits per-commit
// headers followed by the affected file list. Walk format:
//
//	COMMIT <sha>\t<unix-time>\t<author>\t<subject>\n
//	<path>\n
//	<path>\n
//	\n
//	COMMIT <sha>\t...
//
// We walk commits newest-first (git's default order). For each path
// touched in a commit:
//   - the FIRST time we see it → that's LastCommit{Time,Author,Subject}
//   - every subsequent time     → update FirstSeen (we keep overwriting
//     so the LAST commit we see for the path wins)
//   - CommitCount increments each appearance
func logFiles(ctx context.Context, root string) (map[string]FileGitInfo, error) {
	out, err := runGit(ctx, root,
		"log",
		"--name-only",
		"--format=COMMIT\t%H\t%at\t%an\t%s",
		"--no-renames",
		"HEAD",
	)
	if err != nil {
		return nil, err
	}

	files := make(map[string]FileGitInfo)
	var (
		curTime    time.Time
		curAuthor  string
		curSubject string
		haveCommit bool
	)
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "COMMIT\t"); ok {
			parts := strings.SplitN(rest, "\t", 4)
			if len(parts) < 4 {
				continue
			}
			ts, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				continue
			}
			curTime = time.Unix(ts, 0).UTC()
			curAuthor = parts[2]
			curSubject = parts[3]
			haveCommit = true
			continue
		}
		if !haveCommit {
			// File path before any commit header — shouldn't happen
			// with the format above, but be defensive.
			continue
		}
		info, ok := files[line]
		if !ok {
			info = FileGitInfo{
				LastCommitTime:    curTime,
				LastCommitAuthor:  curAuthor,
				LastCommitSubject: curSubject,
				FirstSeen:         curTime,
				CommitCount:       0,
			}
		}
		// Walking newest-first → keep overwriting FirstSeen with the
		// OLDER value we currently see. Don't touch LastCommit*; that's
		// frozen at the first sight.
		info.FirstSeen = curTime
		info.CommitCount++
		files[line] = info
	}
	return files, nil
}

// runGit shells out to git with -C <root> so the command runs in the
// repo's working directory regardless of the caller's cwd. Returns
// stdout as a string; errors carry stderr in the message for
// diagnosability.
func runGit(ctx context.Context, root string, args ...string) (string, error) {
	full := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Pass ctx cancellation through unwrapped so callers can
		// errors.Is() it. Other errors carry stderr for debugging.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %v: %w (stderr: %s)", args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// splitNUL splits a NUL-delimited git output (from `-z`) into a slice,
// discarding the trailing empty record git always emits.
func splitNUL(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\x00")
	// Trim trailing empty caused by the terminator.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func setFromSlice(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, k := range s {
		out[k] = struct{}{}
	}
	return out
}

// buildEmptyHeadCache handles the empty-repo case (git init with no
// commits): no HEAD, no log, but ls-files still works on the index.
// Ignored-files detection also works (it doesn't need a HEAD). We
// return a cache with empty per-file metadata so is_git_tracked /
// is_git_ignored predicates can still answer.
func buildEmptyHeadCache(ctx context.Context, repoRoot string) (*Cache, error) {
	tracked, err := lsFilesTracked(ctx, repoRoot)
	if err != nil {
		// Even ls-files failed — return nil cache and let the walk
		// proceed with empty git data.
		return nil, nil //nolint:nilerr
	}
	ignored, err := lsFilesIgnored(ctx, repoRoot)
	if err != nil {
		ignored = nil
	}
	return &Cache{
		repoRoot:    repoRoot,
		repoRootAlt: "", // populated only by the New() happy path
		headSHA:     "",
		files:       nil,
		tracked:     setFromSlice(tracked),
		ignored:     setFromSlice(ignored),
	}, nil
}

// HasGitBinary returns true when the `git` executable is on PATH.
// Useful for CLI callers that want to warn up front rather than
// silently produce empty git_* attributes. Cheap (one exec.LookPath).
func HasGitBinary() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
