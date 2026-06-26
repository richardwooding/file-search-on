package celexpr

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djherbis/times"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/projectdetect"
	"github.com/richardwooding/gitmeta"
	"github.com/richardwooding/ollamaembed"
)

// symlinkInfo captures the result of an os.Lstat + os.Readlink probe
// against a real OS path. All fields stay zero when the probe fails
// (e.g. in tests where the path doesn't exist on disk), so callers
// can apply the info unconditionally — non-symlinks surface as
// is_symlink=false / target_path="".
type symlinkInfo struct {
	isSymlink bool
	target    string
	broken    bool
}

// probeSymlink runs os.Lstat against displayPath and, if the path is a
// symlink, reads its target via os.Readlink and tests resolvability
// via os.Stat. Returns a zero-value symlinkInfo when displayPath
// isn't a real OS path or isn't a symlink — keeping the call cheap
// and safe to invoke unconditionally.
func probeSymlink(displayPath string) symlinkInfo {
	if displayPath == "" {
		return symlinkInfo{}
	}
	lstatInfo, err := os.Lstat(displayPath)
	if err != nil {
		return symlinkInfo{}
	}
	if lstatInfo.Mode()&os.ModeSymlink == 0 {
		return symlinkInfo{}
	}
	info := symlinkInfo{isSymlink: true}
	if target, rerr := os.Readlink(displayPath); rerr == nil {
		info.target = target
	}
	if _, terr := os.Stat(displayPath); terr != nil {
		info.broken = true
	}
	return info
}

// fileTimesInfo carries the platform-specific filesystem timestamps
// — created (btime) and metadataChanged (ctime) — pulled via
// djherbis/times. Either may be zero when the filesystem doesn't
// track that timestamp; both zero for in-memory test fs.FS where
// displayPath isn't a real OS path.
type fileTimesInfo struct {
	created         time.Time
	metadataChanged time.Time
}

// probeFileTimes calls times.Stat(displayPath) and pulls btime + ctime
// when the underlying filesystem exposes them. Best-effort: any error
// (path doesn't exist, in-memory fs.FS, unsupported FS) returns a
// zero-valued result.
func probeFileTimes(displayPath string) fileTimesInfo {
	if displayPath == "" {
		return fileTimesInfo{}
	}
	t, err := times.Stat(displayPath)
	if err != nil {
		return fileTimesInfo{}
	}
	var out fileTimesInfo
	if t.HasBirthTime() {
		out.created = t.BirthTime()
	}
	if t.HasChangeTime() {
		out.metadataChanged = t.ChangeTime()
	}
	return out
}

// applyFileTimes writes the filesystem-timestamp probe result onto a
// built FileAttributes and sets IsBtimeAnomaly when CreatedAt is
// after ModTime — the classic forensic "this file was placed here
// after being modified elsewhere" indicator.
func applyFileTimes(attrs *FileAttributes, ft fileTimesInfo) {
	if attrs == nil {
		return
	}
	attrs.CreatedAt = ft.created
	attrs.MetadataChangedAt = ft.metadataChanged
	if !ft.created.IsZero() && !attrs.ModTime.IsZero() && ft.created.After(attrs.ModTime) {
		attrs.IsBtimeAnomaly = true
	}
}

// applySymlinkInfo writes the symlink probe result onto a built
// FileAttributes. Sets the IsSymlink / IsBrokenSymlink struct fields
// (so CEL evaluation reads them via the activation's typed switch)
// and lands target_path under Extra (so it's surfaced to the CEL
// `target_path` string variable via the Extra-key fallback).
func applySymlinkInfo(attrs *FileAttributes, sym symlinkInfo) {
	if attrs == nil || !sym.isSymlink {
		return
	}
	attrs.IsSymlink = true
	attrs.IsBrokenSymlink = sym.broken
	if sym.target != "" {
		if attrs.Extra == nil {
			attrs.Extra = content.Attributes{}
		}
		attrs.Extra["target_path"] = sym.target
	}
}

// staticSiteTypes is the set of registered project-type names that
// constitute a static-site generator for the purposes of the
// is_static_site CEL family predicate. Mirrors how setTypeFlags
// populates is_image / is_audio from a content-type prefix, but the
// match is against the file's resolved project_type rather than its
// content_type. Opt-in via search.Options.ResolveProjects — without
// it, project_types is empty and the predicate stays false.
//
// Adding a new SSG project type in the projectdetect module/builtins.go
// requires adding its name here too. The four-place invariant
// (cel.Variable + activation default + Extra population + schema doc)
// applies — see .claude/skills/extend-cel-schema for the audit.
var staticSiteTypes = map[string]struct{}{
	"hugo":       {},
	"jekyll":     {},
	"eleventy":   {},
	"astro":      {},
	"gatsby":     {},
	"mkdocs":     {},
	"docusaurus": {},
	"pelican":    {},
}

// anyStaticSite reports whether any name in matches is a recognised
// static-site generator type. Caller passes the resolved
// project-type names from ProjectResolver.Resolve.
func anyStaticSite(matches []string) bool {
	for _, m := range matches {
		if _, ok := staticSiteTypes[m]; ok {
			return true
		}
	}
	return false
}

// Evaluator evaluates CEL expressions against file attributes
type Evaluator struct {
	env  *cel.Env
	prog cel.Program
}

// Evaluate evaluates the expression against the given file attributes
// Evaluate evaluates the expression against the given file attributes.
// Uses a custom cel.Activation backed directly by *FileAttributes so
// there's no per-call map allocation (was ~35% of walker allocations
// per pprof before this).
func (e *Evaluator) Evaluate(attrs *FileAttributes) (bool, error) {
	out, _, err := e.prog.Eval(&fileAttrsActivation{attrs: attrs})
	if err != nil {
		return false, fmt.Errorf("evaluating CEL expression: %w", err)
	}
	return out == types.True, nil
}

// BuildOptions tunes BuildAttributesWith. Index, when non-nil, is
// consulted before any expensive parse: a (size, mtime)-validated hit
// returns the cached attributes without re-running registry.Detect or
// ContentType.Attributes; a miss falls through to the existing
// extraction path and stores the result for the next call. nil Index
// disables caching.
//
// IncludeBody, when true, makes BuildAttributesWith read the file
// body for text-based content types (markdown / text / html / csv /
// json / xml / source/*) and surface it via the "body" CEL variable.
// Bodies are capped at BodyMaxBytes (default 1 MiB when zero) to
// bound memory and stop pathological inputs from blowing up the
// search response. The cap is on the cached/returned body string,
// not on the file read — files larger than the cap are truncated,
// not skipped. Binary content types leave body empty regardless.
//
// The body read is gated by the IncludeBody flag rather than the
// CEL expression's variable references because cel-go doesn't expose
// "did the compiled program use this variable" cheaply. Callers
// that want CEL filters like `body.contains("X")` or
// `body.matches("...")` must opt in.
type BuildOptions struct {
	Index        index.Index
	IncludeBody  bool
	BodyMaxBytes int
	// ProjectResolver, when set, populates Extra["project_types"]
	// and Extra["project_type"] for each file by walking up the
	// file's directory chain to the nearest project-root indicator.
	// nil disables project-context resolution. Constructed by the
	// walker when search.Options.ResolveProjects is true (one per
	// walk root for multi-root walks).
	ProjectResolver *projectdetect.ProjectResolver
	// SkipAttributesParse, when true, makes BuildAttributesWith
	// detect the file's content type and run setTypeFlags (so per-
	// type and family bools fire) BUT skip the expensive
	// ContentType.Attributes(ctx, fsys, path) parse. The returned
	// FileAttributes has Path / Size / ModTime / ContentType /
	// per-type bools populated and an empty Extra map.
	//
	// Used by ComputeStats when GroupBy is a detector-only key
	// (content_type / ext / dir / mtime_*) AND the CEL expression
	// doesn't need attribute fields. Cuts /Applications-style stats
	// from minutes to seconds.
	//
	// When set, the index cache is bypassed for both Lookup and Put
	// — empty Extras would otherwise poison the cache for later
	// calls that DO want attributes.
	SkipAttributesParse bool

	// SkipAttributesPrefixes is a per-file refinement of
	// SkipAttributesParse: any content type whose name starts with
	// one of the prefixes has its per-format Attributes parse AND
	// its attribute-cache write skipped. Detection still fires so
	// ContentType + is_X family flags populate. Empty list = full
	// parse for every detected type (today's default).
	//
	// Driven by search.Options.Profile through the walker; CEL
	// callers don't usually set this directly. Cache-skip is
	// intentional: a profile-skipped entry would have empty Extra,
	// and a later profile-less walk would serve that empty cached
	// entry instead of re-parsing for real. Issue #284.
	SkipAttributesPrefixes []string

	// ComputeHashes, when true, makes BuildAttributesWith populate
	// MD5 / SHA1 / SHA256 on every walked file. The three hashes
	// are computed in one io.MultiWriter pass via
	// internal/cryptohash so a single file read populates all
	// three. The cached index.Entry.MD5 / SHA1 / Hash fields are
	// consulted first; on hit they short-circuit the file read.
	// Cache miss or a hit with empty Hash triggers re-compute.
	//
	// Off by default — hashing every file in a tree is expensive
	// (multi-GB videos read fully into the hashers). Opt-in for
	// forensic / dedup workflows; CLI exposes via --with-hashes
	// on the search subcommand, MCP via compute_hashes input.
	ComputeHashes bool

	// WithPHash, when true, computes the 64-bit perceptual hash of
	// every image walked and surfaces it as the `phash` CEL string
	// (16-char hex). The hash is robust to mild resize / JPEG
	// re-encode / brightness shifts so `image_similar_to(reference,
	// threshold)` finds visually-similar variants. Pure-Go DCT over
	// a 32×32 grayscale grid; ~1ms per image. Cached in index.Entry.
	// PHash under (size, mtime). Auto-enabled when the CEL
	// expression references `image_similar_to(...)` so callers don't
	// have to remember the flag. CLI: --with-phash. MCP: with_phash.
	// Issue #208.
	WithPHash bool

	// CheckDisguised, when true, makes BuildAttributesWith call
	// registry.DetectBoth (instead of Detect) and populate
	// MagicContentType / ExtensionContentType / IsDisguised. The
	// extra cost is one 512-byte file read per file whose
	// extension already won — the magic pass that Detect's
	// fast-path normally skips.
	//
	// Cached via index.Entry.MagicContentType /
	// ExtensionContentType (both gob-additive); a cache hit with
	// either field populated short-circuits the re-detect.
	//
	// Off by default. Opt-in for forensic / triage workflows; CLI
	// exposes via --check-disguised on the search subcommand, MCP
	// via check_disguised input.
	CheckDisguised bool

	// ReadExtendedAttributes, when true, populates extended-attribute
	// attrs (xattr_keys, is_quarantined, quarantine_source_url,
	// finder_tags, finder_color, …) on every walked file via
	// content.ReadXattrs. Darwin-only — non-Darwin builds always
	// surface empty xattr attrs regardless of this flag (see
	// internal/content/xattrs_other.go).
	//
	// Off by default — xattrs are syscalls (Listxattr + Getxattr)
	// that add 50-100 microseconds per file. Opt-in for forensic /
	// triage workflows; CLI exposes via --with-xattrs on the search
	// subcommand, MCP via with_xattrs input. Issue #193.
	ReadExtendedAttributes bool

	// Allowlist / Denylist are hash-allowlist / hash-denylist
	// query layers (PR #146). When non-nil AND ComputeHashes is
	// also set, BuildAttributesWith populates IsKnownGood /
	// IsKnownBad on each FileAttributes by looking up the
	// computed MD5 / SHA1 / SHA256 in the respective Set. Either
	// or both may be nil (no list loaded → corresponding flag
	// stays false). Membership is NOT cached in the index —
	// it's a function of the loaded set, not the file's
	// (size, mtime).
	Allowlist hashset.Set
	Denylist  hashset.Set

	// Embedder + SemanticQueryEmbedding power the `similarity`
	// CEL variable (issue #151). When both are set,
	// BuildAttributesWith reads each file's body, embeds it via
	// the Embedder (or reuses the cached Vector from
	// index.Entry.Vector when available), normalises, and stores
	// the cosine against SemanticQueryEmbedding in
	// FileAttributes.Similarity.
	//
	// The caller is responsible for pre-embedding the query once
	// per walk and passing the resulting vector via
	// SemanticQueryEmbedding — that keeps the per-file work
	// strictly local (one cosine dot product + optional embed +
	// optional cache put) and avoids re-embedding the query for
	// every file.
	//
	// When Embedder is nil OR SemanticQueryEmbedding is empty,
	// Similarity stays at 0 — same wire shape as "no semantic
	// search requested".
	Embedder               ollamaembed.Embedder
	SemanticQueryEmbedding []float32

	// EmbedInputMaxBytes caps the body text handed to the Embedder
	// (NOT the body read / cached for `body.contains` — that's
	// BodyMaxBytes). Embedding models have small context windows
	// (a few hundred to a few thousand tokens) and some Ollama
	// model/version combinations return an HTTP error rather than
	// silently truncating book-length input, which would otherwise
	// surface as a silent "0 results" (issue #305). Capping the embed
	// input avoids that AND saves bandwidth — the model truncates to
	// its context window anyway, so bytes past the window never
	// influence the vector. 0 → defaultEmbedInputMaxBytes. The cap is
	// applied on a UTF-8 rune boundary.
	EmbedInputMaxBytes int

	// KeywordQuery, when non-empty, is the tokenized keyword query used
	// to capture per-file BM25 carrier data (term frequencies of these
	// terms + the body token length) during body extraction (issue #335).
	// The actual BM25 score needs corpus IDF, so it's left to the
	// buffered post-pass (search.FinalizeBM25); this just records the
	// cheap per-file inputs. Empty → no BM25 capture.
	KeywordQuery []string

	// GitCache, when non-nil, drives population of the git_* fields on
	// FileAttributes (last commit time/author/subject, first seen,
	// commit count, is_tracked, is_ignored). The walker builds one
	// cache per root via gitmeta.New (which runs a single git log
	// pass) and shares it across all worker goroutines for that root.
	// nil means "no git data; leave git_* at zero values" — the
	// non-git-tree default. Issue #271.
	GitCache *gitmeta.Cache

	// OCRImages, when true, runs OCR over `image/*` files via the
	// registered OCR provider (macOS Vision today, future Tesseract /
	// Windows.Media.Ocr providers slot into the same hook). The
	// recognized text populates the `body` CEL variable; the average
	// per-line confidence, detected language, and provider name
	// populate `ocr_confidence` / `ocr_language` / `ocr_provider`.
	//
	// Off by default. OCR is expensive (200ms-2s per image first
	// walk; cached on subsequent walks via bodies_v1). CLI exposes
	// via --ocr; MCP via ocr_images.
	//
	// Independent of IncludeBody — passing --ocr alone is enough to
	// populate `body` on images. Document body extraction for non-
	// image types still requires --body.
	//
	// On platforms without a registered OCR provider (Linux / Windows
	// today), the flag is a no-op: ocr.HasProvider() returns false
	// and the OCR hook short-circuits.
	OCRImages bool

	// OCRTimeout caps each per-file OCR call. Defaults to 10 seconds
	// when zero; the helper process gets SIGKILL when the ctx times
	// out so a misbehaving image can't stall the walk.
	OCRTimeout time.Duration

	// VerifyC2PA, when true, runs full C2PA / Content Credentials
	// verification (content.ValidateImageC2PA → c2pa.Validate) over
	// image/* files and surfaces the VERIFIED attributes c2pa_valid /
	// c2pa_verified_signer / c2pa_verified_signed_at /
	// c2pa_validation_status — the authenticated counterpart to the fast,
	// unverified c2pa_* attributes that always populate via c2pa.Read.
	// Off by default: validation does real cryptographic work, unlike the
	// header read. The result is never cached (it is clock-dependent — a
	// signer cert can expire while the file is unchanged), so it is
	// recomputed on every walk, like project context. Issue #441.
	VerifyC2PA bool
}

// defaultBodyMaxBytes caps the body string supplied to CEL when
// IncludeBody is true and BuildOptions.BodyMaxBytes is unset. 1 MiB
// is plenty for typical text files (markdown posts, source modules,
// JSON manifests) and bounds the worst case on adversarial input.
const defaultBodyMaxBytes = 1 << 20

// defaultEmbedInputMaxBytes caps the text handed to the embedding
// model when BuildOptions.EmbedInputMaxBytes is unset. 8 KiB comfortably
// covers the context window of the common Ollama embedding models
// (all-minilm ~256 tokens, nomic-embed-text ~2048 tokens ≈ 8 KiB) and
// stays under the input size at which some model/version combinations
// reject the request outright (issue #305). Larger-context models can
// raise it via --embed-max-bytes / the search_semantic embed_max_bytes
// input.
const defaultEmbedInputMaxBytes = 8 << 10

// BuildAttributes is the no-cache wrapper. New callers should use
// BuildAttributesWith and pass an index.Index when caching is desired.
func BuildAttributes(ctx context.Context, fsys fs.FS, fsPath, displayPath string, registry *content.Registry) (*FileAttributes, error) {
	return BuildAttributesWith(ctx, fsys, fsPath, displayPath, registry, BuildOptions{})
}

// BuildAttributesWith builds file attributes for a given path. fsys is the
// filesystem to read from; fsPath is the fs.FS-style key (forward slashes,
// relative to the fsys root) used for IO; displayPath is the OS-native
// path surfaced to users via FileAttributes.Path. In production both come
// from the walker (`os.DirFS(root)` + relative slash path / `filepath.Join`
// of the same). In tests, both can be the same fs-style key. ctx is
// checked at entry and threaded into ContentType.Attributes so per-file
// work can be cancelled mid-scan.
//
// When opts.Index is non-nil and the on-disk file's mtime is non-zero,
// the cache is consulted before registry.Detect and ContentType.Attributes
// run; on hit, the cached (ContentType, Extra) is returned with a fresh
// FileAttributes built from the live os.Stat result. On miss the regular
// extraction path runs and the result is asynchronously enqueued for
// storage.
// attrBuild carries the per-file context shared by BuildAttributesWith and its
// enrichment helpers — the inputs that don't change once stat + symlink/time
// probes have run. cacheKey is NOT a field: it can be cleared mid-build (skip
// profile, parse error), so it's passed explicitly to each helper.
type attrBuild struct {
	ctx         context.Context
	fsys        fs.FS
	fsPath      string
	displayPath string
	info        fs.FileInfo
	sym         symlinkInfo
	ftimes      fileTimesInfo
	registry    *content.Registry
	opts        BuildOptions
}

// enrichCacheHit applies the per-walk, never-cached enrichments to a record
// assembled from an attribute-cache hit: on-demand body, OCR, verified C2PA,
// project context, hashes, perceptual hash, known-hash status, semantic
// similarity, BM25 carrier, disguise, xattrs, git metadata, file times and
// symlink info. Mirrors the cache-miss tail but reads the cached entry for the
// hash/phash/similarity/disguise fields. Behaviour extracted verbatim from
// BuildAttributesWith (no logic change).
func (b *attrBuild) enrichCacheHit(attrs *FileAttributes, cacheKey string, cached *index.Entry) {
	opts := b.opts
	if opts.IncludeBody && canExtractBody(cached.ContentType) {
		body := lookupOrExtractBody(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, b.info, cached.ContentType, opts)
		if body != "" {
			if attrs.Extra == nil {
				attrs.Extra = content.Attributes{}
			}
			attrs.Extra["body"] = body
		}
	}
	if opts.OCRImages && strings.HasPrefix(cached.ContentType, "image/") {
		if attrs.Extra == nil {
			attrs.Extra = content.Attributes{}
		}
		runImageOCR(b.ctx, b.displayPath, cacheKey, b.info, attrs.Extra, opts)
	}
	if opts.VerifyC2PA && strings.HasPrefix(cached.ContentType, "image/") {
		if v, ok := content.ValidateImageC2PA(b.ctx, b.fsys, b.fsPath, cached.ContentType); ok {
			if attrs.Extra == nil {
				attrs.Extra = content.Attributes{}
			}
			maps.Copy(attrs.Extra, v)
		}
	}
	if opts.ProjectResolver != nil {
		attrs.Extra = applyProjectContext(attrs.Extra, opts.ProjectResolver, b.displayPath)
	}
	if opts.ComputeHashes {
		populateHashes(b.ctx, b.displayPath, cacheKey, b.info, cached, attrs, opts.Index)
	}
	if opts.WithPHash {
		if attrs.Extra == nil {
			attrs.Extra = content.Attributes{}
		}
		populatePHash(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, cached.ContentType, b.info, cached, attrs.Extra, opts.Index)
	}
	if opts.Allowlist != nil || opts.Denylist != nil {
		applyKnownStatus(attrs, opts)
	}
	if opts.Embedder != nil && len(opts.SemanticQueryEmbedding) > 0 {
		populateSimilarity(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, b.info, cached, attrs, opts)
	}
	if len(opts.KeywordQuery) > 0 {
		populateBM25Doc(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, b.info, cached.ContentType, attrs, opts)
	}
	if opts.CheckDisguised {
		if cached.DisguiseChecked {
			applyDisguise(attrs, cached.MagicContentType, cached.ExtensionContentType)
		} else {
			magicCT, extCT := redetectDisguise(b.fsys, b.fsPath, b.registry)
			applyDisguise(attrs, magicCT, extCT)
			// Backfill cache with the disguise fields so the next walk can
			// serve from cache.
			updated := *cached
			updated.MagicContentType = magicCT
			updated.ExtensionContentType = extCT
			updated.DisguiseChecked = true
			_ = opts.Index.Put(cacheKey, &updated)
		}
	}
	if opts.ReadExtendedAttributes {
		applyXattrs(attrs, b.displayPath)
	}
	if opts.GitCache != nil {
		applyGitMeta(attrs, b.displayPath, opts.GitCache)
	}
	applyFileTimes(attrs, b.ftimes)
	applySymlinkInfo(attrs, b.sym)
}

// enrichCacheMiss applies the post-parse enrichments to a freshly-built record
// on the cache-miss path: hashes, perceptual hash, known-hash status, semantic
// similarity, BM25 carrier, disguise, xattrs, git metadata, file times and
// symlink info. The hash/phash/similarity helpers stamp their fields onto the
// shared cacheEntry (re-Put inside them) so the next walk hits. Behaviour
// extracted verbatim from BuildAttributesWith (no logic change).
func (b *attrBuild) enrichCacheMiss(attrs *FileAttributes, cacheKey string, cacheEntry *index.Entry, contentTypeName, magicCT, extCT string) {
	opts := b.opts
	if opts.ComputeHashes {
		populateHashes(b.ctx, b.displayPath, cacheKey, b.info, cacheEntry, attrs, opts.Index)
	}
	if opts.WithPHash {
		if attrs.Extra == nil {
			attrs.Extra = content.Attributes{}
		}
		populatePHash(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, contentTypeName, b.info, cacheEntry, attrs.Extra, opts.Index)
	}
	if opts.Allowlist != nil || opts.Denylist != nil {
		applyKnownStatus(attrs, opts)
	}
	if opts.Embedder != nil && len(opts.SemanticQueryEmbedding) > 0 {
		populateSimilarity(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, b.info, cacheEntry, attrs, opts)
	}
	if len(opts.KeywordQuery) > 0 {
		populateBM25Doc(b.ctx, b.fsys, b.fsPath, b.displayPath, cacheKey, b.info, contentTypeName, attrs, opts)
	}
	if opts.CheckDisguised {
		applyDisguise(attrs, magicCT, extCT)
	}
	if opts.ReadExtendedAttributes {
		applyXattrs(attrs, b.displayPath)
	}
	if opts.GitCache != nil {
		applyGitMeta(attrs, b.displayPath, opts.GitCache)
	}
	applyFileTimes(attrs, b.ftimes)
	applySymlinkInfo(attrs, b.sym)
}

// applyProjectContext resolves the nearest enclosing project for displayPath
// and stamps project_types / project_type / is_static_site onto extra,
// returning it (allocating when a project matched and extra was nil). Never
// cached — "containing project" is a directory-tree property, not a per-file
// one. Shared by the cache-hit and cache-miss paths.
func applyProjectContext(extra content.Attributes, resolver *projectdetect.ProjectResolver, displayPath string) content.Attributes {
	matches := resolver.Resolve(displayPath)
	if len(matches) == 0 {
		return extra
	}
	if extra == nil {
		extra = content.Attributes{}
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.Type
	}
	extra["project_types"] = names
	extra["project_type"] = names[0]
	if anyStaticSite(names) {
		extra["is_static_site"] = true
	}
	return extra
}

func BuildAttributesWith(ctx context.Context, fsys fs.FS, fsPath, displayPath string, registry *content.Registry, opts BuildOptions) (*FileAttributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Symlink probe — best-effort. When displayPath points at a real
	// OS path AND that path is a symbolic link, set sym fields so the
	// regular path below can apply them to the returned attrs and
	// skip cache+detection for broken links. For in-memory test
	// filesystems os.Lstat returns ENOENT and sym stays zero.
	sym := probeSymlink(displayPath)

	// Filesystem timestamp probe (PR #144). One extra stat()-shaped
	// syscall per file via djherbis/times to pull btime + ctime
	// where the OS / filesystem exposes them. Best-effort: zero on
	// any error (in-memory test fs.FS, unsupported filesystem).
	ftimes := probeFileTimes(displayPath)

	// Use the symlink's own Lstat info (rather than the resolved
	// target's) for entries reaching this function as leaves where
	// either the target is missing (broken link) OR the target is a
	// directory. The walker calls BuildAttributesWith on file-like
	// leaf entries — a symlink-to-dir arriving here means the walker
	// is treating it as a leaf (FollowSymlinks=false). Letting
	// fs.Stat resolve and report IsDir=true would surface a dir as
	// a confusing "file" entry; instead we use Lstat so size and
	// mtime reflect the symlink itself.
	useLstatInfo := sym.broken
	if sym.isSymlink && !sym.broken {
		if statInfo, serr := os.Stat(displayPath); serr == nil && statInfo.IsDir() {
			useLstatInfo = true
		}
	}

	var info fs.FileInfo
	if useLstatInfo {
		lstatInfo, lerr := os.Lstat(displayPath)
		if lerr != nil {
			return nil, lerr
		}
		info = lstatInfo
	} else {
		var err error
		info, err = fs.Stat(fsys, fsPath)
		if err != nil {
			return nil, err
		}
	}

	name := info.Name()
	ext := strings.ToLower(filepath.Ext(name))
	dir := filepath.Dir(displayPath)

	// Shared context for the enrichment helpers. cacheKey is threaded
	// separately (it can be cleared mid-build).
	b := &attrBuild{
		ctx: ctx, fsys: fsys, fsPath: fsPath, displayPath: displayPath,
		info: info, sym: sym, ftimes: ftimes, registry: registry, opts: opts,
	}

	// Cache-key conversion: keys are absolute, OS-native paths so two
	// runs that walk the same physical tree under different roots
	// (./docs vs /home/u/proj/docs) hit the same entry. Non-absolute
	// keys (typical only in tests with an in-memory fs.FS and no Root)
	// degrade to "no caching" — Lookup returns miss and Put silently
	// drops via the implementation's filepath.IsAbs guard.
	//
	// SkipAttributesParse bypasses the cache entirely — an entry with
	// no parsed Extra would poison later calls that do want them.
	var cacheKey string
	if opts.Index != nil && !opts.SkipAttributesParse {
		if abs, absErr := filepath.Abs(displayPath); absErr == nil {
			cacheKey = filepath.Clean(abs)
		}
	}

	// Broken-or-dir symlinks bypass the cache entirely — there's no
	// target file content to validate against, and re-resolving on
	// every walk is cheap. (The cache is keyed on file content's
	// (size, mtime); a symlink as a leaf has no content to cache.)
	if opts.Index != nil && cacheKey != "" && !useLstatInfo {
		if cached, ok := opts.Index.Lookup(cacheKey, info.Size(), info.ModTime()); ok {
			attrs := assembleFromCache(name, displayPath, dir, ext, info, cached)
			// Apply the per-walk, never-cached enrichments (body / OCR /
			// C2PA / project / hashes / phash / known-status / similarity /
			// BM25 / disguise / xattr / git / times / symlink).
			b.enrichCacheHit(attrs, cacheKey, cached)
			return attrs, nil
		}
	}

	// Symlinks treated as leaves (broken OR target-is-dir) have no
	// file content to detect against — skip the registry pass and
	// return a minimal record so agents can still find them via
	// is_symlink / is_broken_symlink / target_path.
	if useLstatInfo {
		attrs := &FileAttributes{
			Name:        name,
			Path:        displayPath,
			Dir:         dir,
			Size:        info.Size(),
			Ext:         ext,
			ModTime:     info.ModTime(),
			ContentType: "",
			Extra:       content.Attributes{},
		}
		if opts.GitCache != nil {
			applyGitMeta(attrs, displayPath, opts.GitCache)
		}
		applyFileTimes(attrs, ftimes)
		applySymlinkInfo(attrs, sym)
		return attrs, nil
	}

	var ct content.ContentType
	var magicCT, extCT string
	if opts.CheckDisguised {
		nameType, magicType := registry.DetectBoth(fsys, fsPath)
		ct = nameType
		if ct == nil {
			ct = magicType
		}
		if nameType != nil {
			extCT = nameType.Name()
		}
		if magicType != nil {
			magicCT = magicType.Name()
		}
	} else {
		ct = registry.Detect(fsys, fsPath)
	}
	contentTypeName := ""
	var extra content.Attributes
	if ct != nil {
		contentTypeName = ct.Name()
		// SkipAttributesParse: detect the content-type name only (cheap —
		// extension + magic bytes from the registry) and skip the
		// per-format Attributes parse. Used by ComputeStats when the
		// group_by key is detector-only.
		//
		// SkipAttributesPrefixes is the per-file refinement (#284):
		// when the detected content type matches any prefix, behave
		// like SkipAttributesParse=true for THIS file. Clear cacheKey
		// too so the Put further down doesn't poison the cache with
		// an empty-Extra entry.
		skipForProfile := matchesAttributeSkipPrefix(contentTypeName, opts.SkipAttributesPrefixes)
		if skipForProfile {
			cacheKey = ""
		}
		if !opts.SkipAttributesParse && !skipForProfile {
			a, err := ct.Attributes(ctx, fsys, fsPath)
			switch {
			case err != nil && ctx.Err() != nil:
				// Cancellation (timeout / Ctrl-C) — the whole walk is
				// stopping; propagate so the caller surfaces it.
				return nil, err
			case err != nil:
				// A parse error on a malformed / corrupt file (truncated
				// PDF, non-zip docx, garbage with a valid extension, ...)
				// must NOT drop the file from results — that silently
				// hides exactly the suspicious files a forensic /
				// inventory "match everything" is looking for (#321).
				// Degrade to a basic record: the detected content_type +
				// stat, with no type-specific attributes. Clear cacheKey
				// so the empty parse isn't cached — a later valid version
				// re-parses on its next (size, mtime) change.
				cacheKey = ""
			default:
				extra = a
			}
			extra = applyPathBasedContentHooks(contentTypeName, displayPath, extra)
		}
	}

	// Async store on miss. The implementation handles back-pressure;
	// we never wait for the write. Body is NOT included in the cached
	// Extra — it's read on demand per call (see cache-hit branch
	// above) and would otherwise bloat the index file.
	//
	// cacheEntry is the SINGLE entry the enrichment helpers below
	// (populateHashes / populatePHash / populateSimilarity) merge into:
	// they receive it as their `cached` argument and stamp their own
	// fields onto it before re-Put-ing the same value. Without this,
	// each helper Put a SEPARATE minimal entry to the same key and the
	// last write clobbered the others — dropping the parsed Extra (the
	// embedding-vs-attributes clobber behind issue #306 / Finding #5).
	var cacheEntry *index.Entry
	if opts.Index != nil && cacheKey != "" {
		// Clone the parsed attributes: the helpers below re-Put
		// cacheEntry after `extra` gains the on-demand body and the
		// per-walk project context (neither of which belongs in the
		// (size, mtime) cache), so the cached copy must be decoupled
		// from later mutations to `extra`.
		cacheEntry = &index.Entry{
			Size:                 info.Size(),
			ModTimeUnixNano:      info.ModTime().UnixNano(),
			ContentType:          contentTypeName,
			Extra:                maps.Clone(map[string]any(extra)),
			MagicContentType:     magicCT,
			ExtensionContentType: extCT,
			DisguiseChecked:      opts.CheckDisguised,
		}
		_ = opts.Index.Put(cacheKey, cacheEntry)
	}

	// Add body to the returned Extra (separately from the cached
	// Extra above). CEL evaluation runs against this attrs, so the
	// body needs to be present for `body.contains(...)` /
	// `body.matches(...)` filters to fire.
	//
	// Bodies live in the dedicated bodies_v1 bucket — separate from
	// the attribute Extra (which is what got Put a few lines up).
	// Cache-aware: try LookupBody first; on miss extract + PutBody.
	if opts.IncludeBody && canExtractBody(contentTypeName) {
		body := lookupOrExtractBody(ctx, fsys, fsPath, displayPath, cacheKey, info, contentTypeName, opts)
		if body != "" {
			if extra == nil {
				extra = content.Attributes{}
			}
			extra["body"] = body
		}
	}

	// Image OCR (issue #189). Independent of IncludeBody — passing
	// --ocr alone is enough to populate `body` for screenshots. The
	// runImageOCR helper handles its own body-cache integration; the
	// OCR extras (confidence/language/provider) flow into Extra and
	// get cached via attrs_v1 below.
	if opts.OCRImages && strings.HasPrefix(contentTypeName, "image/") {
		if extra == nil {
			extra = content.Attributes{}
		}
		runImageOCR(ctx, displayPath, cacheKey, info, extra, opts)
	}

	// Verified C2PA (#441). Added AFTER the cache Put above, so the
	// clock-dependent verified result never enters the (size, mtime)
	// attribute cache — recomputed each walk when the flag is set.
	if opts.VerifyC2PA && strings.HasPrefix(contentTypeName, "image/") {
		if v, ok := content.ValidateImageC2PA(ctx, fsys, fsPath, contentTypeName); ok {
			if extra == nil {
				extra = content.Attributes{}
			}
			maps.Copy(extra, v)
		}
	}

	// Project-context resolution. NOT cached in the index — the
	// "containing project" is a directory-tree property, not a
	// per-file one, and would invalidate every time a project root
	// is added or removed elsewhere.
	if opts.ProjectResolver != nil {
		extra = applyProjectContext(extra, opts.ProjectResolver, displayPath)
	}

	attrs := &FileAttributes{
		Name:        name,
		Path:        displayPath,
		Dir:         dir,
		Size:        info.Size(),
		Ext:         ext,
		ModTime:     info.ModTime(),
		ContentType: contentTypeName,
		Extra:       extra,
	}
	setTypeFlags(attrs, contentTypeName)
	// Post-parse enrichments (hashes / phash / known-status / similarity /
	// BM25 / disguise / xattr / git / times / symlink). The cacheEntry is
	// the shared entry the hash/phash/similarity helpers re-Put.
	b.enrichCacheMiss(attrs, cacheKey, cacheEntry, contentTypeName, magicCT, extCT)
	return attrs, nil
}

// applyPathBasedContentHooks enriches extra with attributes that depend on the
// absolute displayPath rather than the fs.FS-relative bytes ContentType.Attributes
// sees: the SQLite curated app name (#177), the path-anchored plist kind (#185),
// the browser vendor for bookmark files (#188), and Apple Live Photo pairing
// (#194). Each fires only for its content type; directory-anchored signals
// (`/LaunchAgents/`, `/Library/Keychains/`, …) are invisible to
// ContentType.Attributes when the search root is narrower than the relevant
// directory. Returns extra, allocating it the first time a hook fires on a nil
// map. Caching is automatic — the enriched extra is what gets Put below; the
// per-file (size, mtime) cache caveat (a deleted Live Photo sibling won't
// invalidate the flag until this file changes) matches the existing precedent.
func applyPathBasedContentHooks(contentTypeName, displayPath string, extra content.Attributes) content.Attributes {
	set := func(k string, v any) {
		if extra == nil {
			extra = content.Attributes{}
		}
		extra[k] = v
	}
	switch contentTypeName {
	case "database/sqlite":
		if name := content.LookupSQLiteAppName(extra, displayPath); name != "" {
			set("sqlite_application_name", name)
		}
	case "system/plist":
		if kind := content.LookupPlistKindFromPath(displayPath); kind != "" {
			set("plist_kind", kind)
		}
	case "browser/bookmarks-chromium", "browser/bookmarks-safari":
		if vendor := content.LookupBrowserVendor(displayPath); vendor != "" {
			set("browser_vendor", vendor)
		}
	case "image/heic":
		if sib, sz, ok := content.FindLivePhotoVideo(displayPath); ok {
			set("is_live_photo", true)
			set("live_photo_video_path", sib)
			set("live_photo_video_size", sz)
		}
	case "video/quicktime":
		// `.mov` detects as video/quicktime (per videotype.go). The
		// IsLivePhotoVideoExt gate guards against future expansions of the
		// quicktime ext set away from .mov.
		if content.IsLivePhotoVideoExt(displayPath) {
			if sib, ok := content.FindLivePhotoImage(displayPath); ok {
				set("is_live_photo_video", true)
				set("live_photo_image_path", sib)
			}
		}
	}
	return extra
}

// matchesAttributeSkipPrefix reports whether contentTypeName starts
// with any of the prefixes. Used to honour
// BuildOptions.SkipAttributesPrefixes per-file. Empty input fast-
// paths to false. Issue #284.
func matchesAttributeSkipPrefix(contentTypeName string, prefixes []string) bool {
	if contentTypeName == "" || len(prefixes) == 0 {
		return false
	}
	for _, p := range prefixes {
		if strings.HasPrefix(contentTypeName, p) {
			return true
		}
	}
	return false
}

// applyGitMeta populates the git_* fields on attrs from the per-walk
// gitmeta cache. Looks up by absolute path; falls back to leaving
// fields at zero when the file isn't tracked (or the displayPath is
// outside the git repo). is_git_tracked / is_git_ignored fire even
// for files without commit history (newly-staged files).
func applyGitMeta(attrs *FileAttributes, displayPath string, cache *gitmeta.Cache) {
	if displayPath == "" || cache == nil {
		return
	}
	abs, err := filepath.Abs(displayPath)
	if err != nil {
		abs = displayPath
	}
	if info, ok := cache.Lookup(abs); ok {
		attrs.GitLastCommitTime = info.LastCommitTime
		attrs.GitLastCommitAuthor = info.LastCommitAuthor
		attrs.GitLastCommitSubject = info.LastCommitSubject
		attrs.GitFirstSeen = info.FirstSeen
		attrs.GitCommitCount = int64(info.CommitCount)
	}
	attrs.IsGitTracked = cache.IsTracked(abs)
	attrs.IsGitIgnored = cache.IsIgnored(abs)
}

// applyXattrs reads extended attributes for the file at displayPath
// (Darwin only; non-Darwin returns empty) and merges them into the
// FileAttributes Extra map. Bool predicates are also surfaced as
// typed struct fields where applicable.
//
// Empty displayPath (archive-walk paths) skip — xattrs require a
// real OS path. Issue #193.
func applyXattrs(attrs *FileAttributes, displayPath string) {
	if displayPath == "" {
		return
	}
	xa := content.ReadXattrs(displayPath)
	if len(xa) == 0 {
		return
	}
	if attrs.Extra == nil {
		attrs.Extra = content.Attributes{}
	}
	for k, v := range xa {
		// Lift the two boolean umbrellas to typed FileAttributes
		// fields so the activation resolver short-circuits on them
		// rather than falling through to the Extra-map lookup.
		switch k {
		case "is_xattr_rich":
			if b, ok := v.(bool); ok {
				attrs.IsXattrRich = b
			}
		case "is_quarantined":
			if b, ok := v.(bool); ok {
				attrs.IsQuarantined = b
			}
		}
		attrs.Extra[k] = v
	}
}

func assembleFromCache(name, displayPath, dir, ext string, info fs.FileInfo, cached *index.Entry) *FileAttributes {
	return AssembleAttributes(name, displayPath, dir, ext, cached.ContentType,
		info.Size(), info.ModTime(), content.Attributes(cached.Extra))
}

// AssembleAttributes builds a *FileAttributes from a previously
// computed (contentType, extra) record + the file's identity
// metadata. Used by archive-walk on cache hits to evaluate CEL
// against cached entry records without re-walking the archive or
// re-running content-type detection.
//
// The returned *FileAttributes has its typed is_* fields set via
// setTypeFlags(contentType), so all the standard CEL predicates
// (is_markdown, is_pdf, …) fire correctly.
func AssembleAttributes(name, displayPath, dir, ext, contentType string, size int64, modTime time.Time, extra content.Attributes) *FileAttributes {
	attrs := &FileAttributes{
		Name:        name,
		Path:        displayPath,
		Dir:         dir,
		Size:        size,
		Ext:         ext,
		ModTime:     modTime,
		ContentType: contentType,
		Extra:       extra,
	}
	setTypeFlags(attrs, contentType)
	return attrs
}
