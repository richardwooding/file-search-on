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
	// created_at for VHD/ISO, cluster_bits + is_encrypted for
	// QCOW2, image_count for WIM).
	DiskImageFormat string `json:"disk_image_format,omitempty"`
	VirtualSize     int64  `json:"virtual_size,omitempty"`
	DiskType        string `json:"disk_type,omitempty"`
	VolumeLabel     string `json:"volume_label,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"` // RFC3339 when set
	ClusterBits     int64  `json:"cluster_bits,omitempty"`
	IsEncrypted     bool   `json:"is_encrypted,omitempty"`
	ImageCount      int64  `json:"image_count,omitempty"`

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
	m.IsClass, m.IsPyc, m.IsWasm, m.IsBytecode = a.IsClass, a.IsPyc, a.IsWasm, a.IsBytecode

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
	if v, ok := a.Extra["created_at"].(time.Time); ok && !v.IsZero() {
		m.CreatedAt = v.Format(time.RFC3339)
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

	return m
}
