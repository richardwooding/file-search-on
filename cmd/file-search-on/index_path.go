package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.etcd.io/bbolt"

	"github.com/richardwooding/file-search-on/internal/index"
)

// indexSubdir is the per-user cache subdirectory for the default
// on-disk index files. Mirrors the registrySubdir convention used by
// internal/monitor/registry.go so all of file-search-on's per-user
// state lives under one parent.
const indexSubdir = "file-search-on/indexes"

// maxBasenameLen caps the human-readable filename component to keep
// the resulting path well under typical filesystem limits even when
// the cwd basename is unusually long.
const maxBasenameLen = 50

// IndexBackend records which storage mode an index ended up using and
// why. The CLI surfaces nothing with this directly, but the MCP server
// passes it to the monitor.Controller so the dashboard and the
// monitor_info MCP tool can show "this session is in-memory because
// another instance holds the index" without scraping logs.
type IndexBackend struct {
	// Mode is "persistent" when the bbolt file is open, "in-memory"
	// when the cache is process-lifetime only.
	Mode string
	// Path is the absolute file path when Mode == "persistent", empty
	// otherwise.
	Path string
	// Reason is empty for the happy path. When Mode == "in-memory"
	// because of a fallback or opt-out, this carries the cause:
	//   - "no_index_flag"   — caller passed --no-index
	//   - "lock_contention" — another process holds the writer lock
	Reason string
}

// IndexBackend mode constants — keep stringly-typed so they pass
// cleanly through JSON to the dashboard / monitor_info without
// per-language enum gymnastics.
const (
	BackendPersistent = "persistent"
	BackendInMemory   = "in-memory"

	ReasonNoIndexFlag   = "no_index_flag"
	ReasonLockContention = "lock_contention"
)

// defaultIndexPath returns the per-cwd default index file path,
// creating the parent directory if needed. cwd is the absolute
// working directory; pass an empty string to use os.Getwd().
//
// Path shape: <UserCacheDir>/file-search-on/indexes/<basename>-<sha1[:6]>.db
//
// The basename-shorthash compound keeps the filename readable in `ls`
// while still being collision-free across projects that share a
// basename (multiple ~/.../foo/ dirs hash to different short codes).
// The hash is taken over the absolute path so symlinked variants of
// the same directory map to one file, but plain renames stay
// isolated.
func defaultIndexPath(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("abs %q: %w", cwd, err)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, indexSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	sum := sha1.Sum([]byte(abs))
	shorthash := hex.EncodeToString(sum[:])[:6]
	bn := sanitiseBasename(filepath.Base(abs))
	if bn == "" {
		bn = "root"
	}
	return filepath.Join(dir, bn+"-"+shorthash+".db"), nil
}

// sanitiseBasename produces a filesystem-safe component from an
// arbitrary directory name. Allowed characters survive verbatim;
// anything else becomes an underscore. The result is also length-
// capped to maxBasenameLen so the final filename stays short.
func sanitiseBasename(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > maxBasenameLen {
		out = out[:maxBasenameLen]
	}
	return out
}

// isBoltLockTimeout reports whether err is bbolt's "couldn't acquire
// the writer lock" sentinel. Used by openIndex to decide between
// surfacing the error and silently falling back to in-memory.
func isBoltLockTimeout(err error) bool {
	return errors.Is(err, bbolt.ErrTimeout)
}

// resolveIndexBackend picks the right index for the given cwd /
// IndexPath / NoIndex inputs, opens it, and returns diagnostic info
// describing the selection. On lock-timeout fallback, a single-line
// warning is logged to stderr so the human (or the MCP client's log
// pane) sees what happened.
//
// The signature deliberately fronts the diagnostic info: every caller
// that runs a dashboard (mcp, watch) needs it; CLI subcommands that
// don't can simply discard the second return value.
func resolveIndexBackend(cwd, path string, noIndex bool, bodyCap index.BodyCacheCap) (index.Index, IndexBackend, error) {
	// Explicit opt-out wins immediately — no path picking, no disk I/O.
	if noIndex {
		return index.NewMemory(), IndexBackend{Mode: BackendInMemory, Reason: ReasonNoIndexFlag}, nil
	}
	// No explicit path → pick the per-cwd default.
	if path == "" {
		var err error
		path, err = defaultIndexPath(cwd)
		if err != nil {
			// Fall back to in-memory rather than erroring out the
			// process — if the cache dir is unavailable we still
			// want the tool to work.
			fmt.Fprintf(os.Stderr, "file-search-on: could not resolve default index path (%v); using in-memory cache\n", err)
			return index.NewMemory(), IndexBackend{Mode: BackendInMemory, Reason: ReasonLockContention}, nil
		}
	}
	idx, err := index.OpenWith(path, bodyCap)
	if err == nil {
		return idx, IndexBackend{Mode: BackendPersistent, Path: path}, nil
	}
	if isBoltLockTimeout(err) {
		fmt.Fprintf(os.Stderr, "file-search-on: index file %s is held by another instance; this session will use in-memory cache (use --no-index to silence this warning)\n", path)
		return index.NewMemory(), IndexBackend{Mode: BackendInMemory, Reason: ReasonLockContention}, nil
	}
	if errors.Is(err, index.ErrSchemaMismatch) {
		return nil, IndexBackend{}, fmt.Errorf("index file at %s has an incompatible schema; delete it or pass a new --index-path", path)
	}
	return nil, IndexBackend{}, fmt.Errorf("open index: %w", err)
}
