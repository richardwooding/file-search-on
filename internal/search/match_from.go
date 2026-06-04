package search

import (
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// MatchFrom projects a Result (with Attrs populated) into a Match wire
// object. Empty ContentType is rewritten to "unknown" so agents and
// verbose CLI output see a labelled bucket instead of an empty string
// for files where detection failed.
//
// The projection is decomposed into per-family applyXxxAttrs helpers so
// adding a content family touches one focused function rather than this
// orchestrator. Helpers run in the original order and each reads from
// the FileAttributes (typed fields + the Extra map) into the matching
// Match fields. Reading a nil Extra map is safe in Go, but the
// orchestrator still short-circuits on it to skip ~200 no-op lookups.
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

	// Typed FileAttributes fields populate regardless of the Extra map.
	applyTypedPredicates(&m, a)
	applyComputedAttrs(&m, a)
	if a.Extra == nil {
		return m
	}
	applyCommonAttrs(&m, a)
	applyImageAttrs(&m, a)
	applyMediaAttrs(&m, a)
	applyArchiveBinaryAttrs(&m, a)
	applyEmailSourceAttrs(&m, a)
	applyFrontmatterAttrs(&m, a)
	applyDiskImageAttrs(&m, a)
	applyInstallPackageAttrs(&m, a)
	applyMiscAttrs(&m, a)
	applyCodesignAttrs(&m, a)
	applyPlistAttrs(&m, a)
	applyBytecodeAttrs(&m, a)
	applyScienceAttrs(&m, a)
	applyDatabaseAttrs(&m, a)
	applyBookmarkAttrs(&m, a)
	applyChatAttrs(&m, a)
	applyFontAttrs(&m, a)
	applyXattrAttrs(&m, a)
	return m
}

// applyTypedPredicates copies the typed is_* predicate fields and forensic hashes straight off FileAttributes.
func applyTypedPredicates(m *Match, a *celexpr.FileAttributes) {
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
	m.IsChatExport, m.IsSlackExport, m.IsDiscordExport, m.IsSignalExport = a.IsChatExport, a.IsSlackExport, a.IsDiscordExport, a.IsSignalExport
	m.IsXattrRich, m.IsQuarantined = a.IsXattrRich, a.IsQuarantined
	m.IsFont, m.IsTTF, m.IsOTF, m.IsFontCollection, m.IsWOFF, m.IsWOFF2 = a.IsFont, a.IsTTF, a.IsOTF, a.IsFontCollection, a.IsWOFF, a.IsWOFF2
	m.MD5, m.SHA1, m.SHA256 = a.MD5, a.SHA1, a.SHA256
}

// applyComputedAttrs handles the opt-in computed attributes (phash, model3d, similarity) and the filesystem-timestamp / disguise / known-hash typed fields.
func applyComputedAttrs(m *Match, a *celexpr.FileAttributes) {
	if v, ok := a.Extra["phash"].(string); ok {
		m.PHash = v
	}
	if v, ok := a.Extra["model3d_format"].(string); ok {
		m.Model3DFormat = v
	}
	if v, ok := a.Extra["vertex_count"].(int64); ok {
		m.VertexCount = v
	}
	if v, ok := a.Extra["face_count"].(int64); ok {
		m.FaceCount = v
	}
	if v, ok := a.Extra["has_normals"].(bool); ok {
		m.HasNormals = v
	}
	if v, ok := a.Extra["has_textures"].(bool); ok {
		m.HasTextures = v
	}
	if v, ok := a.Extra["materials"].([]string); ok {
		m.Materials = v
	}
	if v, ok := a.Extra["bounding_box"].([]float64); ok {
		m.BoundingBox = v
	}
	m.Similarity = a.Similarity
	m.BM25 = a.BM25
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
	// Git-aware metadata (issue #271 wire-format follow-up). The CEL
	// variables were already declared + queryable; this surfaces the
	// typed FileAttributes fields on Match so fields: ["git_*"]
	// projection works and JSON output carries them. Same pattern as
	// CreatedAt / MetadataChangedAt above (RFC3339 string).
	if !a.GitLastCommitTime.IsZero() {
		m.GitLastCommitTime = a.GitLastCommitTime.Format(time.RFC3339)
	}
	if !a.GitFirstSeen.IsZero() {
		m.GitFirstSeen = a.GitFirstSeen.Format(time.RFC3339)
	}
	m.GitLastCommitAuthor = a.GitLastCommitAuthor
	m.GitLastCommitSubject = a.GitLastCommitSubject
	m.GitCommitCount = a.GitCommitCount
	m.IsGitTracked = a.IsGitTracked
	m.IsGitIgnored = a.IsGitIgnored
}

// applyCommonAttrs covers the cross-family scalars (title/author/language/counts), manifest module fields, and project context.
func applyCommonAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyImageAttrs covers image dimensions and EXIF (camera / GPS / exposure).
func applyImageAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyMediaAttrs covers audio tags and video codec / colour / subtitle / replaygain attributes.
func applyMediaAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyArchiveBinaryAttrs covers archive entry stats and compiled-binary architecture attributes.
func applyArchiveBinaryAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyEmailSourceAttrs covers email headers, source LOC, and notebook cell counts.
func applyEmailSourceAttrs(m *Match, a *celexpr.FileAttributes) {
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
	// imports / functions / type_names all live on Extra as []string
	// from the per-language extractors in
	// internal/content/source_symbols_*.go. Surfacing each to a typed
	// Match field unblocks fields: ["imports"|"functions"|"type_names"]
	// projection and gets the lists into the wire response.
	// #275 (imports), #278 (functions + type_names).
	if v, ok := a.Extra["imports"].([]string); ok {
		m.Imports = v
	}
	if v, ok := a.Extra["functions"].([]string); ok {
		m.Functions = v
	}
	if v, ok := a.Extra["type_names"].([]string); ok {
		m.TypeNames = v
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
}

// applyFrontmatterAttrs covers markdown front-matter (format / tags / categories / draft / date).
func applyFrontmatterAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyDiskImageAttrs covers the disk-image family attributes.
func applyDiskImageAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyInstallPackageAttrs covers the install-package family attributes.
func applyInstallPackageAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyMiscAttrs covers license id, test-file detection, and symlink target.
func applyMiscAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyCodesignAttrs covers the Mach-O code signature and entitlement attributes.
func applyCodesignAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyPlistAttrs covers the Apple property-list attributes.
func applyPlistAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyBytecodeAttrs covers the VM-bytecode family attributes.
func applyBytecodeAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyScienceAttrs covers the science-data family (FITS / VOTable / HDF5 / PDS / CDF).
func applyScienceAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyDatabaseAttrs covers the SQLite / database family attributes.
func applyDatabaseAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyBookmarkAttrs covers the browser-bookmark attributes.
func applyBookmarkAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyChatAttrs covers the chat-export attributes.
func applyChatAttrs(m *Match, a *celexpr.FileAttributes) {
	// Chat exports (issue #214). Bool predicates come from the typed
	// FileAttributes fields above; per-file attrs live in Extra.
	if v, ok := a.Extra["chat_message_count"].(int64); ok {
		m.ChatMessageCount = v
	}
	if v, ok := a.Extra["chat_participants"].([]string); ok && len(v) > 0 {
		m.ChatParticipants = v
	}
	if v, ok := a.Extra["chat_channel"].(string); ok {
		m.ChatChannel = v
	}
	if v, ok := a.Extra["chat_workspace"].(string); ok {
		m.ChatWorkspace = v
	}
	if v, ok := a.Extra["chat_start_at"].(time.Time); ok && !v.IsZero() {
		m.ChatStartAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["chat_end_at"].(time.Time); ok && !v.IsZero() {
		m.ChatEndAt = v.Format(time.RFC3339)
	}
}

// applyFontAttrs covers the font attributes.
func applyFontAttrs(m *Match, a *celexpr.FileAttributes) {
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
}

// applyXattrAttrs covers the extended-attribute (xattr) family.
func applyXattrAttrs(m *Match, a *celexpr.FileAttributes) {
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
}
