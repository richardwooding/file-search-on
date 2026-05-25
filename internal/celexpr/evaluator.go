package celexpr

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djherbis/times"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/projecttype"
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
// Adding a new SSG project type in internal/projecttype/builtins.go
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
	IsTOML      bool

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

	// OS-generated system metadata files. Per-type flags
	// (IsDSStore, IsLocalized, IsThumbsDB, IsDesktopIni,
	// IsKDEDirectory) match exact content_type names; family flags
	// (IsMacOSMetadata, IsWindowsMetadata, IsLinuxMetadata,
	// IsSystemMetadata) match content_type prefixes. The OS-specific
	// family AND IsSystemMetadata both fire for any system/<os>-*
	// file — see the family-prefix `if` chain in setTypeFlags.
	IsDSStore         bool
	IsLocalized       bool
	IsThumbsDB        bool
	IsDesktopIni      bool
	IsKDEDirectory    bool
	IsPlist           bool
	IsMacOSMetadata   bool
	IsWindowsMetadata bool
	IsLinuxMetadata   bool
	IsSystemMetadata  bool

	// Disk-image content types. Per-type flags fire on the matched
	// content_type (one of disk-image/dmg, disk-image/iso9660,
	// disk-image/vhd, disk-image/vhdx, disk-image/vmdk,
	// disk-image/qcow2, disk-image/wim); IsDiskImage is the umbrella
	// family flag, set via the disk-image/ prefix block in
	// setTypeFlags.
	IsDMG       bool
	IsISO       bool
	IsVHD       bool
	IsVHDX      bool
	IsVMDK      bool
	IsQCOW2     bool
	IsWIM       bool
	IsDiskImage bool

	// Install-package content types. Per-type flag fires on the
	// matched content_type (install/pkg, install/deb, install/rpm,
	// install/appimage); IsInstallPackage is the umbrella family
	// flag, set via the install/ prefix block in setTypeFlags.
	IsPkg            bool
	IsDeb            bool
	IsRPM            bool
	IsAppImage       bool
	IsInstallPackage bool

	// VM-bytecode content types. Per-type flag fires on the matched
	// content_type (bytecode/jvm, bytecode/python, bytecode/wasm);
	// IsBytecode is the umbrella family flag set via the bytecode/
	// prefix block in setTypeFlags.
	IsClass    bool
	IsPyc      bool
	IsWasm     bool
	IsBytecode bool

	// Science-data content types (issue #158). IsFITS fires on
	// content_type == "science/fits". IsScienceData is the umbrella
	// family flag set via the science/ prefix block in setTypeFlags
	// — positioned to extend over future VOTable / HDF5 / PDS / CDF
	// content types without touching consumers.
	IsFITS        bool
	IsVotable     bool
	IsHDF5        bool
	IsPDS3        bool
	IsPDS4        bool
	IsPDS         bool
	IsCDF         bool
	IsScienceData bool

	// Database content types (issue #170). IsSQLite fires on
	// content_type == "database/sqlite". IsDatabase is the umbrella
	// family flag set via the database/ prefix block in setTypeFlags
	// — positioned to extend over future DuckDB / PostgreSQL-dump /
	// BoltDB content types without touching consumers.
	//
	// SQLite WAL + SHM sidecars (issue #176) are deliberately NOT in
	// the IsSQLite / IsDatabase fold — they're companions to a real
	// database file, not databases themselves. Compose via OR if a
	// query wants the trio: `is_sqlite || is_sqlite_wal || is_sqlite_shm`.
	IsSQLite    bool
	IsSQLiteWAL bool
	IsSQLiteSHM bool
	IsDatabase  bool

	// Browser bookmark content types (issue #188). IsBookmarkFile is
	// the cross-browser family umbrella, populated via the `browser/`
	// prefix block in setTypeFlags.
	IsChromiumBookmarks bool
	IsSafariBookmarks   bool
	IsBookmarkFile      bool

	// Font content types (issue #197). IsFont is the family umbrella
	// (any content_type starting with `font/`); the per-format flags
	// match specific content types. Per-TRAIT predicates
	// (is_variable_font / is_color_font / is_monospace_font /
	// is_italic_font / is_bold_font) come from sfnt parser output and
	// live in Extra — they fall through the activation switch to the
	// Extra map lookup, same shape as is_codesigned (#187).
	IsTTF            bool
	IsOTF            bool
	IsFontCollection bool
	IsWOFF           bool
	IsWOFF2          bool
	IsFont           bool

	// RAW photo content types (issue #196). IsRawPhoto is the umbrella
	// family flag, set via the `image/raw-` prefix block in setTypeFlags;
	// per-format flags fire on the matched content_type (image/raw-cr2,
	// image/raw-nef, …). The shared `image/` prefix also fires IsImage
	// for cross-family queries like `is_image && is_raw_photo`.
	IsCR2 bool
	IsCR3 bool
	IsNEF bool
	IsARW bool
	IsDNG bool
	IsRAF bool
	IsORF bool
	IsRW2 bool
	IsRawPhoto bool

	// Extended attributes (issue #193). Populated by applyXattrs
	// when BuildOptions.ReadExtendedAttributes is set. Darwin-only;
	// other platforms leave both false. IsXattrRich is the umbrella
	// — true when the file has any xattr at all. IsQuarantined fires
	// specifically for the com.apple.quarantine xattr (Gatekeeper's
	// downloaded-from-the-web marker).
	IsXattrRich   bool
	IsQuarantined bool

	// Symlink awareness. IsSymlink fires when os.Lstat reports the
	// entry as a symbolic link (filesystem semantics — not "file that
	// looks like a shortcut"). IsBrokenSymlink fires when the target
	// can't be resolved (dangling link). TargetPath carries the raw
	// link target as recorded on disk (relative or absolute), under
	// the target_path key in Extra.
	IsSymlink       bool
	IsBrokenSymlink bool

	// Forensic-interop hashes (PR #143). Populated when the caller
	// sets BuildOptions.ComputeHashes OR when the file participates
	// in find_duplicates / find_near_duplicates. All three compute
	// in one pass via search.HashFile — io.MultiWriter over md5,
	// sha1, sha256. Empty strings when hashing wasn't requested.
	// Cached in index.Entry.MD5 / SHA1 / Hash (the existing Hash
	// field is sha256); validation is the standard (size, mtime).
	MD5    string
	SHA1   string
	SHA256 string

	// Filesystem-level timestamps (PR #144). Populated by the walker
	// via the djherbis/times wrapper around statx(2) / Stat_t /
	// GetFileTime. CreatedAt is the file's birth time (when the
	// inode was first created on this filesystem). MetadataChangedAt
	// is ctime (last status change — permissions, ownership,
	// hard-link count). Both zero when the filesystem doesn't track
	// them (rare on modern fs: ext4 / APFS / NTFS / btrfs / xfs all
	// do); atime is deliberately not surfaced — modern mounts use
	// relatime / noatime by default and the value is unreliable.
	//
	// IsBtimeAnomaly fires when CreatedAt is non-zero AND
	// CreatedAt > ModTime — the classic "this file was placed here
	// AFTER being modified elsewhere" forensic indicator (copy /
	// restore / planted artefact).
	CreatedAt           time.Time
	MetadataChangedAt   time.Time
	IsBtimeAnomaly      bool

	// Disguise detection (PR #145). Populated when the caller sets
	// BuildOptions.CheckDisguised. MagicContentType is what the
	// file's first 512 bytes look like under magic-byte sniffing
	// alone (registry.DetectBoth's magic-pass result);
	// ExtensionContentType is what the name-based passes (exact-
	// basename + extension) would return. Either may be empty.
	// IsDisguised fires when both are non-empty AND they disagree —
	// the classic "this .txt file contains a PE binary" forensic
	// indicator.
	MagicContentType     string
	ExtensionContentType string
	IsDisguised          bool

	// Hash-allowlist / hash-denylist membership (PR #146). Populated
	// when the caller supplies BuildOptions.Allowlist / Denylist
	// AND ComputeHashes is set so the file's MD5 / SHA1 / SHA256
	// are available to match. False when no list is loaded OR the
	// hash isn't in the list. IsKnownGood + IsKnownBad can both
	// be true on misconfigured pairs — agent's responsibility.
	IsKnownGood bool
	IsKnownBad  bool

	// Similarity is the cosine similarity between this file's body
	// embedding and the search-call's query embedding (issue #151).
	// Populated when BuildOptions.SemanticQuery + Embedder are set;
	// 0 otherwise. Always in [-1, 1] for normalised embeddings;
	// typical thresholds for "related" content are 0.5-0.7.
	Similarity float64

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
		cel.Variable("is_toml", cel.BoolType),

		// Project-context variables (PR #97). Populated by the
		// walker via Options.ResolveProjects = true; empty otherwise.
		cel.Variable("project_types", cel.ListType(cel.StringType)),
		cel.Variable("project_type", cel.StringType),
		// is_static_site fires when the resolved project_type is one
		// of the registered static-site generators (Hugo / Jekyll /
		// Eleventy / Astro / Gatsby / MkDocs / Docusaurus / Pelican).
		// Same opt-in semantics as project_type / project_types —
		// requires ResolveProjects to be enabled.
		cel.Variable("is_static_site", cel.BoolType),

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

		// OS-generated metadata files (system/*). Per-type and
		// family predicates — for any matched file the OS-specific
		// family AND is_system_metadata both fire.
		cel.Variable("is_ds_store", cel.BoolType),
		cel.Variable("is_localized", cel.BoolType),
		cel.Variable("is_thumbs_db", cel.BoolType),
		cel.Variable("is_desktop_ini", cel.BoolType),
		cel.Variable("is_kde_directory", cel.BoolType),
		// Apple property list (issue #185).
		cel.Variable("is_plist", cel.BoolType),
		cel.Variable("plist_format", cel.StringType),
		cel.Variable("plist_root_kind", cel.StringType),
		cel.Variable("plist_kind", cel.StringType),
		cel.Variable("plist_bundle_identifier", cel.StringType),
		cel.Variable("plist_bundle_name", cel.StringType),
		cel.Variable("plist_bundle_version", cel.StringType),
		cel.Variable("plist_bundle_short_version", cel.StringType),
		cel.Variable("plist_executable", cel.StringType),
		cel.Variable("plist_min_os_version", cel.StringType),
		cel.Variable("plist_label", cel.StringType),
		cel.Variable("plist_program", cel.StringType),
		cel.Variable("plist_program_arguments", cel.ListType(cel.StringType)),
		cel.Variable("plist_run_at_load", cel.BoolType),
		cel.Variable("plist_keep_alive", cel.BoolType),
		cel.Variable("is_macos_metadata", cel.BoolType),
		cel.Variable("is_windows_metadata", cel.BoolType),
		cel.Variable("is_linux_metadata", cel.BoolType),
		cel.Variable("is_system_metadata", cel.BoolType),

		// Disk-image content types (per-type + family predicates).
		cel.Variable("is_dmg", cel.BoolType),
		cel.Variable("is_iso", cel.BoolType),
		cel.Variable("is_vhd", cel.BoolType),
		cel.Variable("is_vhdx", cel.BoolType),
		cel.Variable("is_vmdk", cel.BoolType),
		cel.Variable("is_qcow2", cel.BoolType),
		cel.Variable("is_wim", cel.BoolType),
		cel.Variable("is_disk_image", cel.BoolType),
		cel.Variable("disk_image_format", cel.StringType),
		cel.Variable("virtual_size", cel.IntType),
		cel.Variable("disk_type", cel.StringType),
		cel.Variable("volume_label", cel.StringType),
		cel.Variable("disk_image_created_at", cel.TimestampType),
		cel.Variable("cluster_bits", cel.IntType),
		cel.Variable("is_encrypted", cel.BoolType),
		cel.Variable("image_count", cel.IntType),

		// Install-package content types (per-type + family predicates).
		cel.Variable("is_pkg", cel.BoolType),
		cel.Variable("is_deb", cel.BoolType),
		cel.Variable("is_rpm", cel.BoolType),
		cel.Variable("is_appimage", cel.BoolType),
		cel.Variable("is_install_package", cel.BoolType),
		cel.Variable("package_format", cel.StringType),
		cel.Variable("package_name", cel.StringType),
		cel.Variable("package_version", cel.StringType),
		cel.Variable("package_release", cel.StringType),
		cel.Variable("package_arch", cel.StringType),
		cel.Variable("package_kind", cel.StringType),
		cel.Variable("appimage_version", cel.IntType),

		// License detection (populated by repo/license parser).
		cel.Variable("license_id", cel.StringType),

		// Test-file detection (populated by source/* parser).
		cel.Variable("is_test_file", cel.BoolType),

		// Symlink awareness. Populated for every file (not just
		// content-type-specific) by the walker's Lstat pass.
		cel.Variable("is_symlink", cel.BoolType),
		cel.Variable("is_broken_symlink", cel.BoolType),
		cel.Variable("target_path", cel.StringType),

		// VM-bytecode content types (per-type + family predicates +
		// per-format attributes).
		cel.Variable("is_class", cel.BoolType),
		cel.Variable("is_pyc", cel.BoolType),
		cel.Variable("is_wasm", cel.BoolType),
		cel.Variable("is_bytecode", cel.BoolType),
		cel.Variable("bytecode_format", cel.StringType),
		cel.Variable("runtime_version", cel.StringType),
		cel.Variable("class_name", cel.StringType),
		cel.Variable("super_class", cel.StringType),
		cel.Variable("interfaces", cel.ListType(cel.StringType)),
		cel.Variable("method_count", cel.IntType),
		cel.Variable("field_count", cel.IntType),
		cel.Variable("access_flags", cel.ListType(cel.StringType)),
		cel.Variable("python_version", cel.StringType),
		cel.Variable("source_mtime", cel.TimestampType),
		cel.Variable("wasm_version", cel.IntType),
		cel.Variable("section_count", cel.IntType),
		cel.Variable("import_count", cel.IntType),
		cel.Variable("export_count", cel.IntType),

		// Science-data content types (issue #158). Per-type +
		// family predicates plus FITS header attributes. Reuses
		// title (← OBJECT), author (← OBSERVER), and taken_at
		// (← parsed DATE-OBS) across the document / image families.
		cel.Variable("is_fits", cel.BoolType),
		cel.Variable("is_votable", cel.BoolType),
		cel.Variable("is_hdf5", cel.BoolType),
		cel.Variable("is_pds3", cel.BoolType),
		cel.Variable("is_pds4", cel.BoolType),
		cel.Variable("is_pds", cel.BoolType),
		cel.Variable("is_cdf", cel.BoolType),
		cel.Variable("is_science_data", cel.BoolType),
		// Database family (issue #170).
		cel.Variable("is_sqlite", cel.BoolType),
		cel.Variable("is_database", cel.BoolType),
		cel.Variable("database_format", cel.StringType),
		// SQLite WAL + SHM sidecars (issue #176). Predicates do NOT
		// imply is_sqlite / is_database — they're companions to the
		// real database file, not databases themselves.
		cel.Variable("is_sqlite_wal", cel.BoolType),
		cel.Variable("is_sqlite_shm", cel.BoolType),
		// Browser bookmark content types (issue #188).
		cel.Variable("is_bookmark_file", cel.BoolType),
		cel.Variable("is_chromium_bookmarks", cel.BoolType),
		cel.Variable("is_safari_bookmarks", cel.BoolType),
		cel.Variable("bookmark_count", cel.IntType),
		cel.Variable("bookmark_folder_count", cel.IntType),
		cel.Variable("bookmark_folders", cel.ListType(cel.StringType)),
		cel.Variable("bookmark_urls", cel.ListType(cel.StringType)),
		cel.Variable("bookmark_titles", cel.ListType(cel.StringType)),
		cel.Variable("browser_vendor", cel.StringType),
		cel.Variable("bookmark_profile", cel.StringType),
		// Font content types (issue #197).
		cel.Variable("is_font", cel.BoolType),
		cel.Variable("is_ttf", cel.BoolType),
		cel.Variable("is_otf", cel.BoolType),
		cel.Variable("is_font_collection", cel.BoolType),
		cel.Variable("is_woff", cel.BoolType),
		cel.Variable("is_woff2", cel.BoolType),
		cel.Variable("is_variable_font", cel.BoolType),
		cel.Variable("is_color_font", cel.BoolType),
		cel.Variable("is_monospace_font", cel.BoolType),
		cel.Variable("is_italic_font", cel.BoolType),
		cel.Variable("is_bold_font", cel.BoolType),
		cel.Variable("font_format", cel.StringType),
		cel.Variable("font_outline_kind", cel.StringType),
		cel.Variable("font_family", cel.StringType),
		cel.Variable("font_subfamily", cel.StringType),
		cel.Variable("font_full_name", cel.StringType),
		cel.Variable("font_version", cel.StringType),
		cel.Variable("font_postscript_name", cel.StringType),
		cel.Variable("font_manufacturer", cel.StringType),
		cel.Variable("font_designer", cel.StringType),
		cel.Variable("font_license", cel.StringType),
		cel.Variable("font_license_url", cel.StringType),
		cel.Variable("font_typographic_family", cel.StringType),
		cel.Variable("font_weight", cel.IntType),
		cel.Variable("font_width", cel.IntType),
		cel.Variable("font_embedding", cel.StringType),
		cel.Variable("font_panose", cel.StringType),
		cel.Variable("font_unicode_ranges", cel.ListType(cel.StringType)),
		cel.Variable("font_revision", cel.DoubleType),
		cel.Variable("font_units_per_em", cel.IntType),
		cel.Variable("font_mac_style", cel.ListType(cel.StringType)),
		cel.Variable("font_italic_angle", cel.DoubleType),
		cel.Variable("font_glyph_count", cel.IntType),
		cel.Variable("font_axis_count", cel.IntType),
		cel.Variable("font_axes", cel.ListType(cel.StringType)),
		cel.Variable("font_collection_count", cel.IntType),
		cel.Variable("font_collection_families", cel.ListType(cel.StringType)),
		cel.Variable("woff2_total_sfnt_size", cel.IntType),
		cel.Variable("woff2_total_compressed_size", cel.IntType),
		// RAW photo content types (issue #196).
		cel.Variable("is_raw_photo", cel.BoolType),
		cel.Variable("is_cr2", cel.BoolType),
		cel.Variable("is_cr3", cel.BoolType),
		cel.Variable("is_nef", cel.BoolType),
		cel.Variable("is_arw", cel.BoolType),
		cel.Variable("is_dng", cel.BoolType),
		cel.Variable("is_raf", cel.BoolType),
		cel.Variable("is_orf", cel.BoolType),
		cel.Variable("is_rw2", cel.BoolType),
		cel.Variable("raw_kind", cel.StringType),
		cel.Variable("raw_vendor", cel.StringType),
		// Apple Live Photo pairing (issue #194). Surfaces on both
		// sides of the pair via path-based sibling lookup in
		// BuildAttributesWith — no new content type.
		cel.Variable("is_live_photo", cel.BoolType),
		cel.Variable("is_live_photo_video", cel.BoolType),
		cel.Variable("live_photo_video_path", cel.StringType),
		cel.Variable("live_photo_video_size", cel.IntType),
		cel.Variable("live_photo_image_path", cel.StringType),
		// Extended attributes (issue #193). Darwin-only surfacing,
		// gated by BuildOptions.ReadExtendedAttributes opt-in.
		cel.Variable("xattr_keys", cel.ListType(cel.StringType)),
		cel.Variable("xattr_count", cel.IntType),
		cel.Variable("is_xattr_rich", cel.BoolType),
		cel.Variable("is_quarantined", cel.BoolType),
		cel.Variable("quarantine_agent", cel.StringType),
		cel.Variable("quarantine_event_id", cel.StringType),
		cel.Variable("quarantine_source_url", cel.StringType),
		cel.Variable("quarantine_referrer_url", cel.StringType),
		cel.Variable("quarantine_download_date", cel.TimestampType),
		cel.Variable("quarantine_user_approved", cel.BoolType),
		cel.Variable("finder_tags", cel.ListType(cel.StringType)),
		cel.Variable("finder_color", cel.StringType),
		cel.Variable("has_finder_comment", cel.BoolType),
		cel.Variable("sqlite_wal_format_version", cel.IntType),
		cel.Variable("sqlite_wal_page_size", cel.IntType),
		cel.Variable("sqlite_wal_checkpoint_seq", cel.IntType),
		cel.Variable("sqlite_wal_frame_count", cel.IntType),
		cel.Variable("sqlite_wal_byte_order", cel.StringType),
		cel.Variable("sqlite_page_size", cel.IntType),
		cel.Variable("sqlite_format_version", cel.IntType),
		cel.Variable("sqlite_page_count", cel.IntType),
		cel.Variable("sqlite_schema_version", cel.IntType),
		cel.Variable("sqlite_text_encoding", cel.StringType),
		cel.Variable("sqlite_user_version", cel.IntType),
		cel.Variable("sqlite_application_id", cel.IntType),
		// Curated app-name lookup (issue #177).
		cel.Variable("sqlite_application_name", cel.StringType),
		// Schema introspection (sqlite_master walker — follow-up to #174).
		cel.Variable("sqlite_table_count", cel.IntType),
		cel.Variable("sqlite_view_count", cel.IntType),
		cel.Variable("sqlite_index_count", cel.IntType),
		cel.Variable("sqlite_trigger_count", cel.IntType),
		cel.Variable("sqlite_table_names", cel.ListType(cel.StringType)),
		cel.Variable("sqlite_schema_fingerprint", cel.StringType),
		// FTS3/4/5 virtual-table detection (issue #178).
		cel.Variable("sqlite_fts_table_count", cel.IntType),
		cel.Variable("sqlite_fts_table_names", cel.ListType(cel.StringType)),
		cel.Variable("cdf_version", cel.StringType),
		cel.Variable("cdf_encoding", cel.StringType),
		cel.Variable("cdf_majority", cel.StringType),
		cel.Variable("variable_count", cel.IntType),
		cel.Variable("attribute_count", cel.IntType),
		cel.Variable("pds_version", cel.StringType),
		cel.Variable("mission_name", cel.StringType),
		cel.Variable("spacecraft_name", cel.StringType),
		cel.Variable("instrument_name", cel.StringType),
		cel.Variable("target_name", cel.StringType),
		cel.Variable("product_id", cel.StringType),
		cel.Variable("start_time", cel.StringType),
		cel.Variable("hdf5_format_version", cel.IntType),
		cel.Variable("hdf5_size_of_offsets", cel.IntType),
		cel.Variable("hdf5_size_of_lengths", cel.IntType),
		cel.Variable("science_format", cel.StringType),
		cel.Variable("votable_version", cel.StringType),
		cel.Variable("table_count", cel.IntType),
		cel.Variable("total_rows", cel.IntType),
		cel.Variable("field_names", cel.ListType(cel.StringType)),
		cel.Variable("field_units", cel.ListType(cel.StringType)),
		cel.Variable("field_ucds", cel.ListType(cel.StringType)),
		cel.Variable("votable_data_format", cel.StringType),
		cel.Variable("telescope", cel.StringType),
		cel.Variable("instrument", cel.StringType),
		cel.Variable("object", cel.StringType),
		cel.Variable("observer", cel.StringType),
		cel.Variable("date_obs", cel.StringType),
		cel.Variable("exptime", cel.DoubleType),
		cel.Variable("filter", cel.StringType),
		cel.Variable("airmass", cel.DoubleType),
		cel.Variable("ra", cel.DoubleType),
		cel.Variable("dec", cel.DoubleType),
		cel.Variable("bitpix", cel.IntType),
		cel.Variable("naxis", cel.IntType),
		cel.Variable("naxis1", cel.IntType),
		cel.Variable("naxis2", cel.IntType),
		cel.Variable("hdu_count", cel.IntType),
		cel.Variable("fits_kind", cel.StringType),

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
		// Mach-O code signature attributes (issue #187).
		cel.Variable("is_codesigned", cel.BoolType),
		cel.Variable("is_apple_signed", cel.BoolType),
		cel.Variable("is_third_party_signed", cel.BoolType),
		cel.Variable("codesign_identifier", cel.StringType),
		cel.Variable("codesign_team_id", cel.StringType),
		cel.Variable("codesign_hash_type", cel.StringType),
		cel.Variable("codesign_hardened_runtime", cel.BoolType),
		cel.Variable("codesign_library_validation", cel.BoolType),
		cel.Variable("codesign_killed", cel.BoolType),
		cel.Variable("codesign_adhoc", cel.BoolType),
		cel.Variable("entitlements", cel.ListType(cel.StringType)),
		cel.Variable("entitlement_app_sandbox", cel.BoolType),
		cel.Variable("entitlement_full_disk_access", cel.BoolType),
		cel.Variable("entitlement_network_client", cel.BoolType),
		cel.Variable("entitlement_network_server", cel.BoolType),
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
		// Forensic-interop hashes (PR #143). Empty strings when the
		// caller didn't request hashing — CEL filters like
		// `md5 == "..."` simply don't match.
		cel.Variable("md5", cel.StringType),
		cel.Variable("sha1", cel.StringType),
		cel.Variable("sha256", cel.StringType),
		// Filesystem-level timestamps (PR #144). Populated for every
		// real-OS file via djherbis/times; zero on test fs.FS that
		// doesn't expose the underlying inode.
		cel.Variable("created_at", cel.TimestampType),
		cel.Variable("metadata_changed_at", cel.TimestampType),
		// mod_time is the file's last-modified time as reported by
		// os.Stat (filesystem mtime). Always populated for real files
		// regardless of opt-in flags. Useful for time-relative
		// filtering: `mod_time > timestamp("2025-01-01T00:00:00Z")`.
		// The `mod_time` sort key has worked for a long time but the
		// CEL variable was missing until issue #168's preset library
		// surfaced the gap.
		cel.Variable("mod_time", cel.TimestampType),
		cel.Variable("is_btime_anomaly", cel.BoolType),
		// Disguise detection (PR #145). Empty / false when the
		// caller didn't opt in via CheckDisguised.
		cel.Variable("magic_content_type", cel.StringType),
		cel.Variable("extension_content_type", cel.StringType),
		cel.Variable("is_disguised", cel.BoolType),
		// Hash-allowlist / denylist membership (PR #146). False
		// when no list is loaded OR the file's hash isn't in the
		// list. Requires --with-hashes / compute_hashes so the
		// per-file hash trio is computed.
		cel.Variable("is_known_good", cel.BoolType),
		cel.Variable("is_known_bad", cel.BoolType),
		// Semantic similarity (issue #151). Populated when the
		// caller sets SemanticQuery + Embedder; 0 otherwise.
		cel.Variable("similarity", cel.DoubleType),
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
	// ProjectResolver, when set, populates Extra["project_types"]
	// and Extra["project_type"] for each file by walking up the
	// file's directory chain to the nearest project-root indicator.
	// nil disables project-context resolution. Constructed by the
	// walker when search.Options.ResolveProjects is true (one per
	// walk root for multi-root walks).
	ProjectResolver *projecttype.ProjectResolver
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
	Embedder               embed.Embedder
	SemanticQueryEmbedding []float32
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
			// Body lives in the dedicated bodies_v1 bucket — independent
			// of the attribute cache so eviction and per-bucket caps
			// can be tuned separately. Consult the body cache first;
			// on miss, re-extract and async-Put so subsequent calls hit.
			if opts.IncludeBody && canExtractBody(cached.ContentType) {
				body := lookupOrExtractBody(ctx, fsys, fsPath, displayPath, cacheKey, info, cached.ContentType, opts)
				if body != "" {
					if attrs.Extra == nil {
						attrs.Extra = content.Attributes{}
					}
					attrs.Extra["body"] = body
				}
			}
			// Same project-context wiring as the cache-miss path —
			// the index doesn't (and shouldn't) cache project context.
			if opts.ProjectResolver != nil {
				if matches := opts.ProjectResolver.Resolve(displayPath); len(matches) > 0 {
					if attrs.Extra == nil {
						attrs.Extra = content.Attributes{}
					}
					names := make([]string, len(matches))
					for i, m := range matches {
						names[i] = m.Type
					}
					attrs.Extra["project_types"] = names
					attrs.Extra["project_type"] = names[0]
					if anyStaticSite(names) {
						attrs.Extra["is_static_site"] = true
					}
				}
			}
			// Hash trio (PR #143). On cache hit we reuse cached.MD5 /
			// SHA1 / Hash when all three are populated; otherwise
			// compute and re-Put so the next call is free.
			if opts.ComputeHashes {
				populateHashes(ctx, displayPath, cacheKey, info, cached, attrs, opts.Index)
			}
			// Hash-allowlist / -denylist membership (PR #146).
			// Membership depends on the loaded sets, not (size, mtime),
			// so we never cache the resulting flags — just re-check.
			if opts.Allowlist != nil || opts.Denylist != nil {
				applyKnownStatus(attrs, opts)
			}
			// Semantic similarity (issue #151). Cache-aware via
			// cached.Vector; on miss, embed via opts.Embedder.
			if opts.Embedder != nil && len(opts.SemanticQueryEmbedding) > 0 {
				populateSimilarity(ctx, fsys, fsPath, displayPath, cacheKey, info, cached, attrs, opts)
			}
			// Disguise check (PR #145). Reuse cached.MagicContentType /
			// ExtensionContentType when DisguiseChecked is true (older
			// entries lack the marker, in which case we re-detect).
			if opts.CheckDisguised {
				if cached.DisguiseChecked {
					applyDisguise(attrs, cached.MagicContentType, cached.ExtensionContentType)
				} else {
					magicCT, extCT := redetectDisguise(fsys, fsPath, registry)
					applyDisguise(attrs, magicCT, extCT)
					// Backfill cache with the disguise fields so the
					// next walk can serve from cache.
					updated := *cached
					updated.MagicContentType = magicCT
					updated.ExtensionContentType = extCT
					updated.DisguiseChecked = true
					_ = opts.Index.Put(cacheKey, &updated)
				}
			}
			// xattr re-read on cache hit. xattrs can change between
			// walks (user re-tags, OS sets quarantine on first run)
			// independently of (size, mtime), so we don't cache them
			// — re-read on every walk when opted in. Issue #193.
			if opts.ReadExtendedAttributes {
				applyXattrs(attrs, displayPath)
			}
			applyFileTimes(attrs, ftimes)
			applySymlinkInfo(attrs, sym)
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
		if !opts.SkipAttributesParse {
			a, err := ct.Attributes(ctx, fsys, fsPath)
			if err != nil {
				return nil, err
			}
			extra = a
			// Curated SQLite app-name lookup (issue #177). Lives here
			// rather than inside ContentType.Attributes because the
			// path-based registry dimensions (PathContains: "Chrome",
			// "/Library/Keychains/", …) need the absolute displayPath,
			// while ContentType.Attributes is handed only the fs.FS-
			// relative fsPath. Caching is automatic because the
			// enriched `extra` is what gets Put into the index below.
			if contentTypeName == "database/sqlite" {
				if name := content.LookupSQLiteAppName(extra, displayPath); name != "" {
					if extra == nil {
						extra = content.Attributes{}
					}
					extra["sqlite_application_name"] = name
				}
			}
			// Path-based plist_kind override (issue #185). Mirrors the
			// SQLite hook above for the same reason — directory-anchored
			// signals (`/LaunchAgents/`, `/LaunchDaemons/`,
			// `/Preferences/`) are invisible to ContentType.Attributes
			// when the search root is narrower than the relevant
			// directory. Path-based kinds beat the content-based kinds
			// that parsePlist set: a plist under LaunchAgents/ IS a
			// LaunchAgent regardless of what its content claims.
			if contentTypeName == "system/plist" {
				if kind := content.LookupPlistKindFromPath(displayPath); kind != "" {
					if extra == nil {
						extra = content.Attributes{}
					}
					extra["plist_kind"] = kind
				}
			}
			// Browser-vendor lookup for bookmark files (issue #188).
			// Same architecture as the two hooks above — Chromium and
			// Safari forks share the file format; only the absolute
			// path tells us which browser owns the file.
			if contentTypeName == "browser/bookmarks-chromium" ||
				contentTypeName == "browser/bookmarks-safari" {
				if vendor := content.LookupBrowserVendor(displayPath); vendor != "" {
					if extra == nil {
						extra = content.Attributes{}
					}
					extra["browser_vendor"] = vendor
				}
			}
			// Apple Live Photo pairing (issue #194). HEIC still +
			// sibling MOV share the same basename; one extra os.Stat
			// per HEIC / MOV file confirms the pair. Same path-based
			// architecture as the three hooks above. Cache caveat:
			// like the others, the lookup result is cached against
			// THIS file's (size, mtime) — deleting the sibling later
			// won't invalidate the cached `is_live_photo` flag until
			// this file itself changes. Accepted trade-off matching
			// the existing precedent.
			if contentTypeName == "image/heic" {
				if sib, sz, ok := content.FindLivePhotoVideo(displayPath); ok {
					if extra == nil {
						extra = content.Attributes{}
					}
					extra["is_live_photo"] = true
					extra["live_photo_video_path"] = sib
					extra["live_photo_video_size"] = sz
				}
			} else if contentTypeName == "video/quicktime" && content.IsLivePhotoVideoExt(displayPath) {
				// `.mov` detects as video/quicktime (per videotype.go).
				// The IsLivePhotoVideoExt gate guards against future
				// expansions of the quicktime ext set away from .mov.
				if sib, ok := content.FindLivePhotoImage(displayPath); ok {
					if extra == nil {
						extra = content.Attributes{}
					}
					extra["is_live_photo_video"] = true
					extra["live_photo_image_path"] = sib
				}
			}
		}
	}

	// Async store on miss. The implementation handles back-pressure;
	// we never wait for the write. Body is NOT included in the cached
	// Extra — it's read on demand per call (see cache-hit branch
	// above) and would otherwise bloat the index file.
	if opts.Index != nil && cacheKey != "" {
		_ = opts.Index.Put(cacheKey, &index.Entry{
			Size:                 info.Size(),
			ModTimeUnixNano:      info.ModTime().UnixNano(),
			ContentType:          contentTypeName,
			Extra:                map[string]any(extra),
			MagicContentType:     magicCT,
			ExtensionContentType: extCT,
			DisguiseChecked:      opts.CheckDisguised,
		})
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

	// Project-context resolution. NOT cached in the index — the
	// "containing project" is a directory-tree property, not a
	// per-file one, and would invalidate every time a project root
	// is added or removed elsewhere.
	if opts.ProjectResolver != nil {
		if matches := opts.ProjectResolver.Resolve(displayPath); len(matches) > 0 {
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
	// Hash trio (PR #143). Cache-miss path: no cached entry to consult,
	// so populateHashes always computes when ComputeHashes is set. The
	// fresh values flow back into the cache so subsequent walks hit.
	if opts.ComputeHashes {
		populateHashes(ctx, displayPath, cacheKey, info, nil, attrs, opts.Index)
	}
	if opts.Allowlist != nil || opts.Denylist != nil {
		applyKnownStatus(attrs, opts)
	}
	if opts.Embedder != nil && len(opts.SemanticQueryEmbedding) > 0 {
		populateSimilarity(ctx, fsys, fsPath, displayPath, cacheKey, info, nil, attrs, opts)
	}
	if opts.CheckDisguised {
		applyDisguise(attrs, magicCT, extCT)
	}
	if opts.ReadExtendedAttributes {
		applyXattrs(attrs, displayPath)
	}
	applyFileTimes(attrs, ftimes)
	applySymlinkInfo(attrs, sym)
	return attrs, nil
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
	case "toml":
		attrs.IsTOML = true
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
		attrs.IsText = true // LICENSE/LICENCE/COPYING are plain text by convention
	case "repo/changelog":
		attrs.IsChangelog = true
		attrs.IsText = true // bare CHANGELOG/HISTORY (the .md variants are caught by extension)
	case "repo/contributing":
		attrs.IsContributing = true
		attrs.IsText = true // bare CONTRIBUTING (the .md variant is caught by extension)
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
		attrs.IsJSON = true // package.json + package-lock.json are JSON
	case "manifest/cargo":
		attrs.IsCargoManifest = true
		attrs.IsTOML = true // Cargo.toml + Cargo.lock are TOML
	case "manifest/pipfile":
		attrs.IsPipfile = true
		// NOTE: Pipfile is TOML but Pipfile.lock is JSON — until the
		// type splits, neither IsTOML nor IsJSON fires here.
	case "manifest/python-reqs":
		attrs.IsPythonReqs = true
		attrs.IsText = true // requirements.txt is line-oriented plain text
	case "manifest/gemfile":
		attrs.IsGemfile = true
	case "platform/procfile":
		attrs.IsProcfile = true
	case "platform/vagrant":
		attrs.IsVagrantfile = true

	// OS-generated system metadata files (system/<os>-*). Both the
	// OS-specific family flag AND IsSystemMetadata fire — see the
	// independent prefix `if` blocks below.
	case "system/macos-ds-store":
		attrs.IsDSStore = true
	case "system/macos-localized":
		attrs.IsLocalized = true
	case "system/windows-thumbs-db":
		attrs.IsThumbsDB = true
	case "system/windows-desktop-ini":
		attrs.IsDesktopIni = true
	case "system/linux-directory":
		attrs.IsKDEDirectory = true
	case "system/plist":
		attrs.IsPlist = true
		// Plist is Apple-specific by overwhelming convention. The
		// `system/` prefix block below also fires IsSystemMetadata;
		// IsMacOSMetadata fires here explicitly because the content-
		// type name doesn't follow the `system/macos-*` convention.
		attrs.IsMacOSMetadata = true

	// Disk-image content types. Per-type flag fires here; family flag
	// IsDiskImage fires via the disk-image/ prefix block below.
	case "disk-image/dmg":
		attrs.IsDMG = true
	case "disk-image/iso9660":
		attrs.IsISO = true
	case "disk-image/vhd":
		attrs.IsVHD = true
	case "disk-image/vhdx":
		attrs.IsVHDX = true
	case "disk-image/vmdk":
		attrs.IsVMDK = true
	case "disk-image/qcow2":
		attrs.IsQCOW2 = true
	case "disk-image/wim":
		attrs.IsWIM = true

	// Install-package content types. Per-type flag fires here;
	// family flag IsInstallPackage fires via the install/ prefix
	// block below.
	case "install/pkg":
		attrs.IsPkg = true
	case "install/deb":
		attrs.IsDeb = true
	case "install/rpm":
		attrs.IsRPM = true
	case "install/appimage":
		attrs.IsAppImage = true

	// VM-bytecode content types. Per-type flag fires here;
	// family flag IsBytecode fires via the bytecode/ prefix block.
	case "bytecode/jvm":
		attrs.IsClass = true
	case "bytecode/python":
		attrs.IsPyc = true
	case "bytecode/wasm":
		attrs.IsWasm = true

	// Science-data content types (issue #158). Per-type flag fires
	// here; family flag IsScienceData fires via the science/ prefix
	// block below.
	case "science/fits":
		attrs.IsFITS = true
	case "science/votable":
		attrs.IsVotable = true
	case "science/hdf5":
		attrs.IsHDF5 = true
	case "science/pds3":
		attrs.IsPDS3 = true
		attrs.IsPDS = true
	case "science/pds4":
		attrs.IsPDS4 = true
		attrs.IsPDS = true
	case "science/cdf":
		attrs.IsCDF = true
	case "database/sqlite":
		attrs.IsSQLite = true
	case "database/sqlite-wal":
		attrs.IsSQLiteWAL = true
	case "database/sqlite-shm":
		attrs.IsSQLiteSHM = true
	case "browser/bookmarks-chromium":
		attrs.IsChromiumBookmarks = true
	case "browser/bookmarks-safari":
		attrs.IsSafariBookmarks = true
	case "font/ttf":
		attrs.IsTTF = true
	case "font/otf":
		attrs.IsOTF = true
	case "font/collection":
		attrs.IsFontCollection = true
	case "font/woff":
		attrs.IsWOFF = true
	case "font/woff2":
		attrs.IsWOFF2 = true

	// RAW photo content types. Per-format flag fires here; family flag
	// IsRawPhoto + IsImage fire via the `image/raw-` and `image/`
	// prefix blocks below.
	case "image/raw-cr2":
		attrs.IsCR2 = true
	case "image/raw-cr3":
		attrs.IsCR3 = true
	case "image/raw-nef":
		attrs.IsNEF = true
	case "image/raw-arw":
		attrs.IsARW = true
	case "image/raw-dng":
		attrs.IsDNG = true
	case "image/raw-raf":
		attrs.IsRAF = true
	case "image/raw-orf":
		attrs.IsORF = true
	case "image/raw-rw2":
		attrs.IsRW2 = true
	}

	// Family prefix flags. Independent `if` blocks rather than a
	// switch so multiple prefixes can fire for one content type —
	// e.g. system/macos-ds-store sets both IsMacOSMetadata and
	// IsSystemMetadata. The 14 original family prefixes (image,
	// office, audio, video, archive, binary, email, source, notebook,
	// build, repo, ignore, manifest, platform) are mutually
	// non-overlapping, so the refactor from switch is behaviour-
	// preserving for them; the new system/* family is the reason it
	// was needed.
	if strings.HasPrefix(name, "image/") {
		attrs.IsImage = true
	}
	if strings.HasPrefix(name, "office/") {
		attrs.IsOffice = true
	}
	if strings.HasPrefix(name, "audio/") {
		attrs.IsAudio = true
	}
	if strings.HasPrefix(name, "video/") {
		attrs.IsVideo = true
	}
	if strings.HasPrefix(name, "archive/") {
		attrs.IsArchive = true
	}
	if strings.HasPrefix(name, "binary/") {
		attrs.IsBinary = true
	}
	if strings.HasPrefix(name, "email/") {
		attrs.IsEmail = true
	}
	if strings.HasPrefix(name, "source/") {
		attrs.IsSource = true
	}
	if strings.HasPrefix(name, "notebook/") {
		attrs.IsNotebook = true
	}
	if strings.HasPrefix(name, "build/") {
		attrs.IsBuild = true
	}
	if strings.HasPrefix(name, "repo/") {
		attrs.IsRepoMeta = true
	}
	if strings.HasPrefix(name, "ignore/") {
		attrs.IsIgnore = true
	}
	if strings.HasPrefix(name, "manifest/") {
		attrs.IsManifest = true
	}
	if strings.HasPrefix(name, "platform/") {
		attrs.IsPlatform = true
	}
	if strings.HasPrefix(name, "system/macos-") {
		attrs.IsMacOSMetadata = true
	}
	if strings.HasPrefix(name, "system/windows-") {
		attrs.IsWindowsMetadata = true
	}
	if strings.HasPrefix(name, "system/linux-") {
		attrs.IsLinuxMetadata = true
	}
	if strings.HasPrefix(name, "system/") {
		attrs.IsSystemMetadata = true
	}
	if strings.HasPrefix(name, "disk-image/") {
		attrs.IsDiskImage = true
	}
	if strings.HasPrefix(name, "install/") {
		attrs.IsInstallPackage = true
	}
	if strings.HasPrefix(name, "bytecode/") {
		attrs.IsBytecode = true
	}
	if strings.HasPrefix(name, "science/") {
		attrs.IsScienceData = true
	}
	// WAL / SHM sidecars share the `database/` prefix but are explicitly
	// excluded from the umbrella `is_database` (and from `is_sqlite`) —
	// they accompany a database, they aren't one. Issue #176.
	if strings.HasPrefix(name, "database/") && name != "database/sqlite-wal" && name != "database/sqlite-shm" {
		attrs.IsDatabase = true
	}
	if strings.HasPrefix(name, "browser/bookmarks-") {
		attrs.IsBookmarkFile = true
	}
	if strings.HasPrefix(name, "font/") {
		attrs.IsFont = true
	}
	if strings.HasPrefix(name, "image/raw-") {
		attrs.IsRawPhoto = true
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
