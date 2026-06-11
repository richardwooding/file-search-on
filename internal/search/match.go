package search

import "time"

// Match is one match returned by the `search` tool. Beyond path /
// content_type / size, every CEL-visible attribute is included when the
// matched content type emits it; absent fields are omitted from the JSON
// payload so simple consumers see a compact shape.
type Match struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	// Rank is the computed score from search.Options.RankExpr (issue
	// #168). Omitted when no rank expression was configured.
	Rank float64 `json:"rank,omitempty"`

	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Language string `json:"language,omitempty"`

	WordCount   int64 `json:"word_count,omitempty"`
	LineCount   int64 `json:"line_count,omitempty"`
	PageCount   int64 `json:"page_count,omitempty"`
	ColumnCount int64 `json:"column_count,omitempty"`

	CSVColumns  []string `json:"csv_columns,omitempty"`
	RootElement string   `json:"root_element,omitempty"`
	JSONKind    string   `json:"json_kind,omitempty"`

	YAMLKind          string `json:"yaml_kind,omitempty"`
	YAMLDocumentCount int64  `json:"yaml_document_count,omitempty"`

	ImgWidth  int64 `json:"img_width,omitempty"`
	ImgHeight int64 `json:"img_height,omitempty"`

	// C2PA / Content Credentials (#374) — the file's CLAIMED, unverified
	// provenance (we read the embedded JUMBF manifest without validating
	// its signature). IsC2PA marks presence; the rest mirror the claim.
	IsC2PA             bool   `json:"is_c2pa,omitempty"`
	C2PAClaimGenerator string `json:"c2pa_claim_generator,omitempty"`
	C2PATitle          string `json:"c2pa_title,omitempty"`
	C2PAFormat         string `json:"c2pa_format,omitempty"`
	C2PAAIGenerated    bool   `json:"c2pa_ai_generated,omitempty"`
	C2PASignedBy       string `json:"c2pa_signed_by,omitempty"`
	C2PASignedAt       string `json:"c2pa_signed_at,omitempty"` // RFC3339 when set

	CameraMake   string  `json:"camera_make,omitempty"`
	CameraModel  string  `json:"camera_model,omitempty"`
	Lens         string  `json:"lens,omitempty"`
	TakenAt      string  `json:"taken_at,omitempty"` // RFC3339 when set
	Orientation  int64   `json:"orientation,omitempty"`
	GPSLat       float64 `json:"gps_lat,omitempty"`
	GPSLon       float64 `json:"gps_lon,omitempty"`
	ISO          int64   `json:"iso,omitempty"`
	FocalLength  float64 `json:"focal_length,omitempty"`
	FStop        float64 `json:"f_stop,omitempty"`
	ExposureTime float64 `json:"exposure_time,omitempty"`

	FrontmatterFormat string         `json:"frontmatter_format,omitempty"`
	Frontmatter       map[string]any `json:"frontmatter,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	Categories        []string       `json:"categories,omitempty"`
	Draft             bool           `json:"draft,omitempty"`
	Date              string         `json:"date,omitempty"` // RFC3339 when set

	IsMarkdown bool `json:"is_markdown,omitempty"`
	IsJSON     bool `json:"is_json,omitempty"`
	IsXML      bool `json:"is_xml,omitempty"`
	IsHTML     bool `json:"is_html,omitempty"`
	IsPDF      bool `json:"is_pdf,omitempty"`
	IsImage    bool `json:"is_image,omitempty"`
	IsText     bool `json:"is_text,omitempty"`
	IsCSV      bool `json:"is_csv,omitempty"`
	IsEPUB     bool `json:"is_epub,omitempty"`
	IsOffice   bool `json:"is_office,omitempty"`
	IsAudio    bool `json:"is_audio,omitempty"`
	IsVideo    bool `json:"is_video,omitempty"`
	IsArchive  bool `json:"is_archive,omitempty"`
	IsBinary   bool `json:"is_binary,omitempty"`
	IsEmail    bool `json:"is_email,omitempty"`
	IsSource   bool `json:"is_source,omitempty"`
	IsNotebook bool `json:"is_notebook,omitempty"`
	IsYAML     bool `json:"is_yaml,omitempty"`
	IsTOML     bool `json:"is_toml,omitempty"`

	// Project context. Populated only when the walker was invoked
	// with Options.ResolveProjects (CLI --resolve-projects, MCP
	// resolve_projects=true). Empty for files outside any project
	// root or when resolution wasn't requested.
	ProjectTypes []string `json:"project_types,omitempty"`
	ProjectType  string   `json:"project_type,omitempty"`
	// IsStaticSite fires when the resolved project_type is one of the
	// recognised static-site generators (hugo / jekyll / eleventy /
	// astro / gatsby / mkdocs / docusaurus / pelican). Same opt-in
	// semantics as ProjectType — requires the walker's
	// ResolveProjects flag.
	IsStaticSite bool `json:"is_static_site,omitempty"`

	// Exact-name content types (PR #94). Per-type predicates plus
	// family predicates (IsBuild, IsRepoMeta, IsIgnore, IsManifest,
	// IsPlatform) coexist, mirroring how is_image / is_audio etc.
	// fire alongside content_type.
	IsDockerfile    bool `json:"is_dockerfile,omitempty"`
	IsMakefile      bool `json:"is_makefile,omitempty"`
	IsJustfile      bool `json:"is_justfile,omitempty"`
	IsRakefile      bool `json:"is_rakefile,omitempty"`
	IsLicense       bool `json:"is_license,omitempty"`
	IsChangelog     bool `json:"is_changelog,omitempty"`
	IsContributing  bool `json:"is_contributing,omitempty"`
	IsCodeowners    bool `json:"is_codeowners,omitempty"`
	IsGitignore     bool `json:"is_gitignore,omitempty"`
	IsDockerignore  bool `json:"is_dockerignore,omitempty"`
	IsGomod         bool `json:"is_gomod,omitempty"`
	IsNodeManifest  bool `json:"is_node_manifest,omitempty"`
	IsCargoManifest bool `json:"is_cargo_manifest,omitempty"`
	IsPipfile       bool `json:"is_pipfile,omitempty"`
	IsPythonReqs    bool `json:"is_python_reqs,omitempty"`
	IsGemfile       bool `json:"is_gemfile,omitempty"`
	IsProcfile      bool `json:"is_procfile,omitempty"`
	IsVagrantfile   bool `json:"is_vagrantfile,omitempty"`
	IsBuild         bool `json:"is_build,omitempty"`
	IsRepoMeta      bool `json:"is_repo_meta,omitempty"`
	IsIgnore        bool `json:"is_ignore,omitempty"`
	IsManifest      bool `json:"is_manifest,omitempty"`
	IsPlatform      bool `json:"is_platform,omitempty"`

	// OS-generated system metadata files. Per-type plus OS-specific
	// family plus the cross-OS IsSystemMetadata family.
	IsDSStore         bool `json:"is_ds_store,omitempty"`
	IsLocalized       bool `json:"is_localized,omitempty"`
	IsThumbsDB        bool `json:"is_thumbs_db,omitempty"`
	IsDesktopIni      bool `json:"is_desktop_ini,omitempty"`
	IsKDEDirectory    bool `json:"is_kde_directory,omitempty"`
	IsMacOSMetadata   bool `json:"is_macos_metadata,omitempty"`
	IsWindowsMetadata bool `json:"is_windows_metadata,omitempty"`
	IsLinuxMetadata   bool `json:"is_linux_metadata,omitempty"`
	IsSystemMetadata  bool `json:"is_system_metadata,omitempty"`

	// Science-data family. Per-type predicates plus the umbrella
	// IsScienceData family flag.
	IsFITS        bool `json:"is_fits,omitempty"`
	IsVotable     bool `json:"is_votable,omitempty"`
	IsHDF5        bool `json:"is_hdf5,omitempty"`
	IsPDS3        bool `json:"is_pds3,omitempty"`
	IsPDS4        bool `json:"is_pds4,omitempty"`
	IsPDS         bool `json:"is_pds,omitempty"`
	IsCDF         bool `json:"is_cdf,omitempty"`
	IsScienceData bool `json:"is_science_data,omitempty"`

	// Database family (issue #170). IsSQLite per-type, IsDatabase
	// umbrella.
	IsSQLite   bool `json:"is_sqlite,omitempty"`
	IsDatabase bool `json:"is_database,omitempty"`

	// Disk-image family. Per-type predicates plus the umbrella
	// IsDiskImage family flag.
	IsDMG       bool `json:"is_dmg,omitempty"`
	IsISO       bool `json:"is_iso,omitempty"`
	IsVHD       bool `json:"is_vhd,omitempty"`
	IsVHDX      bool `json:"is_vhdx,omitempty"`
	IsVMDK      bool `json:"is_vmdk,omitempty"`
	IsQCOW2     bool `json:"is_qcow2,omitempty"`
	IsWIM       bool `json:"is_wim,omitempty"`
	IsDiskImage bool `json:"is_disk_image,omitempty"`

	// Install-package family. Per-type predicates plus the umbrella
	// IsInstallPackage family flag.
	IsPkg            bool `json:"is_pkg,omitempty"`
	IsDeb            bool `json:"is_deb,omitempty"`
	IsRPM            bool `json:"is_rpm,omitempty"`
	IsAppImage       bool `json:"is_appimage,omitempty"`
	IsInstallPackage bool `json:"is_install_package,omitempty"`

	// IsTestFile fires for source files whose basename matches the
	// per-language test convention.
	IsTestFile bool `json:"is_test_file,omitempty"`

	// Symlink awareness. is_symlink fires when os.Lstat reports the
	// entry as a symbolic link; target_path carries the raw link
	// target as recorded on disk; is_broken_symlink fires when the
	// target can't be resolved.
	IsSymlink       bool   `json:"is_symlink,omitempty"`
	IsBrokenSymlink bool   `json:"is_broken_symlink,omitempty"`
	TargetPath      string `json:"target_path,omitempty"`

	// Mach-O code signature (issue #187). Populated for binary/mach-o
	// files; ELF / PE leave these empty. is_apple_signed and
	// is_third_party_signed are derived predicates from team_id +
	// adhoc, not their own fields in the signature.
	IsCodesigned              bool     `json:"is_codesigned,omitempty"`
	IsAppleSigned             bool     `json:"is_apple_signed,omitempty"`
	IsThirdPartySigned        bool     `json:"is_third_party_signed,omitempty"`
	CodesignIdentifier        string   `json:"codesign_identifier,omitempty"`
	CodesignTeamID            string   `json:"codesign_team_id,omitempty"`
	CodesignHashType          string   `json:"codesign_hash_type,omitempty"`
	CodesignHardenedRuntime   bool     `json:"codesign_hardened_runtime,omitempty"`
	CodesignLibraryValidation bool     `json:"codesign_library_validation,omitempty"`
	CodesignKilled            bool     `json:"codesign_killed,omitempty"`
	CodesignAdhoc             bool     `json:"codesign_adhoc,omitempty"`
	Entitlements              []string `json:"entitlements,omitempty"`
	EntitlementAppSandbox     bool     `json:"entitlement_app_sandbox,omitempty"`
	EntitlementFullDiskAccess bool     `json:"entitlement_full_disk_access,omitempty"`
	EntitlementNetworkClient  bool     `json:"entitlement_network_client,omitempty"`
	EntitlementNetworkServer  bool     `json:"entitlement_network_server,omitempty"`

	// Apple property list (issue #185). is_plist + 14 typed attributes
	// pulled from CFBundle* / LaunchAgent / LaunchDaemon keys.
	IsPlist                 bool     `json:"is_plist,omitempty"`
	PlistFormat             string   `json:"plist_format,omitempty"`
	PlistRootKind           string   `json:"plist_root_kind,omitempty"`
	PlistKind               string   `json:"plist_kind,omitempty"`
	PlistBundleIdentifier   string   `json:"plist_bundle_identifier,omitempty"`
	PlistBundleName         string   `json:"plist_bundle_name,omitempty"`
	PlistBundleVersion      string   `json:"plist_bundle_version,omitempty"`
	PlistBundleShortVersion string   `json:"plist_bundle_short_version,omitempty"`
	PlistExecutable         string   `json:"plist_executable,omitempty"`
	PlistMinOSVersion       string   `json:"plist_min_os_version,omitempty"`
	PlistLabel              string   `json:"plist_label,omitempty"`
	PlistProgram            string   `json:"plist_program,omitempty"`
	PlistProgramArguments   []string `json:"plist_program_arguments,omitempty"`
	PlistRunAtLoad          bool     `json:"plist_run_at_load,omitempty"`
	PlistKeepAlive          bool     `json:"plist_keep_alive,omitempty"`

	// VM-bytecode family. Per-type predicates + umbrella, plus the
	// per-format attribute surface.
	IsClass    bool `json:"is_class,omitempty"`
	IsPyc      bool `json:"is_pyc,omitempty"`
	IsWasm     bool `json:"is_wasm,omitempty"`
	IsBytecode bool `json:"is_bytecode,omitempty"`

	BytecodeFormat string   `json:"bytecode_format,omitempty"`
	RuntimeVersion string   `json:"runtime_version,omitempty"`
	ClassName      string   `json:"class_name,omitempty"`
	SuperClass     string   `json:"super_class,omitempty"`
	Interfaces     []string `json:"interfaces,omitempty"`
	MethodCount    int64    `json:"method_count,omitempty"`
	FieldCount     int64    `json:"field_count,omitempty"`
	AccessFlags    []string `json:"access_flags,omitempty"`
	PythonVersion  string   `json:"python_version,omitempty"`
	SourceMtime    string   `json:"source_mtime,omitempty"` // RFC3339 when set
	WasmVersion    int64    `json:"wasm_version,omitempty"`
	SectionCount   int64    `json:"section_count,omitempty"`
	ImportCount    int64    `json:"import_count,omitempty"`
	ExportCount    int64    `json:"export_count,omitempty"`

	// Science-data family attributes (issue #158). ScienceFormat is
	// the umbrella discriminator (`"fits"` today); FITS-specific
	// fields surface alongside. OBJECT / OBSERVER / DATE-OBS also
	// populate the shared Title / Author / TakenAt fields above.
	ScienceFormat string  `json:"science_format,omitempty"`
	Telescope     string  `json:"telescope,omitempty"`
	Instrument    string  `json:"instrument,omitempty"`
	Object        string  `json:"object,omitempty"`
	Observer      string  `json:"observer,omitempty"`
	DateObs       string  `json:"date_obs,omitempty"`
	Exptime       float64 `json:"exptime,omitempty"`
	Filter        string  `json:"filter,omitempty"`
	Airmass       float64 `json:"airmass,omitempty"`
	RA            float64 `json:"ra,omitempty"`
	Dec           float64 `json:"dec,omitempty"`
	Bitpix        int64   `json:"bitpix,omitempty"`
	Naxis         int64   `json:"naxis,omitempty"`
	Naxis1        int64   `json:"naxis1,omitempty"`
	Naxis2        int64   `json:"naxis2,omitempty"`
	HDUCount      int64   `json:"hdu_count,omitempty"`
	FITSKind      string  `json:"fits_kind,omitempty"`

	// VOTable (issue #160). VOTableVersion / TableCount / TotalRows
	// always populated for matched VOTable files; the FIELD parallel
	// lists only when at least one FIELD is declared.
	VOTableVersion    string   `json:"votable_version,omitempty"`
	TableCount        int64    `json:"table_count,omitempty"`
	TotalRows         int64    `json:"total_rows,omitempty"`
	FieldNames        []string `json:"field_names,omitempty"`
	FieldUnits        []string `json:"field_units,omitempty"`
	FieldUCDs         []string `json:"field_ucds,omitempty"`
	VOTableDataFormat string   `json:"votable_data_format,omitempty"`

	// HDF5 superblock attributes (issue #161). Always populated
	// when IsHDF5 fires; the v1 scope is superblock-only — the
	// recursive hierarchy walk (group_count / dataset_count /
	// top_level_groups) is deferred to a follow-up.
	HDF5FormatVersion int64 `json:"hdf5_format_version,omitempty"`
	HDF5SizeOfOffsets int64 `json:"hdf5_size_of_offsets,omitempty"`
	HDF5SizeOfLengths int64 `json:"hdf5_size_of_lengths,omitempty"`

	// PDS attributes (issue #162). Shared across PDS3 PVL + PDS4
	// XML variants. pds_version distinguishes between them.
	PDSVersion     string `json:"pds_version,omitempty"`
	MissionName    string `json:"mission_name,omitempty"`
	SpacecraftName string `json:"spacecraft_name,omitempty"`
	InstrumentName string `json:"instrument_name,omitempty"`
	TargetName     string `json:"target_name,omitempty"`
	ProductID      string `json:"product_id,omitempty"`
	StartTime      string `json:"start_time,omitempty"`

	// CDF attributes (issue #163). NASA Common Data Format —
	// heliophysics time-series. variable_count + attribute_count
	// may be 0 / absent when the GDR is beyond the read cap on a
	// non-seekable fs.FS.
	CDFVersion     string `json:"cdf_version,omitempty"`
	CDFEncoding    string `json:"cdf_encoding,omitempty"`
	CDFMajority    string `json:"cdf_majority,omitempty"`
	VariableCount  int64  `json:"variable_count,omitempty"`
	AttributeCount int64  `json:"attribute_count,omitempty"`

	// Database family (issue #170). SQLite header-only attributes
	// for v1; schema introspection deferred to a follow-up.
	DatabaseFormat        string `json:"database_format,omitempty"`
	SQLitePageSize        int64  `json:"sqlite_page_size,omitempty"`
	SQLiteFormatVersion   int64  `json:"sqlite_format_version,omitempty"`
	SQLitePageCount       int64  `json:"sqlite_page_count,omitempty"`
	SQLiteSchemaVersion   int64  `json:"sqlite_schema_version,omitempty"`
	SQLiteTextEncoding    string `json:"sqlite_text_encoding,omitempty"`
	SQLiteUserVersion     int64  `json:"sqlite_user_version,omitempty"`
	SQLiteApplicationID   int64  `json:"sqlite_application_id,omitempty"`
	SQLiteApplicationName string `json:"sqlite_application_name,omitempty"`

	// SQLite schema introspection (follow-up to #174). Populated by
	// the hand-rolled sqlite_master b-tree walker — see
	// internal/content/database_sqlite_btree.go.
	SQLiteTableCount        int64    `json:"sqlite_table_count,omitempty"`
	SQLiteViewCount         int64    `json:"sqlite_view_count,omitempty"`
	SQLiteIndexCount        int64    `json:"sqlite_index_count,omitempty"`
	SQLiteTriggerCount      int64    `json:"sqlite_trigger_count,omitempty"`
	SQLiteTableNames        []string `json:"sqlite_table_names,omitempty"`
	SQLiteSchemaFingerprint string   `json:"sqlite_schema_fingerprint,omitempty"`

	// FTS3 / FTS4 / FTS5 virtual-table detection (issue #178). Pair
	// with `--body` / include_body to grep inside FTS-indexed text.
	SQLiteFTSTableCount int64    `json:"sqlite_fts_table_count,omitempty"`
	SQLiteFTSTableNames []string `json:"sqlite_fts_table_names,omitempty"`

	// SQLite WAL sidecar (issue #176). Populated when content_type ==
	// "database/sqlite-wal" — parsed from the 32-byte WAL header.
	SQLiteWALFormatVersion int64  `json:"sqlite_wal_format_version,omitempty"`
	SQLiteWALPageSize      int64  `json:"sqlite_wal_page_size,omitempty"`
	SQLiteWALCheckpointSeq int64  `json:"sqlite_wal_checkpoint_seq,omitempty"`
	SQLiteWALFrameCount    int64  `json:"sqlite_wal_frame_count,omitempty"`
	SQLiteWALByteOrder     string `json:"sqlite_wal_byte_order,omitempty"`

	// Extended attributes (issue #193). Populated when the caller
	// opts in via --with-xattrs / with_xattrs. Darwin-only; non-
	// Darwin walks leave these empty.
	IsXattrRich            bool      `json:"is_xattr_rich,omitempty"`
	IsQuarantined          bool      `json:"is_quarantined,omitempty"`
	XattrKeys              []string  `json:"xattr_keys,omitempty"`
	XattrCount             int64     `json:"xattr_count,omitempty"`
	QuarantineAgent        string    `json:"quarantine_agent,omitempty"`
	QuarantineEventID      string    `json:"quarantine_event_id,omitempty"`
	QuarantineSourceURL    string    `json:"quarantine_source_url,omitempty"`
	QuarantineReferrerURL  string    `json:"quarantine_referrer_url,omitempty"`
	QuarantineDownloadDate time.Time `json:"quarantine_download_date,omitzero"`
	QuarantineUserApproved bool      `json:"quarantine_user_approved,omitempty"`
	FinderTags             []string  `json:"finder_tags,omitempty"`
	FinderColor            string    `json:"finder_color,omitempty"`
	HasFinderComment       bool      `json:"has_finder_comment,omitempty"`

	// Browser bookmarks (issue #188).
	IsBookmarkFile      bool     `json:"is_bookmark_file,omitempty"`
	IsChromiumBookmarks bool     `json:"is_chromium_bookmarks,omitempty"`
	IsSafariBookmarks   bool     `json:"is_safari_bookmarks,omitempty"`
	BookmarkCount       int64    `json:"bookmark_count,omitempty"`
	BookmarkFolderCount int64    `json:"bookmark_folder_count,omitempty"`
	BookmarkFolders     []string `json:"bookmark_folders,omitempty"`
	BookmarkURLs        []string `json:"bookmark_urls,omitempty"`
	BookmarkTitles      []string `json:"bookmark_titles,omitempty"`
	BrowserVendor       string   `json:"browser_vendor,omitempty"`
	BookmarkProfile     string   `json:"bookmark_profile,omitempty"`

	// Chat exports (issue #214).
	IsChatExport     bool     `json:"is_chat_export,omitempty"`
	IsSlackExport    bool     `json:"is_slack_export,omitempty"`
	IsDiscordExport  bool     `json:"is_discord_export,omitempty"`
	IsSignalExport   bool     `json:"is_signal_export,omitempty"`
	ChatMessageCount int64    `json:"chat_message_count,omitempty"`
	ChatParticipants []string `json:"chat_participants,omitempty"`
	ChatChannel      string   `json:"chat_channel,omitempty"`
	ChatWorkspace    string   `json:"chat_workspace,omitempty"`
	ChatStartAt      string   `json:"chat_start_at,omitempty"` // RFC3339 when set
	ChatEndAt        string   `json:"chat_end_at,omitempty"`   // RFC3339 when set

	// Font content types (issue #197).
	IsFont                   bool     `json:"is_font,omitempty"`
	IsTTF                    bool     `json:"is_ttf,omitempty"`
	IsOTF                    bool     `json:"is_otf,omitempty"`
	IsFontCollection         bool     `json:"is_font_collection,omitempty"`
	IsWOFF                   bool     `json:"is_woff,omitempty"`
	IsWOFF2                  bool     `json:"is_woff2,omitempty"`
	IsVariableFont           bool     `json:"is_variable_font,omitempty"`
	IsColorFont              bool     `json:"is_color_font,omitempty"`
	IsMonospaceFont          bool     `json:"is_monospace_font,omitempty"`
	IsItalicFont             bool     `json:"is_italic_font,omitempty"`
	IsBoldFont               bool     `json:"is_bold_font,omitempty"`
	FontFormat               string   `json:"font_format,omitempty"`
	FontOutlineKind          string   `json:"font_outline_kind,omitempty"`
	FontFamily               string   `json:"font_family,omitempty"`
	FontSubfamily            string   `json:"font_subfamily,omitempty"`
	FontFullName             string   `json:"font_full_name,omitempty"`
	FontVersion              string   `json:"font_version,omitempty"`
	FontPostScriptName       string   `json:"font_postscript_name,omitempty"`
	FontManufacturer         string   `json:"font_manufacturer,omitempty"`
	FontDesigner             string   `json:"font_designer,omitempty"`
	FontLicense              string   `json:"font_license,omitempty"`
	FontLicenseURL           string   `json:"font_license_url,omitempty"`
	FontTypographicFamily    string   `json:"font_typographic_family,omitempty"`
	FontWeight               int64    `json:"font_weight,omitempty"`
	FontWidth                int64    `json:"font_width,omitempty"`
	FontEmbedding            string   `json:"font_embedding,omitempty"`
	FontPanose               string   `json:"font_panose,omitempty"`
	FontUnicodeRanges        []string `json:"font_unicode_ranges,omitempty"`
	FontRevision             float64  `json:"font_revision,omitempty"`
	FontUnitsPerEm           int64    `json:"font_units_per_em,omitempty"`
	FontMacStyle             []string `json:"font_mac_style,omitempty"`
	FontItalicAngle          float64  `json:"font_italic_angle,omitempty"`
	FontGlyphCount           int64    `json:"font_glyph_count,omitempty"`
	FontAxisCount            int64    `json:"font_axis_count,omitempty"`
	FontAxes                 []string `json:"font_axes,omitempty"`
	FontCollectionCount      int64    `json:"font_collection_count,omitempty"`
	FontCollectionFamilies   []string `json:"font_collection_families,omitempty"`
	WOFF2TotalSfntSize       int64    `json:"woff2_total_sfnt_size,omitempty"`
	WOFF2TotalCompressedSize int64    `json:"woff2_total_compressed_size,omitempty"`

	// Install-package attributes.
	PackageFormat   string `json:"package_format,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	PackageVersion  string `json:"package_version,omitempty"`
	PackageRelease  string `json:"package_release,omitempty"`
	PackageArch     string `json:"package_arch,omitempty"`
	PackageKind     string `json:"package_kind,omitempty"`
	AppImageVersion int64  `json:"appimage_version,omitempty"`

	// SPDX license id detected by scanning LICENSE-shaped files.
	LicenseID string `json:"license_id,omitempty"`

	// Disk-image attributes. disk_image_format + virtual_size are
	// populated for every matched disk-image file; the others are
	// per-format (disk_type for VHD/VMDK, volume_label for ISO,
	// disk_image_created_at for VHD/ISO, cluster_bits +
	// is_encrypted for QCOW2, image_count for WIM).
	DiskImageFormat    string `json:"disk_image_format,omitempty"`
	VirtualSize        int64  `json:"virtual_size,omitempty"`
	DiskType           string `json:"disk_type,omitempty"`
	VolumeLabel        string `json:"volume_label,omitempty"`
	DiskImageCreatedAt string `json:"disk_image_created_at,omitempty"` // RFC3339 when set
	ClusterBits        int64  `json:"cluster_bits,omitempty"`
	IsEncrypted        bool   `json:"is_encrypted,omitempty"`
	ImageCount         int64  `json:"image_count,omitempty"`

	Module    string `json:"module,omitempty"`
	GoVersion string `json:"go_version,omitempty"`
	BaseImage string `json:"base_image,omitempty"`

	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Composer    string `json:"composer,omitempty"`
	Year        int64  `json:"year,omitempty"`
	Track       int64  `json:"track,omitempty"`
	Genre       string `json:"genre,omitempty"`

	Duration       float64 `json:"duration,omitempty"`
	Bitrate        int64   `json:"bitrate,omitempty"`
	NominalBitrate int64   `json:"nominal_bitrate,omitempty"`
	SampleRate     int64   `json:"sample_rate,omitempty"`
	Channels       int64   `json:"channels,omitempty"`
	BitDepth       int64   `json:"bit_depth,omitempty"`

	VideoCodec  string  `json:"video_codec,omitempty"`
	AudioCodec  string  `json:"audio_codec,omitempty"`
	VideoWidth  int64   `json:"video_width,omitempty"`
	VideoHeight int64   `json:"video_height,omitempty"`
	FrameRate   float64 `json:"frame_rate,omitempty"`
	Rotation    int64   `json:"rotation,omitempty"`

	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	IsHDR          bool   `json:"is_hdr,omitempty"`

	Subtitles         bool     `json:"subtitles,omitempty"`
	SubtitleLanguages []string `json:"subtitle_languages,omitempty"`

	ReplayGainTrackGain float64 `json:"replaygain_track_gain,omitempty"`
	ReplayGainAlbumGain float64 `json:"replaygain_album_gain,omitempty"`

	EntryCount       int64    `json:"entry_count,omitempty"`
	UncompressedSize int64    `json:"uncompressed_size,omitempty"`
	TopLevelEntries  []string `json:"top_level_entries,omitempty"`
	HasRootDir       bool     `json:"has_root_dir,omitempty"`

	Architectures       []string `json:"architectures,omitempty"`
	Bitness             int64    `json:"bitness,omitempty"`
	BinaryFormat        string   `json:"binary_format,omitempty"`
	BinaryType          string   `json:"binary_type,omitempty"`
	IsDynamicallyLinked bool     `json:"is_dynamically_linked,omitempty"`
	IsStripped          bool     `json:"is_stripped,omitempty"`
	EntryPoint          int64    `json:"entry_point,omitempty"`

	EmailTo         []string `json:"email_to,omitempty"`
	EmailCc         []string `json:"email_cc,omitempty"`
	EmailMessageID  string   `json:"email_message_id,omitempty"`
	EmailInReplyTo  string   `json:"email_in_reply_to,omitempty"`
	SentAt          string   `json:"sent_at,omitempty"` // RFC3339 when set
	AttachmentCount int64    `json:"attachment_count,omitempty"`
	EmailCount      int64    `json:"email_count,omitempty"`

	LOC        int64 `json:"loc,omitempty"`
	CommentLOC int64 `json:"comment_loc,omitempty"`
	BlankLOC   int64 `json:"blank_loc,omitempty"`

	// Imports lists the third-party / module dependencies the source
	// file declares. Populated for source/go (full import paths),
	// source/python (the module side of `import X` + `from X import`),
	// source/java + source/csharp + source/php + source/perl +
	// source/r + source/matlab (FQNs from their respective import-shape
	// keywords). Issue #275 — surfaces the CEL `imports` variable in
	// the wire format so callers can read the dependency list, project
	// via `fields: ["imports"]`, and answer "who depends on X?".
	Imports []string `json:"imports,omitempty"`

	// Functions lists the top-level + nested function / method names
	// declared in this source file. Populated by the same per-language
	// extractor pass that populates Imports — source/go (via go/ast,
	// includes receiver-bound methods as bare names) and source/python +
	// source/java + source/csharp + source/php + source/perl +
	// source/r + source/matlab (regex). Surfaces the CEL `functions`
	// variable in the wire format so `fields: ["functions"]` works.
	// Issue #278.
	Functions []string `json:"functions,omitempty"`

	// TypeNames lists the top-level + nested type / class / interface /
	// enum / record names declared in this source file. Renamed in CEL
	// from `types` to avoid a CEL-keyword collision. Populated by the
	// same per-language extractor as Functions / Imports. Issue #278.
	TypeNames []string `json:"type_names,omitempty"`

	// References lists the distinct callee names of call sites in this
	// source file (the call-site half of the code graph). Populated for
	// source/go (via go/ast CallExpr) and the tree-sitter languages
	// (Rust / TypeScript / JavaScript / Ruby / Swift / Kotlin / C / C++).
	// Name-based: `pkg.Foo()` / `x.Method()` capture "Foo" / "Method".
	// Powers who_calls / dead_code. Issue #363.
	References []string `json:"references,omitempty"`

	// MaxComplexity is the highest cyclomatic complexity of any function
	// in this source file (gocyclo-style: 1 + branch points). File-level
	// hotspot signal — `is_source && max_complexity > 15`. Populated for
	// Go + the tree-sitter languages. Drill into individual functions with
	// the complexity tool. Issue #364.
	MaxComplexity int64 `json:"max_complexity,omitempty"`

	CellCount         int64  `json:"cell_count,omitempty"`
	CodeCellCount     int64  `json:"code_cell_count,omitempty"`
	MarkdownCellCount int64  `json:"markdown_cell_count,omitempty"`
	Kernel            string `json:"kernel,omitempty"`

	// Forensic-interop hashes. Populated when the caller sets
	// `compute_hashes: true` (MCP) or `--with-hashes` (CLI). All
	// three are computed in one io.MultiWriter pass; cached
	// alongside (size, mtime). Empty when not requested.
	MD5    string `json:"md5,omitempty"`
	SHA1   string `json:"sha1,omitempty"`
	SHA256 string `json:"sha256,omitempty"`

	// Perceptual image hash. Populated when the caller sets
	// `with_phash: true` (MCP) or `--with-phash` (CLI), or when the
	// CEL expression references `image_similar_to(...)` (auto-
	// enables). 16-character hex; empty for non-image files or when
	// not requested. Issue #208.
	PHash string `json:"phash,omitempty"`

	// 3D model attributes (issue #213). Populated for model3d/* files
	// (STL / OBJ / glTF). Empty / zero for everything else.
	Model3DFormat string    `json:"model3d_format,omitempty"`
	VertexCount   int64     `json:"vertex_count,omitempty"`
	FaceCount     int64     `json:"face_count,omitempty"`
	HasNormals    bool      `json:"has_normals,omitempty"`
	HasTextures   bool      `json:"has_textures,omitempty"`
	Materials     []string  `json:"materials,omitempty"`
	BoundingBox   []float64 `json:"bounding_box,omitempty"`

	// Filesystem-level timestamps (PR #144). RFC3339 strings when
	// the OS / filesystem tracks them; empty otherwise. CreatedAt
	// is btime; MetadataChangedAt is ctime. IsBtimeAnomaly fires
	// when CreatedAt > ModTime — classic "file placed here AFTER
	// being modified elsewhere" indicator (restored backup, copied
	// across volumes, planted artefact).
	CreatedAt         string `json:"created_at,omitempty"`
	MetadataChangedAt string `json:"metadata_changed_at,omitempty"`
	IsBtimeAnomaly    bool   `json:"is_btime_anomaly,omitempty"`

	// Git-aware metadata (issue #271). Populated when the caller sets
	// with_git: true (MCP) / --with-git (CLI), AND the walk root is
	// inside a git working tree, AND the file is tracked. Surfaces
	// the CEL git_* variables in the wire format so callers can
	// project via fields: ["git_last_commit_time", ...], render in
	// JSON output, and decide based on the values without burning a
	// follow-up call. Time fields are RFC3339 strings (matching
	// CreatedAt / MetadataChangedAt). Empty / zero when the file
	// isn't tracked or with_git was off.
	GitLastCommitTime    string `json:"git_last_commit_time,omitempty"`
	GitLastCommitAuthor  string `json:"git_last_commit_author,omitempty"`
	GitLastCommitSubject string `json:"git_last_commit_subject,omitempty"`
	GitFirstSeen         string `json:"git_first_seen,omitempty"`
	GitCommitCount       int64  `json:"git_commit_count,omitempty"`
	IsGitTracked         bool   `json:"is_git_tracked,omitempty"`
	IsGitIgnored         bool   `json:"is_git_ignored,omitempty"`

	// Disguise detection (PR #145). Populated when the caller sets
	// `check_disguised: true` (MCP) or `--check-disguised` (CLI).
	// MagicContentType is what the file's first 512 bytes look
	// like under magic-byte sniffing alone; ExtensionContentType
	// is what the name implies. IsDisguised fires when both are
	// non-empty AND they disagree — classic "this .txt contains a
	// PE binary" indicator.
	MagicContentType     string `json:"magic_content_type,omitempty"`
	ExtensionContentType string `json:"extension_content_type,omitempty"`
	IsDisguised          bool   `json:"is_disguised,omitempty"`

	// Hash-allowlist / hash-denylist membership (PR #146).
	// IsKnownGood fires when the file's MD5 / SHA1 / SHA256
	// appears in the loaded allowlist; IsKnownBad fires when it
	// appears in the denylist. Requires compute_hashes. NSRL /
	// VirusTotal / threat-intel-feed interop.
	IsKnownGood bool `json:"is_known_good,omitempty"`
	IsKnownBad  bool `json:"is_known_bad,omitempty"`

	// Similarity is the cosine similarity between the file's body
	// embedding and the search call's query embedding (issue
	// #151). Populated only when --semantic-query / SemanticQuery
	// + an embedding model are configured. 0 otherwise.
	Similarity float64 `json:"similarity,omitempty"`

	// BM25 is the Okapi BM25 keyword-relevance score against the keyword
	// query, with IDF over the candidate set (issue #335). Populated only
	// when keyword_query / hybrid is set. 0 otherwise. Only comparable
	// within the same result set.
	BM25 float64 `json:"bm25,omitempty"`

	// Snippet is the first N lines of the file body when the search
	// call had include_snippet=true and the content type is
	// text-based. Empty otherwise. Lets an agent decide whether a
	// match is relevant without a separate read_attributes round trip.
	Snippet string `json:"snippet,omitempty"`
}
