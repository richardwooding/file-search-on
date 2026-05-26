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
	IsCodesigned               bool     `json:"is_codesigned,omitempty"`
	IsAppleSigned              bool     `json:"is_apple_signed,omitempty"`
	IsThirdPartySigned         bool     `json:"is_third_party_signed,omitempty"`
	CodesignIdentifier         string   `json:"codesign_identifier,omitempty"`
	CodesignTeamID             string   `json:"codesign_team_id,omitempty"`
	CodesignHashType           string   `json:"codesign_hash_type,omitempty"`
	CodesignHardenedRuntime    bool     `json:"codesign_hardened_runtime,omitempty"`
	CodesignLibraryValidation  bool     `json:"codesign_library_validation,omitempty"`
	CodesignKilled             bool     `json:"codesign_killed,omitempty"`
	CodesignAdhoc              bool     `json:"codesign_adhoc,omitempty"`
	Entitlements               []string `json:"entitlements,omitempty"`
	EntitlementAppSandbox      bool     `json:"entitlement_app_sandbox,omitempty"`
	EntitlementFullDiskAccess  bool     `json:"entitlement_full_disk_access,omitempty"`
	EntitlementNetworkClient   bool     `json:"entitlement_network_client,omitempty"`
	EntitlementNetworkServer   bool     `json:"entitlement_network_server,omitempty"`

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
	DatabaseFormat      string `json:"database_format,omitempty"`
	SQLitePageSize      int64  `json:"sqlite_page_size,omitempty"`
	SQLiteFormatVersion int64  `json:"sqlite_format_version,omitempty"`
	SQLitePageCount     int64  `json:"sqlite_page_count,omitempty"`
	SQLiteSchemaVersion int64  `json:"sqlite_schema_version,omitempty"`
	SQLiteTextEncoding  string `json:"sqlite_text_encoding,omitempty"`
	SQLiteUserVersion   int64  `json:"sqlite_user_version,omitempty"`
	SQLiteApplicationID   int64  `json:"sqlite_application_id,omitempty"`
	SQLiteApplicationName string `json:"sqlite_application_name,omitempty"`

	// SQLite schema introspection (follow-up to #174). Populated by
	// the hand-rolled sqlite_master b-tree walker — see
	// internal/content/database_sqlite_btree.go.
	SQLiteTableCount         int64    `json:"sqlite_table_count,omitempty"`
	SQLiteViewCount          int64    `json:"sqlite_view_count,omitempty"`
	SQLiteIndexCount         int64    `json:"sqlite_index_count,omitempty"`
	SQLiteTriggerCount       int64    `json:"sqlite_trigger_count,omitempty"`
	SQLiteTableNames         []string `json:"sqlite_table_names,omitempty"`
	SQLiteSchemaFingerprint  string   `json:"sqlite_schema_fingerprint,omitempty"`

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

	// Font content types (issue #197).
	IsFont                  bool     `json:"is_font,omitempty"`
	IsTTF                   bool     `json:"is_ttf,omitempty"`
	IsOTF                   bool     `json:"is_otf,omitempty"`
	IsFontCollection        bool     `json:"is_font_collection,omitempty"`
	IsWOFF                  bool     `json:"is_woff,omitempty"`
	IsWOFF2                 bool     `json:"is_woff2,omitempty"`
	IsVariableFont          bool     `json:"is_variable_font,omitempty"`
	IsColorFont             bool     `json:"is_color_font,omitempty"`
	IsMonospaceFont         bool     `json:"is_monospace_font,omitempty"`
	IsItalicFont            bool     `json:"is_italic_font,omitempty"`
	IsBoldFont              bool     `json:"is_bold_font,omitempty"`
	FontFormat              string   `json:"font_format,omitempty"`
	FontOutlineKind         string   `json:"font_outline_kind,omitempty"`
	FontFamily              string   `json:"font_family,omitempty"`
	FontSubfamily           string   `json:"font_subfamily,omitempty"`
	FontFullName            string   `json:"font_full_name,omitempty"`
	FontVersion             string   `json:"font_version,omitempty"`
	FontPostScriptName      string   `json:"font_postscript_name,omitempty"`
	FontManufacturer        string   `json:"font_manufacturer,omitempty"`
	FontDesigner            string   `json:"font_designer,omitempty"`
	FontLicense             string   `json:"font_license,omitempty"`
	FontLicenseURL          string   `json:"font_license_url,omitempty"`
	FontTypographicFamily   string   `json:"font_typographic_family,omitempty"`
	FontWeight              int64    `json:"font_weight,omitempty"`
	FontWidth               int64    `json:"font_width,omitempty"`
	FontEmbedding           string   `json:"font_embedding,omitempty"`
	FontPanose              string   `json:"font_panose,omitempty"`
	FontUnicodeRanges       []string `json:"font_unicode_ranges,omitempty"`
	FontRevision            float64  `json:"font_revision,omitempty"`
	FontUnitsPerEm          int64    `json:"font_units_per_em,omitempty"`
	FontMacStyle            []string `json:"font_mac_style,omitempty"`
	FontItalicAngle         float64  `json:"font_italic_angle,omitempty"`
	FontGlyphCount          int64    `json:"font_glyph_count,omitempty"`
	FontAxisCount           int64    `json:"font_axis_count,omitempty"`
	FontAxes                []string `json:"font_axes,omitempty"`
	FontCollectionCount     int64    `json:"font_collection_count,omitempty"`
	FontCollectionFamilies  []string `json:"font_collection_families,omitempty"`
	WOFF2TotalSfntSize      int64    `json:"woff2_total_sfnt_size,omitempty"`
	WOFF2TotalCompressedSize int64   `json:"woff2_total_compressed_size,omitempty"`

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

	// Filesystem-level timestamps (PR #144). RFC3339 strings when
	// the OS / filesystem tracks them; empty otherwise. CreatedAt
	// is btime; MetadataChangedAt is ctime. IsBtimeAnomaly fires
	// when CreatedAt > ModTime — classic "file placed here AFTER
	// being modified elsewhere" indicator (restored backup, copied
	// across volumes, planted artefact).
	CreatedAt         string `json:"created_at,omitempty"`
	MetadataChangedAt string `json:"metadata_changed_at,omitempty"`
	IsBtimeAnomaly    bool   `json:"is_btime_anomaly,omitempty"`

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

	// Snippet is the first N lines of the file body when the search
	// call had include_snippet=true and the content type is
	// text-based. Empty otherwise. Lets an agent decide whether a
	// match is relevant without a separate read_attributes round trip.
	Snippet string `json:"snippet,omitempty"`
}

// MatchFrom projects a Result (with Attrs populated) into a Match
// wire object. Empty ContentType is rewritten to "unknown" so
// agents and verbose CLI output see a labelled bucket instead of
// an empty string for files where detection failed.
func MatchFrom(r Result) Match {
	m := Match{
		Path:        r.Path,
		ContentType: r.ContentType,
		Size:        r.Size,
		Snippet:     r.Snippet,
		Rank:        r.Rank,
	}
	if m.ContentType == "" {
		m.ContentType = "unknown"
	}
	if r.Attrs == nil {
		return m
	}
	a := r.Attrs
	m.IsMarkdown, m.IsJSON, m.IsXML, m.IsHTML = a.IsMarkdown, a.IsJSON, a.IsXML, a.IsHTML
	m.IsPDF, m.IsImage = a.IsPDF, a.IsImage
	m.IsText, m.IsCSV, m.IsEPUB, m.IsOffice = a.IsText, a.IsCSV, a.IsEPUB, a.IsOffice
	m.IsAudio = a.IsAudio
	m.IsVideo = a.IsVideo
	m.IsArchive = a.IsArchive
	m.IsBinary = a.IsBinary
	m.IsEmail = a.IsEmail
	m.IsSource = a.IsSource
	m.IsNotebook = a.IsNotebook
	m.IsYAML = a.IsYAML
	m.IsTOML = a.IsTOML
	m.IsDockerfile, m.IsMakefile, m.IsJustfile, m.IsRakefile = a.IsDockerfile, a.IsMakefile, a.IsJustfile, a.IsRakefile
	m.IsLicense, m.IsChangelog, m.IsContributing, m.IsCodeowners = a.IsLicense, a.IsChangelog, a.IsContributing, a.IsCodeowners
	m.IsGitignore, m.IsDockerignore = a.IsGitignore, a.IsDockerignore
	m.IsGomod, m.IsNodeManifest, m.IsCargoManifest = a.IsGomod, a.IsNodeManifest, a.IsCargoManifest
	m.IsPipfile, m.IsPythonReqs, m.IsGemfile = a.IsPipfile, a.IsPythonReqs, a.IsGemfile
	m.IsProcfile, m.IsVagrantfile = a.IsProcfile, a.IsVagrantfile
	m.IsBuild, m.IsRepoMeta, m.IsIgnore, m.IsManifest, m.IsPlatform = a.IsBuild, a.IsRepoMeta, a.IsIgnore, a.IsManifest, a.IsPlatform
	m.IsDSStore, m.IsLocalized, m.IsThumbsDB, m.IsDesktopIni, m.IsKDEDirectory = a.IsDSStore, a.IsLocalized, a.IsThumbsDB, a.IsDesktopIni, a.IsKDEDirectory
	m.IsMacOSMetadata, m.IsWindowsMetadata, m.IsLinuxMetadata, m.IsSystemMetadata = a.IsMacOSMetadata, a.IsWindowsMetadata, a.IsLinuxMetadata, a.IsSystemMetadata
	m.IsDMG, m.IsISO, m.IsVHD, m.IsVHDX, m.IsVMDK, m.IsQCOW2, m.IsWIM, m.IsDiskImage = a.IsDMG, a.IsISO, a.IsVHD, a.IsVHDX, a.IsVMDK, a.IsQCOW2, a.IsWIM, a.IsDiskImage
	m.IsPkg, m.IsDeb, m.IsRPM, m.IsAppImage, m.IsInstallPackage = a.IsPkg, a.IsDeb, a.IsRPM, a.IsAppImage, a.IsInstallPackage
	m.IsSymlink, m.IsBrokenSymlink = a.IsSymlink, a.IsBrokenSymlink
	m.IsPlist = a.IsPlist
	m.IsClass, m.IsPyc, m.IsWasm, m.IsBytecode = a.IsClass, a.IsPyc, a.IsWasm, a.IsBytecode
	m.IsFITS, m.IsVotable, m.IsHDF5, m.IsScienceData = a.IsFITS, a.IsVotable, a.IsHDF5, a.IsScienceData
	m.IsPDS3, m.IsPDS4, m.IsPDS = a.IsPDS3, a.IsPDS4, a.IsPDS
	m.IsCDF = a.IsCDF
	m.IsSQLite, m.IsDatabase = a.IsSQLite, a.IsDatabase
	m.IsBookmarkFile, m.IsChromiumBookmarks, m.IsSafariBookmarks = a.IsBookmarkFile, a.IsChromiumBookmarks, a.IsSafariBookmarks
	m.IsXattrRich, m.IsQuarantined = a.IsXattrRich, a.IsQuarantined
	m.IsFont, m.IsTTF, m.IsOTF, m.IsFontCollection, m.IsWOFF, m.IsWOFF2 = a.IsFont, a.IsTTF, a.IsOTF, a.IsFontCollection, a.IsWOFF, a.IsWOFF2
	m.MD5, m.SHA1, m.SHA256 = a.MD5, a.SHA1, a.SHA256
	if v, ok := a.Extra["phash"].(string); ok {
		m.PHash = v
	}
	m.Similarity = a.Similarity
	if !a.CreatedAt.IsZero() {
		m.CreatedAt = a.CreatedAt.Format(time.RFC3339)
	}
	if !a.MetadataChangedAt.IsZero() {
		m.MetadataChangedAt = a.MetadataChangedAt.Format(time.RFC3339)
	}
	m.IsBtimeAnomaly = a.IsBtimeAnomaly
	m.MagicContentType = a.MagicContentType
	m.ExtensionContentType = a.ExtensionContentType
	m.IsDisguised = a.IsDisguised
	m.IsKnownGood = a.IsKnownGood
	m.IsKnownBad = a.IsKnownBad

	if a.Extra == nil {
		return m
	}
	if v, ok := a.Extra["title"].(string); ok {
		m.Title = v
	}
	if v, ok := a.Extra["author"].(string); ok {
		m.Author = v
	}
	if v, ok := a.Extra["language"].(string); ok {
		m.Language = v
	}
	if v, ok := a.Extra["word_count"].(int64); ok {
		m.WordCount = v
	}
	if v, ok := a.Extra["line_count"].(int64); ok {
		m.LineCount = v
	}
	if v, ok := a.Extra["page_count"].(int64); ok {
		m.PageCount = v
	}
	if v, ok := a.Extra["column_count"].(int64); ok {
		m.ColumnCount = v
	}
	if v, ok := a.Extra["csv_columns"].([]string); ok {
		m.CSVColumns = v
	}
	if v, ok := a.Extra["root_element"].(string); ok {
		m.RootElement = v
	}
	if v, ok := a.Extra["json_kind"].(string); ok {
		m.JSONKind = v
	}
	if v, ok := a.Extra["yaml_kind"].(string); ok {
		m.YAMLKind = v
	}
	if v, ok := a.Extra["yaml_document_count"].(int64); ok {
		m.YAMLDocumentCount = v
	}
	if v, ok := a.Extra["module"].(string); ok {
		m.Module = v
	}
	if v, ok := a.Extra["go_version"].(string); ok {
		m.GoVersion = v
	}
	if v, ok := a.Extra["base_image"].(string); ok {
		m.BaseImage = v
	}
	if v, ok := a.Extra["project_types"].([]string); ok && len(v) > 0 {
		m.ProjectTypes = v
	}
	if v, ok := a.Extra["project_type"].(string); ok {
		m.ProjectType = v
	}
	if v, ok := a.Extra["is_static_site"].(bool); ok {
		m.IsStaticSite = v
	}
	if v, ok := a.Extra["img_width"].(int64); ok {
		m.ImgWidth = v
	}
	if v, ok := a.Extra["img_height"].(int64); ok {
		m.ImgHeight = v
	}
	if v, ok := a.Extra["camera_make"].(string); ok {
		m.CameraMake = v
	}
	if v, ok := a.Extra["camera_model"].(string); ok {
		m.CameraModel = v
	}
	if v, ok := a.Extra["lens"].(string); ok {
		m.Lens = v
	}
	if v, ok := a.Extra["taken_at"].(time.Time); ok && !v.IsZero() {
		m.TakenAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["orientation"].(int64); ok {
		m.Orientation = v
	}
	if v, ok := a.Extra["gps_lat"].(float64); ok {
		m.GPSLat = v
	}
	if v, ok := a.Extra["gps_lon"].(float64); ok {
		m.GPSLon = v
	}
	if v, ok := a.Extra["iso"].(int64); ok {
		m.ISO = v
	}
	if v, ok := a.Extra["focal_length"].(float64); ok {
		m.FocalLength = v
	}
	if v, ok := a.Extra["f_stop"].(float64); ok {
		m.FStop = v
	}
	if v, ok := a.Extra["exposure_time"].(float64); ok {
		m.ExposureTime = v
	}
	if v, ok := a.Extra["artist"].(string); ok {
		m.Artist = v
	}
	if v, ok := a.Extra["album"].(string); ok {
		m.Album = v
	}
	if v, ok := a.Extra["album_artist"].(string); ok {
		m.AlbumArtist = v
	}
	if v, ok := a.Extra["composer"].(string); ok {
		m.Composer = v
	}
	if v, ok := a.Extra["year"].(int64); ok {
		m.Year = v
	}
	if v, ok := a.Extra["track"].(int64); ok {
		m.Track = v
	}
	if v, ok := a.Extra["genre"].(string); ok {
		m.Genre = v
	}
	if v, ok := a.Extra["duration"].(float64); ok {
		m.Duration = v
	}
	if v, ok := a.Extra["bitrate"].(int64); ok {
		m.Bitrate = v
	}
	if v, ok := a.Extra["sample_rate"].(int64); ok {
		m.SampleRate = v
	}
	if v, ok := a.Extra["channels"].(int64); ok {
		m.Channels = v
	}
	if v, ok := a.Extra["bit_depth"].(int64); ok {
		m.BitDepth = v
	}
	if v, ok := a.Extra["nominal_bitrate"].(int64); ok {
		m.NominalBitrate = v
	}
	if v, ok := a.Extra["video_codec"].(string); ok {
		m.VideoCodec = v
	}
	if v, ok := a.Extra["audio_codec"].(string); ok {
		m.AudioCodec = v
	}
	if v, ok := a.Extra["video_width"].(int64); ok {
		m.VideoWidth = v
	}
	if v, ok := a.Extra["video_height"].(int64); ok {
		m.VideoHeight = v
	}
	if v, ok := a.Extra["frame_rate"].(float64); ok {
		m.FrameRate = v
	}
	if v, ok := a.Extra["rotation"].(int64); ok {
		m.Rotation = v
	}
	if v, ok := a.Extra["color_primaries"].(string); ok {
		m.ColorPrimaries = v
	}
	if v, ok := a.Extra["color_transfer"].(string); ok {
		m.ColorTransfer = v
	}
	if v, ok := a.Extra["is_hdr"].(bool); ok {
		m.IsHDR = v
	}
	if v, ok := a.Extra["subtitles"].(bool); ok {
		m.Subtitles = v
	}
	if v, ok := a.Extra["subtitle_languages"].([]string); ok && len(v) > 0 {
		m.SubtitleLanguages = v
	}
	if v, ok := a.Extra["replaygain_track_gain"].(float64); ok {
		m.ReplayGainTrackGain = v
	}
	if v, ok := a.Extra["replaygain_album_gain"].(float64); ok {
		m.ReplayGainAlbumGain = v
	}
	if v, ok := a.Extra["entry_count"].(int64); ok {
		m.EntryCount = v
	}
	if v, ok := a.Extra["uncompressed_size"].(int64); ok {
		m.UncompressedSize = v
	}
	if v, ok := a.Extra["top_level_entries"].([]string); ok && len(v) > 0 {
		m.TopLevelEntries = v
	}
	if v, ok := a.Extra["has_root_dir"].(bool); ok {
		m.HasRootDir = v
	}
	if v, ok := a.Extra["architectures"].([]string); ok && len(v) > 0 {
		m.Architectures = v
	}
	if v, ok := a.Extra["bitness"].(int64); ok {
		m.Bitness = v
	}
	if v, ok := a.Extra["binary_format"].(string); ok {
		m.BinaryFormat = v
	}
	if v, ok := a.Extra["binary_type"].(string); ok {
		m.BinaryType = v
	}
	if v, ok := a.Extra["is_dynamically_linked"].(bool); ok {
		m.IsDynamicallyLinked = v
	}
	if v, ok := a.Extra["is_stripped"].(bool); ok {
		m.IsStripped = v
	}
	if v, ok := a.Extra["entry_point"].(int64); ok {
		m.EntryPoint = v
	}
	if v, ok := a.Extra["email_to"].([]string); ok && len(v) > 0 {
		m.EmailTo = v
	}
	if v, ok := a.Extra["email_cc"].([]string); ok && len(v) > 0 {
		m.EmailCc = v
	}
	if v, ok := a.Extra["email_message_id"].(string); ok {
		m.EmailMessageID = v
	}
	if v, ok := a.Extra["email_in_reply_to"].(string); ok {
		m.EmailInReplyTo = v
	}
	if v, ok := a.Extra["sent_at"].(time.Time); ok && !v.IsZero() {
		m.SentAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["attachment_count"].(int64); ok {
		m.AttachmentCount = v
	}
	if v, ok := a.Extra["email_count"].(int64); ok {
		m.EmailCount = v
	}
	if v, ok := a.Extra["loc"].(int64); ok {
		m.LOC = v
	}
	if v, ok := a.Extra["comment_loc"].(int64); ok {
		m.CommentLOC = v
	}
	if v, ok := a.Extra["blank_loc"].(int64); ok {
		m.BlankLOC = v
	}
	if v, ok := a.Extra["cell_count"].(int64); ok {
		m.CellCount = v
	}
	if v, ok := a.Extra["code_cell_count"].(int64); ok {
		m.CodeCellCount = v
	}
	if v, ok := a.Extra["markdown_cell_count"].(int64); ok {
		m.MarkdownCellCount = v
	}
	if v, ok := a.Extra["kernel"].(string); ok {
		m.Kernel = v
	}
	if v, ok := a.Extra["frontmatter_format"].(string); ok {
		m.FrontmatterFormat = v
	}
	if v, ok := a.Extra["frontmatter"].(map[string]any); ok && len(v) > 0 {
		m.Frontmatter = v
	}
	if v, ok := a.Extra["tags"].([]string); ok && len(v) > 0 {
		m.Tags = v
	}
	if v, ok := a.Extra["categories"].([]string); ok && len(v) > 0 {
		m.Categories = v
	}
	if v, ok := a.Extra["draft"].(bool); ok {
		m.Draft = v
	}
	if v, ok := a.Extra["date"].(time.Time); ok && !v.IsZero() {
		m.Date = v.Format(time.RFC3339)
	}

	// Disk-image family.
	if v, ok := a.Extra["disk_image_format"].(string); ok {
		m.DiskImageFormat = v
	}
	if v, ok := a.Extra["virtual_size"].(int64); ok {
		m.VirtualSize = v
	}
	if v, ok := a.Extra["disk_type"].(string); ok {
		m.DiskType = v
	}
	if v, ok := a.Extra["volume_label"].(string); ok {
		m.VolumeLabel = v
	}
	if v, ok := a.Extra["disk_image_created_at"].(time.Time); ok && !v.IsZero() {
		m.DiskImageCreatedAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["cluster_bits"].(int64); ok {
		m.ClusterBits = v
	}
	if v, ok := a.Extra["is_encrypted"].(bool); ok {
		m.IsEncrypted = v
	}
	if v, ok := a.Extra["image_count"].(int64); ok {
		m.ImageCount = v
	}

	// Install-package family.
	if v, ok := a.Extra["package_format"].(string); ok {
		m.PackageFormat = v
	}
	if v, ok := a.Extra["package_name"].(string); ok {
		m.PackageName = v
	}
	if v, ok := a.Extra["package_version"].(string); ok {
		m.PackageVersion = v
	}
	if v, ok := a.Extra["package_release"].(string); ok {
		m.PackageRelease = v
	}
	if v, ok := a.Extra["package_arch"].(string); ok {
		m.PackageArch = v
	}
	if v, ok := a.Extra["package_kind"].(string); ok {
		m.PackageKind = v
	}
	if v, ok := a.Extra["appimage_version"].(int64); ok {
		m.AppImageVersion = v
	}

	// License id.
	if v, ok := a.Extra["license_id"].(string); ok {
		m.LicenseID = v
	}

	// Test-file detection.
	if v, ok := a.Extra["is_test_file"].(bool); ok {
		m.IsTestFile = v
	}

	// Symlink awareness — target_path lives in Extra; the two
	// booleans come from typed FileAttributes fields above.
	if v, ok := a.Extra["target_path"].(string); ok {
		m.TargetPath = v
	}

	// Mach-O code signature (issue #187). Everything lives in Extra
	// since the parser is content-type-specific (not surfaced via
	// FileAttributes struct fields).
	if v, ok := a.Extra["is_codesigned"].(bool); ok {
		m.IsCodesigned = v
	}
	if v, ok := a.Extra["is_apple_signed"].(bool); ok {
		m.IsAppleSigned = v
	}
	if v, ok := a.Extra["is_third_party_signed"].(bool); ok {
		m.IsThirdPartySigned = v
	}
	if v, ok := a.Extra["codesign_identifier"].(string); ok {
		m.CodesignIdentifier = v
	}
	if v, ok := a.Extra["codesign_team_id"].(string); ok {
		m.CodesignTeamID = v
	}
	if v, ok := a.Extra["codesign_hash_type"].(string); ok {
		m.CodesignHashType = v
	}
	if v, ok := a.Extra["codesign_hardened_runtime"].(bool); ok {
		m.CodesignHardenedRuntime = v
	}
	if v, ok := a.Extra["codesign_library_validation"].(bool); ok {
		m.CodesignLibraryValidation = v
	}
	if v, ok := a.Extra["codesign_killed"].(bool); ok {
		m.CodesignKilled = v
	}
	if v, ok := a.Extra["codesign_adhoc"].(bool); ok {
		m.CodesignAdhoc = v
	}
	if v, ok := a.Extra["entitlements"].([]string); ok && len(v) > 0 {
		m.Entitlements = v
	}
	if v, ok := a.Extra["entitlement_app_sandbox"].(bool); ok {
		m.EntitlementAppSandbox = v
	}
	if v, ok := a.Extra["entitlement_full_disk_access"].(bool); ok {
		m.EntitlementFullDiskAccess = v
	}
	if v, ok := a.Extra["entitlement_network_client"].(bool); ok {
		m.EntitlementNetworkClient = v
	}
	if v, ok := a.Extra["entitlement_network_server"].(bool); ok {
		m.EntitlementNetworkServer = v
	}

	// Apple property list (issue #185). is_plist comes from the typed
	// FileAttributes field above; the rest live in Extra.
	if v, ok := a.Extra["plist_format"].(string); ok {
		m.PlistFormat = v
	}
	if v, ok := a.Extra["plist_root_kind"].(string); ok {
		m.PlistRootKind = v
	}
	if v, ok := a.Extra["plist_kind"].(string); ok {
		m.PlistKind = v
	}
	if v, ok := a.Extra["plist_bundle_identifier"].(string); ok {
		m.PlistBundleIdentifier = v
	}
	if v, ok := a.Extra["plist_bundle_name"].(string); ok {
		m.PlistBundleName = v
	}
	if v, ok := a.Extra["plist_bundle_version"].(string); ok {
		m.PlistBundleVersion = v
	}
	if v, ok := a.Extra["plist_bundle_short_version"].(string); ok {
		m.PlistBundleShortVersion = v
	}
	if v, ok := a.Extra["plist_executable"].(string); ok {
		m.PlistExecutable = v
	}
	if v, ok := a.Extra["plist_min_os_version"].(string); ok {
		m.PlistMinOSVersion = v
	}
	if v, ok := a.Extra["plist_label"].(string); ok {
		m.PlistLabel = v
	}
	if v, ok := a.Extra["plist_program"].(string); ok {
		m.PlistProgram = v
	}
	if v, ok := a.Extra["plist_program_arguments"].([]string); ok && len(v) > 0 {
		m.PlistProgramArguments = v
	}
	if v, ok := a.Extra["plist_run_at_load"].(bool); ok {
		m.PlistRunAtLoad = v
	}
	if v, ok := a.Extra["plist_keep_alive"].(bool); ok {
		m.PlistKeepAlive = v
	}

	// VM-bytecode family attributes.
	if v, ok := a.Extra["bytecode_format"].(string); ok {
		m.BytecodeFormat = v
	}
	if v, ok := a.Extra["runtime_version"].(string); ok {
		m.RuntimeVersion = v
	}
	if v, ok := a.Extra["class_name"].(string); ok {
		m.ClassName = v
	}
	if v, ok := a.Extra["super_class"].(string); ok {
		m.SuperClass = v
	}
	if v, ok := a.Extra["interfaces"].([]string); ok && len(v) > 0 {
		m.Interfaces = v
	}
	if v, ok := a.Extra["method_count"].(int64); ok {
		m.MethodCount = v
	}
	if v, ok := a.Extra["field_count"].(int64); ok {
		m.FieldCount = v
	}
	if v, ok := a.Extra["access_flags"].([]string); ok && len(v) > 0 {
		m.AccessFlags = v
	}
	if v, ok := a.Extra["python_version"].(string); ok {
		m.PythonVersion = v
	}
	if v, ok := a.Extra["source_mtime"].(time.Time); ok && !v.IsZero() {
		m.SourceMtime = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["wasm_version"].(int64); ok {
		m.WasmVersion = v
	}
	if v, ok := a.Extra["section_count"].(int64); ok {
		m.SectionCount = v
	}
	if v, ok := a.Extra["import_count"].(int64); ok {
		m.ImportCount = v
	}
	if v, ok := a.Extra["export_count"].(int64); ok {
		m.ExportCount = v
	}

	// Science-data family.
	if v, ok := a.Extra["science_format"].(string); ok {
		m.ScienceFormat = v
	}
	if v, ok := a.Extra["telescope"].(string); ok {
		m.Telescope = v
	}
	if v, ok := a.Extra["instrument"].(string); ok {
		m.Instrument = v
	}
	if v, ok := a.Extra["object"].(string); ok {
		m.Object = v
	}
	if v, ok := a.Extra["observer"].(string); ok {
		m.Observer = v
	}
	if v, ok := a.Extra["date_obs"].(string); ok {
		m.DateObs = v
	}
	if v, ok := a.Extra["exptime"].(float64); ok {
		m.Exptime = v
	}
	if v, ok := a.Extra["filter"].(string); ok {
		m.Filter = v
	}
	if v, ok := a.Extra["airmass"].(float64); ok {
		m.Airmass = v
	}
	if v, ok := a.Extra["ra"].(float64); ok {
		m.RA = v
	}
	if v, ok := a.Extra["dec"].(float64); ok {
		m.Dec = v
	}
	if v, ok := a.Extra["bitpix"].(int64); ok {
		m.Bitpix = v
	}
	if v, ok := a.Extra["naxis"].(int64); ok {
		m.Naxis = v
	}
	if v, ok := a.Extra["naxis1"].(int64); ok {
		m.Naxis1 = v
	}
	if v, ok := a.Extra["naxis2"].(int64); ok {
		m.Naxis2 = v
	}
	if v, ok := a.Extra["hdu_count"].(int64); ok {
		m.HDUCount = v
	}
	if v, ok := a.Extra["fits_kind"].(string); ok {
		m.FITSKind = v
	}

	// VOTable.
	if v, ok := a.Extra["votable_version"].(string); ok {
		m.VOTableVersion = v
	}
	if v, ok := a.Extra["table_count"].(int64); ok {
		m.TableCount = v
	}
	if v, ok := a.Extra["total_rows"].(int64); ok {
		m.TotalRows = v
	}
	if v, ok := a.Extra["field_names"].([]string); ok && len(v) > 0 {
		m.FieldNames = v
	}
	if v, ok := a.Extra["field_units"].([]string); ok && len(v) > 0 {
		m.FieldUnits = v
	}
	if v, ok := a.Extra["field_ucds"].([]string); ok && len(v) > 0 {
		m.FieldUCDs = v
	}
	if v, ok := a.Extra["votable_data_format"].(string); ok {
		m.VOTableDataFormat = v
	}

	// HDF5.
	if v, ok := a.Extra["hdf5_format_version"].(int64); ok {
		m.HDF5FormatVersion = v
	}
	if v, ok := a.Extra["hdf5_size_of_offsets"].(int64); ok {
		m.HDF5SizeOfOffsets = v
	}
	if v, ok := a.Extra["hdf5_size_of_lengths"].(int64); ok {
		m.HDF5SizeOfLengths = v
	}

	// PDS.
	if v, ok := a.Extra["pds_version"].(string); ok {
		m.PDSVersion = v
	}
	if v, ok := a.Extra["mission_name"].(string); ok {
		m.MissionName = v
	}
	if v, ok := a.Extra["spacecraft_name"].(string); ok {
		m.SpacecraftName = v
	}
	if v, ok := a.Extra["instrument_name"].(string); ok {
		m.InstrumentName = v
	}
	if v, ok := a.Extra["target_name"].(string); ok {
		m.TargetName = v
	}
	if v, ok := a.Extra["product_id"].(string); ok {
		m.ProductID = v
	}
	if v, ok := a.Extra["start_time"].(string); ok {
		m.StartTime = v
	}

	// CDF.
	if v, ok := a.Extra["cdf_version"].(string); ok {
		m.CDFVersion = v
	}
	if v, ok := a.Extra["cdf_encoding"].(string); ok {
		m.CDFEncoding = v
	}
	if v, ok := a.Extra["cdf_majority"].(string); ok {
		m.CDFMajority = v
	}
	if v, ok := a.Extra["variable_count"].(int64); ok {
		m.VariableCount = v
	}
	if v, ok := a.Extra["attribute_count"].(int64); ok {
		m.AttributeCount = v
	}

	// Database family.
	if v, ok := a.Extra["database_format"].(string); ok {
		m.DatabaseFormat = v
	}
	if v, ok := a.Extra["sqlite_page_size"].(int64); ok {
		m.SQLitePageSize = v
	}
	if v, ok := a.Extra["sqlite_format_version"].(int64); ok {
		m.SQLiteFormatVersion = v
	}
	if v, ok := a.Extra["sqlite_page_count"].(int64); ok {
		m.SQLitePageCount = v
	}
	if v, ok := a.Extra["sqlite_schema_version"].(int64); ok {
		m.SQLiteSchemaVersion = v
	}
	if v, ok := a.Extra["sqlite_text_encoding"].(string); ok {
		m.SQLiteTextEncoding = v
	}
	if v, ok := a.Extra["sqlite_user_version"].(int64); ok {
		m.SQLiteUserVersion = v
	}
	if v, ok := a.Extra["sqlite_application_id"].(int64); ok {
		m.SQLiteApplicationID = v
	}
	if v, ok := a.Extra["sqlite_application_name"].(string); ok {
		m.SQLiteApplicationName = v
	}
	if v, ok := a.Extra["sqlite_table_count"].(int64); ok {
		m.SQLiteTableCount = v
	}
	if v, ok := a.Extra["sqlite_view_count"].(int64); ok {
		m.SQLiteViewCount = v
	}
	if v, ok := a.Extra["sqlite_index_count"].(int64); ok {
		m.SQLiteIndexCount = v
	}
	if v, ok := a.Extra["sqlite_trigger_count"].(int64); ok {
		m.SQLiteTriggerCount = v
	}
	if v, ok := a.Extra["sqlite_table_names"].([]string); ok && len(v) > 0 {
		m.SQLiteTableNames = v
	}
	if v, ok := a.Extra["sqlite_schema_fingerprint"].(string); ok {
		m.SQLiteSchemaFingerprint = v
	}
	if v, ok := a.Extra["sqlite_fts_table_count"].(int64); ok {
		m.SQLiteFTSTableCount = v
	}
	if v, ok := a.Extra["sqlite_fts_table_names"].([]string); ok && len(v) > 0 {
		m.SQLiteFTSTableNames = v
	}
	if v, ok := a.Extra["sqlite_wal_format_version"].(int64); ok {
		m.SQLiteWALFormatVersion = v
	}
	if v, ok := a.Extra["sqlite_wal_page_size"].(int64); ok {
		m.SQLiteWALPageSize = v
	}
	if v, ok := a.Extra["sqlite_wal_checkpoint_seq"].(int64); ok {
		m.SQLiteWALCheckpointSeq = v
	}
	if v, ok := a.Extra["sqlite_wal_frame_count"].(int64); ok {
		m.SQLiteWALFrameCount = v
	}
	if v, ok := a.Extra["sqlite_wal_byte_order"].(string); ok {
		m.SQLiteWALByteOrder = v
	}

	// Browser bookmarks (issue #188). Bool predicates come from the
	// typed FileAttributes fields above; per-file attrs live in Extra.
	if v, ok := a.Extra["bookmark_count"].(int64); ok {
		m.BookmarkCount = v
	}
	if v, ok := a.Extra["bookmark_folder_count"].(int64); ok {
		m.BookmarkFolderCount = v
	}
	if v, ok := a.Extra["bookmark_folders"].([]string); ok && len(v) > 0 {
		m.BookmarkFolders = v
	}
	if v, ok := a.Extra["bookmark_urls"].([]string); ok && len(v) > 0 {
		m.BookmarkURLs = v
	}
	if v, ok := a.Extra["bookmark_titles"].([]string); ok && len(v) > 0 {
		m.BookmarkTitles = v
	}
	if v, ok := a.Extra["browser_vendor"].(string); ok {
		m.BrowserVendor = v
	}
	if v, ok := a.Extra["bookmark_profile"].(string); ok {
		m.BookmarkProfile = v
	}

	// Font content types (issue #197). Per-format bool umbrellas
	// come from the typed FileAttributes fields above; per-trait
	// predicates and all string/int/list attrs live in Extra.
	if v, ok := a.Extra["is_variable_font"].(bool); ok {
		m.IsVariableFont = v
	}
	if v, ok := a.Extra["is_color_font"].(bool); ok {
		m.IsColorFont = v
	}
	if v, ok := a.Extra["is_monospace_font"].(bool); ok {
		m.IsMonospaceFont = v
	}
	if v, ok := a.Extra["is_italic_font"].(bool); ok {
		m.IsItalicFont = v
	}
	if v, ok := a.Extra["is_bold_font"].(bool); ok {
		m.IsBoldFont = v
	}
	if v, ok := a.Extra["font_format"].(string); ok {
		m.FontFormat = v
	}
	if v, ok := a.Extra["font_outline_kind"].(string); ok {
		m.FontOutlineKind = v
	}
	if v, ok := a.Extra["font_family"].(string); ok {
		m.FontFamily = v
	}
	if v, ok := a.Extra["font_subfamily"].(string); ok {
		m.FontSubfamily = v
	}
	if v, ok := a.Extra["font_full_name"].(string); ok {
		m.FontFullName = v
	}
	if v, ok := a.Extra["font_version"].(string); ok {
		m.FontVersion = v
	}
	if v, ok := a.Extra["font_postscript_name"].(string); ok {
		m.FontPostScriptName = v
	}
	if v, ok := a.Extra["font_manufacturer"].(string); ok {
		m.FontManufacturer = v
	}
	if v, ok := a.Extra["font_designer"].(string); ok {
		m.FontDesigner = v
	}
	if v, ok := a.Extra["font_license"].(string); ok {
		m.FontLicense = v
	}
	if v, ok := a.Extra["font_license_url"].(string); ok {
		m.FontLicenseURL = v
	}
	if v, ok := a.Extra["font_typographic_family"].(string); ok {
		m.FontTypographicFamily = v
	}
	if v, ok := a.Extra["font_weight"].(int64); ok {
		m.FontWeight = v
	}
	if v, ok := a.Extra["font_width"].(int64); ok {
		m.FontWidth = v
	}
	if v, ok := a.Extra["font_embedding"].(string); ok {
		m.FontEmbedding = v
	}
	if v, ok := a.Extra["font_panose"].(string); ok {
		m.FontPanose = v
	}
	if v, ok := a.Extra["font_unicode_ranges"].([]string); ok && len(v) > 0 {
		m.FontUnicodeRanges = v
	}
	if v, ok := a.Extra["font_revision"].(float64); ok {
		m.FontRevision = v
	}
	if v, ok := a.Extra["font_units_per_em"].(int64); ok {
		m.FontUnitsPerEm = v
	}
	if v, ok := a.Extra["font_mac_style"].([]string); ok && len(v) > 0 {
		m.FontMacStyle = v
	}
	if v, ok := a.Extra["font_italic_angle"].(float64); ok {
		m.FontItalicAngle = v
	}
	if v, ok := a.Extra["font_glyph_count"].(int64); ok {
		m.FontGlyphCount = v
	}
	if v, ok := a.Extra["font_axis_count"].(int64); ok {
		m.FontAxisCount = v
	}
	if v, ok := a.Extra["font_axes"].([]string); ok && len(v) > 0 {
		m.FontAxes = v
	}
	if v, ok := a.Extra["font_collection_count"].(int64); ok {
		m.FontCollectionCount = v
	}
	if v, ok := a.Extra["font_collection_families"].([]string); ok && len(v) > 0 {
		m.FontCollectionFamilies = v
	}
	if v, ok := a.Extra["woff2_total_sfnt_size"].(int64); ok {
		m.WOFF2TotalSfntSize = v
	}
	if v, ok := a.Extra["woff2_total_compressed_size"].(int64); ok {
		m.WOFF2TotalCompressedSize = v
	}

	// Extended attributes (issue #193). Bool umbrellas come from the
	// typed FileAttributes fields above; the rest live in Extra.
	if v, ok := a.Extra["xattr_keys"].([]string); ok && len(v) > 0 {
		m.XattrKeys = v
	}
	if v, ok := a.Extra["xattr_count"].(int64); ok {
		m.XattrCount = v
	}
	if v, ok := a.Extra["quarantine_agent"].(string); ok {
		m.QuarantineAgent = v
	}
	if v, ok := a.Extra["quarantine_event_id"].(string); ok {
		m.QuarantineEventID = v
	}
	if v, ok := a.Extra["quarantine_source_url"].(string); ok {
		m.QuarantineSourceURL = v
	}
	if v, ok := a.Extra["quarantine_referrer_url"].(string); ok {
		m.QuarantineReferrerURL = v
	}
	if v, ok := a.Extra["quarantine_download_date"].(time.Time); ok {
		m.QuarantineDownloadDate = v
	}
	if v, ok := a.Extra["quarantine_user_approved"].(bool); ok {
		m.QuarantineUserApproved = v
	}
	if v, ok := a.Extra["finder_tags"].([]string); ok && len(v) > 0 {
		m.FinderTags = v
	}
	if v, ok := a.Extra["finder_color"].(string); ok {
		m.FinderColor = v
	}
	if v, ok := a.Extra["has_finder_comment"].(bool); ok {
		m.HasFinderComment = v
	}

	return m
}
