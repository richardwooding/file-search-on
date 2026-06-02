package celexpr

import (
	"github.com/richardwooding/file-search-on/internal/content"
	"time"
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
	IsTOML      bool

	// Exact-name content types (PR #94). Per-type predicates fire for
	// the matching content_type; family predicates (IsBuild,
	// IsRepoMeta, IsIgnore, IsManifest, IsPlatform) fire on the
	// content_type name prefix, mirroring how IsImage / IsAudio etc.
	// are populated for image/* / audio/* families.
	IsDockerfile    bool
	IsMakefile      bool
	IsJustfile      bool
	IsRakefile      bool
	IsBuild         bool
	IsLicense       bool
	IsChangelog     bool
	IsContributing  bool
	IsCodeowners    bool
	IsRepoMeta      bool
	IsGitignore     bool
	IsDockerignore  bool
	IsIgnore        bool
	IsGomod         bool
	IsNodeManifest  bool
	IsCargoManifest bool
	IsPipfile       bool
	IsPythonReqs    bool
	IsGemfile       bool
	IsManifest      bool
	IsProcfile      bool
	IsVagrantfile   bool
	IsPlatform      bool

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

	// Chat-export content types (issue #214). IsChatExport is the
	// family umbrella, populated via the `chat/` prefix block in
	// setTypeFlags.
	IsChatExport    bool
	IsSlackExport   bool
	IsDiscordExport bool
	IsSignalExport  bool

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
	IsCR2      bool
	IsCR3      bool
	IsNEF      bool
	IsARW      bool
	IsDNG      bool
	IsRAF      bool
	IsORF      bool
	IsRW2      bool
	IsRawPhoto bool

	// 3D model content types (issue #213). Is3DModel is the umbrella
	// family flag, set via the `model3d/` prefix block in setTypeFlags;
	// per-format flags fire on the matched content_type (model3d/stl,
	// model3d/obj, model3d/gltf). Per-mesh attributes (vertex_count,
	// face_count, has_normals, has_textures, materials, bounding_box)
	// come from the parser and live in Extra.
	IsSTL     bool
	IsOBJ     bool
	IsGLTF    bool
	Is3DModel bool

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
	CreatedAt         time.Time
	MetadataChangedAt time.Time
	IsBtimeAnomaly    bool

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

	// Git-aware metadata (issue #271). Populated when the caller sets
	// BuildOptions.GitCache to a *gitmeta.Cache built from the walk
	// root. Zero values everywhere when the walk isn't inside a git
	// working tree, or when the file isn't tracked / matched by git.
	// Time fields are UTC; CommitCount is a churn proxy useful for
	// finding high-touch files; IsGitTracked / IsGitIgnored are the
	// fast boolean predicates.
	GitLastCommitTime    time.Time
	GitLastCommitAuthor  string
	GitLastCommitSubject string
	GitFirstSeen         time.Time
	GitCommitCount       int64
	IsGitTracked         bool
	IsGitIgnored         bool

	Extra content.Attributes
}
