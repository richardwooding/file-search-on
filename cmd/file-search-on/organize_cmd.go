package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// OrganizeCmd builds a templated symlink (or copy) tree from search
// results — a "virtual organized view" of a flat directory without
// touching the originals. Issue #209.
//
//	file-search-on organize 'is_raw_photo' \
//	  --link-into '~/sorted/{raw_vendor}/{mtime_year}/{basename}' -d ~/Pictures
//
// The --link-into template uses {token} brace syntax (friendlier than
// Go text/template for paths). Tokens resolve against the matched
// file's attributes; literal "/" in the template are real path
// separators, while substituted token VALUES are sanitised so a
// stray slash (e.g. in content_type) can't inject extra nesting.
type OrganizeCmd struct {
	Expr     string   `arg:"" optional:"" help:"CEL expression selecting which files to organize (e.g. 'is_raw_photo'). Empty matches everything."`
	LinkInto string   `name:"link-into" required:"" help:"Destination path template using {token} placeholders. Tokens: {basename} {stem} {ext} {dir} {content_type} {size}; time buckets {mtime_year|month|day} {taken_at_year|month|day} {created_at_*} {sent_at_*}; and any file attribute by its CEL name ({camera_make}, {raw_vendor}, {language}, {ocr_language}, …). Empty / missing tokens become 'unknown'. A leading ~/ expands to your home dir. Example: '~/sorted/{raw_vendor}/{mtime_year}/{basename}'."`
	Dir      []string `short:"d" name:"dir" default:"." help:"Directory to search. Repeatable."`
	DryRun   bool     `name:"dry-run" help:"Print the planned source → target actions without creating anything."`
	Copy     bool     `name:"copy-instead" help:"Copy file contents instead of symlinking. Uses real disk; the default symlink view is free and resolves back to the original."`
	OnConflict string `name:"on-conflict" enum:"skip,overwrite,number" default:"skip" help:"What to do when the target path already exists: skip (leave it, default) | overwrite (replace) | number (append ' (1)', ' (2)', … before the extension)."`

	Workers          int      `short:"w" name:"workers" default:"0" help:"Parallel walker workers. 0 = NumCPU."`
	IndexPath        string   `name:"index-path" help:"Persistent attribute index (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/. Speeds repeat organizes over the same tree."`
	NoIndex          bool     `name:"no-index" help:"Disable the on-disk index entirely; use only in-memory caching for the process lifetime."`
	Exclude          []string `name:"exclude" help:"Basename glob to prune from the walk. Repeatable."`
	RespectGitignore bool     `name:"respect-gitignore" help:"Honour a .gitignore at each walk root."`
	PruneArtefacts   bool     `name:"prune-build-artefacts" help:"Prune vendor / node_modules / target / __pycache__ etc. for detected project types."`
	FollowSymlinks   bool     `name:"follow-symlinks" help:"Descend through directory symlinks during the walk."`
	OCR              bool     `name:"ocr" help:"Run OCR over images so {ocr_language} / {ocr_confidence} tokens resolve (macOS Vision). No-op where no OCR provider is registered."`
	WithPHash        bool     `name:"with-phash" help:"Compute perceptual hashes so {phash} resolves."`
	Body             bool     `name:"body" help:"Read file bodies (needed only if the selecting expression uses body.contains / has_secrets / etc.)."`
	Output           string   `short:"o" name:"output" enum:"default,bare" default:"default" help:"default (per-action lines + summary footer) | bare (target paths only)."`
}

// tokenPattern matches {snake_case_token} placeholders in the template.
var tokenPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func (o *OrganizeCmd) Run(ctx context.Context) error {
	expr := o.Expr
	if expr == "" {
		expr = "true"
	}

	idx, _, err := openIndex(o.IndexPath, o.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	opts := search.Options{
		Roots:               o.Dir,
		Expr:                expr,
		Workers:             o.Workers,
		IncludeAttributes:   true, // organize needs Extra for template tokens
		Index:               idx,
		IncludeBody:         o.Body,
		OCRImages:           o.OCR,
		WithPHash:           o.WithPHash || strings.Contains(expr, "image_similar_to"),
		Excludes:            o.Exclude,
		RespectGitignore:    o.RespectGitignore,
		FollowSymlinks:      o.FollowSymlinks,
		PruneBuildArtefacts: o.PruneArtefacts,
	}

	results, err := search.Walk(ctx, opts, contentpkg.DefaultRegistry())
	if err != nil {
		return err
	}

	var created, skipped, failed int
	for _, r := range results {
		target, terr := o.renderTarget(r)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "skip (template): %s: %v\n", r.Path, terr)
			failed++
			continue
		}

		// Resolve the final target, applying the conflict policy.
		finalTarget, action := o.resolveConflict(target)
		if action == actionSkip {
			if o.Output != "bare" {
				fmt.Fprintf(os.Stderr, "skip (exists): %s\n", target)
			}
			skipped++
			continue
		}

		if o.DryRun {
			o.printAction(r.Path, finalTarget, true)
			created++
			continue
		}

		if err := o.materialize(r.Path, finalTarget, action); err != nil {
			fmt.Fprintf(os.Stderr, "fail: %s -> %s: %v\n", r.Path, finalTarget, err)
			failed++
			continue
		}
		o.printAction(r.Path, finalTarget, false)
		created++
	}

	var verb string
	switch {
	case o.DryRun && o.Copy:
		verb = "would copy"
	case o.DryRun:
		verb = "would link"
	case o.Copy:
		verb = "copied"
	default:
		verb = "linked"
	}
	fmt.Fprintf(os.Stderr, "%s %d, skipped %d, failed %d\n", verb, created, skipped, failed)
	if failed > 0 {
		return &exitCodeError{code: 1}
	}
	return nil
}

func (o *OrganizeCmd) printAction(src, target string, dry bool) {
	if o.Output == "bare" {
		fmt.Println(target)
		return
	}
	arrow := "->"
	prefix := ""
	if dry {
		prefix = "[dry-run] "
	}
	fmt.Printf("%s%s %s %s\n", prefix, src, arrow, target)
}

// renderTarget expands the --link-into template against a single
// result, substituting + sanitising {token} placeholders and
// expanding a leading ~/.
func (o *OrganizeCmd) renderTarget(r search.Result) (string, error) {
	out := tokenPattern.ReplaceAllStringFunc(o.LinkInto, func(tok string) string {
		name := tok[1 : len(tok)-1] // strip { }
		return sanitizeComponent(resolveToken(name, r))
	})
	if out == "" {
		return "", fmt.Errorf("template rendered to empty path")
	}
	return expandHome(out), nil
}

// resolveToken maps a template token to its string value for a given
// result. Unknown / empty tokens return "" (the caller sanitises that
// to "unknown").
func resolveToken(name string, r search.Result) string {
	base := filepath.Base(r.Path)
	switch name {
	case "basename":
		return base
	case "ext":
		e := strings.TrimPrefix(filepath.Ext(base), ".")
		if e == "" {
			return "none"
		}
		return e
	case "stem":
		return strings.TrimSuffix(base, filepath.Ext(base))
	case "dir":
		return filepath.Base(filepath.Dir(r.Path))
	case "content_type":
		return r.ContentType
	case "size":
		return fmt.Sprintf("%d", r.Size)
	}
	// Time buckets — mtime_year, taken_at_month, created_at_day, …
	if attr, layout, ok := organizeTimeBucket(name); ok {
		t := pullResultTime(r, attr)
		if t.IsZero() {
			return ""
		}
		return t.Format(layout)
	}
	// Anything else: look it up in the attribute Extra map.
	if r.Attrs != nil && r.Attrs.Extra != nil {
		if v, ok := r.Attrs.Extra[name]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// organizeTimeBucket mirrors search.timeBucketSpec (unexported there)
// for the template token vocabulary: <attr>_<year|month|day>.
func organizeTimeBucket(name string) (attr, layout string, ok bool) {
	for _, prefix := range []string{"mtime", "created_at", "metadata_changed_at", "taken_at", "sent_at", "date"} {
		for _, g := range []struct{ suffix, layout string }{
			{"_year", "2006"},
			{"_month", "2006-01"},
			{"_day", "2006-01-02"},
		} {
			if name == prefix+g.suffix {
				return prefix, g.layout, true
			}
		}
	}
	return "", "", false
}

// pullResultTime resolves a named time attribute on a result. mtime /
// created_at / metadata_changed_at read FileAttributes fields directly;
// taken_at / sent_at / date come from the Extra map as time.Time.
func pullResultTime(r search.Result, attr string) time.Time {
	if r.Attrs == nil {
		return time.Time{}
	}
	switch attr {
	case "mtime":
		return r.Attrs.ModTime
	case "created_at":
		return r.Attrs.CreatedAt
	case "metadata_changed_at":
		return r.Attrs.MetadataChangedAt
	}
	if r.Attrs.Extra == nil {
		return time.Time{}
	}
	if t, ok := r.Attrs.Extra[attr].(time.Time); ok {
		return t
	}
	return time.Time{}
}

// sanitizeComponent makes a resolved token value safe as a single
// path component: empty -> "unknown", path separators -> "-", and
// trailing/leading dots that would create hidden / traversal segments
// neutralised.
func sanitizeComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, string(os.PathSeparator), "-")
	s = strings.ReplaceAll(s, "\x00", "")
	if s == "." || s == ".." {
		return "unknown"
	}
	return s
}

// expandHome expands a leading ~/ to the user's home directory.
func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

type conflictAction int

const (
	actionCreate conflictAction = iota
	actionOverwrite
	actionSkip
)

// resolveConflict inspects the target path and returns the (possibly
// adjusted) final path + the action to take per the --on-conflict
// policy.
func (o *OrganizeCmd) resolveConflict(target string) (string, conflictAction) {
	if _, err := os.Lstat(target); err != nil {
		return target, actionCreate // doesn't exist
	}
	switch o.OnConflict {
	case "overwrite":
		return target, actionOverwrite
	case "number":
		return numberedPath(target), actionCreate
	default: // skip
		return target, actionSkip
	}
}

// numberedPath finds the first non-existent "<stem> (N)<ext>" variant.
func numberedPath(target string) string {
	ext := filepath.Ext(target)
	stem := strings.TrimSuffix(target, ext)
	for n := 1; n < 100000; n++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, n, ext)
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
	return target // pathological; let materialize fail loudly
}

// materialize creates the parent directory then the symlink / copy.
func (o *OrganizeCmd) materialize(src, target string, action conflictAction) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if action == actionOverwrite {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if o.Copy {
		return copyFile(src, target)
	}
	// Symlink with an ABSOLUTE source so the link resolves regardless
	// of where the target tree lives relative to the source.
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return os.Symlink(absSrc, target)
}

func copyFile(src, target string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
