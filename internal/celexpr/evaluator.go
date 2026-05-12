package celexpr

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// FileAttributes holds all attributes available in CEL expressions
type FileAttributes struct {
	Name        string
	Path        string
	Dir         string
	Size        int64
	Ext         string
	ModTime     time.Time
	ContentType string
	IsMarkdown  bool
	IsJSON      bool
	IsXML       bool
	IsHTML      bool
	IsPDF       bool
	IsImage     bool
	IsText      bool
	IsCSV       bool
	IsEPUB      bool
	IsOffice    bool
	IsAudio     bool
	IsVideo     bool
	IsArchive   bool
	IsBinary    bool
	IsEmail     bool
	IsSource    bool
	IsNotebook  bool
	IsYAML      bool

	// Exact-name content types (PR #94). Per-type predicates fire for
	// the matching content_type; family predicates (IsBuild,
	// IsRepoMeta, IsIgnore, IsManifest, IsPlatform) fire on the
	// content_type name prefix, mirroring how IsImage / IsAudio etc.
	// are populated for image/* / audio/* families.
	IsDockerfile     bool
	IsMakefile       bool
	IsJustfile       bool
	IsRakefile       bool
	IsBuild          bool
	IsLicense        bool
	IsChangelog      bool
	IsContributing   bool
	IsCodeowners     bool
	IsRepoMeta       bool
	IsGitignore      bool
	IsDockerignore   bool
	IsIgnore         bool
	IsGomod          bool
	IsNodeManifest   bool
	IsCargoManifest  bool
	IsPipfile        bool
	IsPythonReqs     bool
	IsGemfile        bool
	IsManifest       bool
	IsProcfile       bool
	IsVagrantfile    bool
	IsPlatform       bool

	Extra content.Attributes
}

// Evaluator evaluates CEL expressions against file attributes
type Evaluator struct {
	env  *cel.Env
	prog cel.Program
}

// New creates a new evaluator for the given CEL expression
func New(expr string) (*Evaluator, error) {
	opts := []cel.EnvOption{
		cel.Variable("name", cel.StringType),
		cel.Variable("path", cel.StringType),
		cel.Variable("dir", cel.StringType),
		cel.Variable("size", cel.IntType),
		cel.Variable("ext", cel.StringType),
		cel.Variable("content_type", cel.StringType),
		cel.Variable("is_markdown", cel.BoolType),
		cel.Variable("is_json", cel.BoolType),
		cel.Variable("is_xml", cel.BoolType),
		cel.Variable("is_html", cel.BoolType),
		cel.Variable("is_pdf", cel.BoolType),
		cel.Variable("is_image", cel.BoolType),
		cel.Variable("is_text", cel.BoolType),
		cel.Variable("is_csv", cel.BoolType),
		cel.Variable("is_epub", cel.BoolType),
		cel.Variable("is_office", cel.BoolType),
		cel.Variable("is_audio", cel.BoolType),
		cel.Variable("is_video", cel.BoolType),
		cel.Variable("is_archive", cel.BoolType),
		cel.Variable("is_binary", cel.BoolType),
		cel.Variable("is_email", cel.BoolType),
		cel.Variable("is_source", cel.BoolType),
		cel.Variable("is_notebook", cel.BoolType),
		cel.Variable("is_yaml", cel.BoolType),
		cel.Variable("yaml_kind", cel.StringType),
		cel.Variable("yaml_document_count", cel.IntType),

		// Exact-name content types (per-type predicates).
		cel.Variable("is_dockerfile", cel.BoolType),
		cel.Variable("is_makefile", cel.BoolType),
		cel.Variable("is_justfile", cel.BoolType),
		cel.Variable("is_rakefile", cel.BoolType),
		cel.Variable("is_license", cel.BoolType),
		cel.Variable("is_changelog", cel.BoolType),
		cel.Variable("is_contributing", cel.BoolType),
		cel.Variable("is_codeowners", cel.BoolType),
		cel.Variable("is_gitignore", cel.BoolType),
		cel.Variable("is_dockerignore", cel.BoolType),
		cel.Variable("is_gomod", cel.BoolType),
		cel.Variable("is_node_manifest", cel.BoolType),
		cel.Variable("is_cargo_manifest", cel.BoolType),
		cel.Variable("is_pipfile", cel.BoolType),
		cel.Variable("is_python_reqs", cel.BoolType),
		cel.Variable("is_gemfile", cel.BoolType),
		cel.Variable("is_procfile", cel.BoolType),
		cel.Variable("is_vagrantfile", cel.BoolType),

		// Exact-name family predicates.
		cel.Variable("is_build", cel.BoolType),
		cel.Variable("is_repo_meta", cel.BoolType),
		cel.Variable("is_ignore", cel.BoolType),
		cel.Variable("is_manifest", cel.BoolType),
		cel.Variable("is_platform", cel.BoolType),

		// Attributes parsed from exact-name types.
		cel.Variable("module", cel.StringType),
		cel.Variable("go_version", cel.StringType),
		cel.Variable("base_image", cel.StringType),
		cel.Variable("title", cel.StringType),
		cel.Variable("body", cel.StringType),
		cel.Variable("word_count", cel.IntType),
		cel.Variable("line_count", cel.IntType),
		cel.Variable("column_count", cel.IntType),
		cel.Variable("csv_columns", cel.ListType(cel.StringType)),
		cel.Variable("language", cel.StringType),
		cel.Variable("page_count", cel.IntType),
		cel.Variable("author", cel.StringType),
		cel.Variable("root_element", cel.StringType),
		cel.Variable("json_kind", cel.StringType),
		cel.Variable("img_width", cel.IntType),
		cel.Variable("img_height", cel.IntType),
		cel.Variable("camera_make", cel.StringType),
		cel.Variable("camera_model", cel.StringType),
		cel.Variable("lens", cel.StringType),
		cel.Variable("taken_at", cel.TimestampType),
		cel.Variable("orientation", cel.IntType),
		cel.Variable("gps_lat", cel.DoubleType),
		cel.Variable("gps_lon", cel.DoubleType),
		cel.Variable("iso", cel.IntType),
		cel.Variable("focal_length", cel.DoubleType),
		cel.Variable("f_stop", cel.DoubleType),
		cel.Variable("exposure_time", cel.DoubleType),
		cel.Variable("artist", cel.StringType),
		cel.Variable("album", cel.StringType),
		cel.Variable("album_artist", cel.StringType),
		cel.Variable("composer", cel.StringType),
		cel.Variable("year", cel.IntType),
		cel.Variable("track", cel.IntType),
		cel.Variable("genre", cel.StringType),
		cel.Variable("duration", cel.DoubleType),
		cel.Variable("bitrate", cel.IntType),
		cel.Variable("sample_rate", cel.IntType),
		cel.Variable("channels", cel.IntType),
		cel.Variable("bit_depth", cel.IntType),
		cel.Variable("video_codec", cel.StringType),
		cel.Variable("audio_codec", cel.StringType),
		cel.Variable("video_width", cel.IntType),
		cel.Variable("video_height", cel.IntType),
		cel.Variable("frame_rate", cel.DoubleType),
		cel.Variable("rotation", cel.IntType),
		cel.Variable("nominal_bitrate", cel.IntType),
		cel.Variable("color_primaries", cel.StringType),
		cel.Variable("color_transfer", cel.StringType),
		cel.Variable("is_hdr", cel.BoolType),
		cel.Variable("subtitles", cel.BoolType),
		cel.Variable("subtitle_languages", cel.ListType(cel.StringType)),
		cel.Variable("replaygain_track_gain", cel.DoubleType),
		cel.Variable("replaygain_album_gain", cel.DoubleType),
		cel.Variable("entry_count", cel.IntType),
		cel.Variable("uncompressed_size", cel.IntType),
		cel.Variable("top_level_entries", cel.ListType(cel.StringType)),
		cel.Variable("has_root_dir", cel.BoolType),
		cel.Variable("architectures", cel.ListType(cel.StringType)),
		cel.Variable("bitness", cel.IntType),
		cel.Variable("binary_format", cel.StringType),
		cel.Variable("binary_type", cel.StringType),
		cel.Variable("is_dynamically_linked", cel.BoolType),
		cel.Variable("is_stripped", cel.BoolType),
		cel.Variable("entry_point", cel.IntType),
		cel.Variable("email_to", cel.ListType(cel.StringType)),
		cel.Variable("email_cc", cel.ListType(cel.StringType)),
		cel.Variable("email_message_id", cel.StringType),
		cel.Variable("email_in_reply_to", cel.StringType),
		cel.Variable("sent_at", cel.TimestampType),
		cel.Variable("attachment_count", cel.IntType),
		cel.Variable("email_count", cel.IntType),
		cel.Variable("loc", cel.IntType),
		cel.Variable("comment_loc", cel.IntType),
		cel.Variable("blank_loc", cel.IntType),
		cel.Variable("cell_count", cel.IntType),
		cel.Variable("code_cell_count", cel.IntType),
		cel.Variable("markdown_cell_count", cel.IntType),
		cel.Variable("kernel", cel.StringType),
		cel.Variable("frontmatter", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("frontmatter_format", cel.StringType),
		cel.Variable("tags", cel.ListType(cel.StringType)),
		cel.Variable("categories", cel.ListType(cel.StringType)),
		cel.Variable("draft", cel.BoolType),
		cel.Variable("date", cel.TimestampType),
	}
	opts = append(opts, fuzzyFunctions()...)
	opts = append(opts, geoFunctions()...)
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating CEL environment: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling CEL expression: %w", issues.Err())
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating CEL program: %w", err)
	}

	return &Evaluator{env: env, prog: prog}, nil
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
}

// defaultBodyMaxBytes caps the body string supplied to CEL when
// IncludeBody is true and BuildOptions.BodyMaxBytes is unset. 1 MiB
// is plenty for typical text files (markdown posts, source modules,
// JSON manifests) and bounds the worst case on adversarial input.
const defaultBodyMaxBytes = 1 << 20

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
func BuildAttributesWith(ctx context.Context, fsys fs.FS, fsPath, displayPath string, registry *content.Registry, opts BuildOptions) (*FileAttributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := fs.Stat(fsys, fsPath)
	if err != nil {
		return nil, err
	}

	name := info.Name()
	ext := strings.ToLower(filepath.Ext(name))
	dir := filepath.Dir(displayPath)

	// Cache-key conversion: keys are absolute, OS-native paths so two
	// runs that walk the same physical tree under different roots
	// (./docs vs /home/u/proj/docs) hit the same entry. Non-absolute
	// keys (typical only in tests with an in-memory fs.FS and no Root)
	// degrade to "no caching" — Lookup returns miss and Put silently
	// drops via the implementation's filepath.IsAbs guard.
	var cacheKey string
	if opts.Index != nil {
		if abs, absErr := filepath.Abs(displayPath); absErr == nil {
			cacheKey = filepath.Clean(abs)
		}
	}

	if opts.Index != nil && cacheKey != "" {
		if cached, ok := opts.Index.Lookup(cacheKey, info.Size(), info.ModTime()); ok {
			attrs := assembleFromCache(name, displayPath, dir, ext, info, cached)
			// Body is intentionally NOT cached: bodies are large
			// relative to the rest of an Entry, change semantics
			// independently of (size, mtime), and CEL filters that
			// need them want fresh reads. Re-read on cache hit
			// when the caller asked for body.
			if opts.IncludeBody && isTextForBody(cached.ContentType) {
				if body, berr := readBody(ctx, fsys, fsPath, opts.BodyMaxBytes); berr == nil && body != "" {
					if attrs.Extra == nil {
						attrs.Extra = content.Attributes{}
					}
					attrs.Extra["body"] = body
				}
			}
			return attrs, nil
		}
	}

	ct := registry.Detect(fsys, fsPath)
	contentTypeName := ""
	var extra content.Attributes
	if ct != nil {
		contentTypeName = ct.Name()
		extra, err = ct.Attributes(ctx, fsys, fsPath)
		if err != nil {
			return nil, err
		}
	}

	// Async store on miss. The implementation handles back-pressure;
	// we never wait for the write. Body is NOT included in the cached
	// Extra — it's read on demand per call (see cache-hit branch
	// above) and would otherwise bloat the index file.
	if opts.Index != nil && cacheKey != "" {
		_ = opts.Index.Put(cacheKey, &index.Entry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			ContentType:     contentTypeName,
			Extra:           map[string]any(extra),
		})
	}

	// Add body to the returned Extra (separately from the cached
	// Extra above). CEL evaluation runs against this attrs, so the
	// body needs to be present for `body.contains(...)` /
	// `body.matches(...)` filters to fire.
	if opts.IncludeBody && isTextForBody(contentTypeName) {
		if body, berr := readBody(ctx, fsys, fsPath, opts.BodyMaxBytes); berr == nil && body != "" {
			if extra == nil {
				extra = content.Attributes{}
			}
			extra["body"] = body
		}
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
	return attrs, nil
}

// setTypeFlags populates the IsX boolean fields on attrs based on the
// content-type name. Per-type flags (IsMarkdown, IsDockerfile, …)
// match exact names; family flags (IsImage, IsBuild, IsManifest, …)
// match the content_type name prefix. Both can be true for the same
// file — e.g. content_type=build/dockerfile sets IsDockerfile AND
// IsBuild. The single source of truth for type-name → predicate
// mapping; reused by BuildAttributesWith and assembleFromCache.
func setTypeFlags(attrs *FileAttributes, name string) {
	// Exact-name (single-format) content types.
	switch name {
	case "markdown":
		attrs.IsMarkdown = true
	case "json":
		attrs.IsJSON = true
	case "yaml":
		attrs.IsYAML = true
	case "xml":
		attrs.IsXML = true
	case "html":
		attrs.IsHTML = true
	case "pdf":
		attrs.IsPDF = true
	case "text":
		attrs.IsText = true
	case "csv":
		attrs.IsCSV = true
	case "epub":
		attrs.IsEPUB = true

	// Exact-name (repo-files family) — per-type flag PLUS family flag
	// set via the prefix check below.
	case "build/dockerfile":
		attrs.IsDockerfile = true
	case "build/makefile":
		attrs.IsMakefile = true
	case "build/justfile":
		attrs.IsJustfile = true
	case "build/rakefile":
		attrs.IsRakefile = true
	case "repo/license":
		attrs.IsLicense = true
	case "repo/changelog":
		attrs.IsChangelog = true
	case "repo/contributing":
		attrs.IsContributing = true
	case "repo/codeowners":
		attrs.IsCodeowners = true
	case "ignore/git":
		attrs.IsGitignore = true
	case "ignore/docker":
		attrs.IsDockerignore = true
	case "manifest/gomod":
		attrs.IsGomod = true
	case "manifest/node":
		attrs.IsNodeManifest = true
	case "manifest/cargo":
		attrs.IsCargoManifest = true
	case "manifest/pipfile":
		attrs.IsPipfile = true
	case "manifest/python-reqs":
		attrs.IsPythonReqs = true
	case "manifest/gemfile":
		attrs.IsGemfile = true
	case "platform/procfile":
		attrs.IsProcfile = true
	case "platform/vagrant":
		attrs.IsVagrantfile = true
	}

	// Family prefix flags. Existing image/office/audio/etc. plus the
	// five new families (build, repo, ignore, manifest, platform).
	switch {
	case strings.HasPrefix(name, "image/"):
		attrs.IsImage = true
	case strings.HasPrefix(name, "office/"):
		attrs.IsOffice = true
	case strings.HasPrefix(name, "audio/"):
		attrs.IsAudio = true
	case strings.HasPrefix(name, "video/"):
		attrs.IsVideo = true
	case strings.HasPrefix(name, "archive/"):
		attrs.IsArchive = true
	case strings.HasPrefix(name, "binary/"):
		attrs.IsBinary = true
	case strings.HasPrefix(name, "email/"):
		attrs.IsEmail = true
	case strings.HasPrefix(name, "source/"):
		attrs.IsSource = true
	case strings.HasPrefix(name, "notebook/"):
		attrs.IsNotebook = true
	case strings.HasPrefix(name, "build/"):
		attrs.IsBuild = true
	case strings.HasPrefix(name, "repo/"):
		attrs.IsRepoMeta = true
	case strings.HasPrefix(name, "ignore/"):
		attrs.IsIgnore = true
	case strings.HasPrefix(name, "manifest/"):
		attrs.IsManifest = true
	case strings.HasPrefix(name, "platform/"):
		attrs.IsPlatform = true
	}
}

func assembleFromCache(name, displayPath, dir, ext string, info fs.FileInfo, cached *index.Entry) *FileAttributes {
	attrs := &FileAttributes{
		Name:        name,
		Path:        displayPath,
		Dir:         dir,
		Size:        info.Size(),
		Ext:         ext,
		ModTime:     info.ModTime(),
		ContentType: cached.ContentType,
		Extra:       content.Attributes(cached.Extra),
	}
	setTypeFlags(attrs, cached.ContentType)
	return attrs
}
