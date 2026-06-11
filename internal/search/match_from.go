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
	setExtra(&m.PHash, a.Extra, "phash")
	setExtra(&m.Model3DFormat, a.Extra, "model3d_format")
	setExtra(&m.VertexCount, a.Extra, "vertex_count")
	setExtra(&m.FaceCount, a.Extra, "face_count")
	setExtra(&m.HasNormals, a.Extra, "has_normals")
	setExtra(&m.HasTextures, a.Extra, "has_textures")
	setExtra(&m.Materials, a.Extra, "materials")
	setExtra(&m.BoundingBox, a.Extra, "bounding_box")
	m.Similarity = a.Similarity
	m.MatchStartLine = a.MatchStartLine
	m.MatchEndLine = a.MatchEndLine
	m.MatchSymbol = a.MatchSymbol
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
	setExtra(&m.Title, a.Extra, "title")
	setExtra(&m.Author, a.Extra, "author")
	setExtra(&m.Language, a.Extra, "language")
	setExtra(&m.WordCount, a.Extra, "word_count")
	setExtra(&m.LineCount, a.Extra, "line_count")
	setExtra(&m.PageCount, a.Extra, "page_count")
	setExtra(&m.ColumnCount, a.Extra, "column_count")
	setExtra(&m.CSVColumns, a.Extra, "csv_columns")
	setExtra(&m.RootElement, a.Extra, "root_element")
	setExtra(&m.JSONKind, a.Extra, "json_kind")
	setExtra(&m.YAMLKind, a.Extra, "yaml_kind")
	setExtra(&m.YAMLDocumentCount, a.Extra, "yaml_document_count")
	setExtra(&m.Module, a.Extra, "module")
	setExtra(&m.GoVersion, a.Extra, "go_version")
	setExtra(&m.BaseImage, a.Extra, "base_image")
	if v, ok := a.Extra["project_types"].([]string); ok && len(v) > 0 {
		m.ProjectTypes = v
	}
	setExtra(&m.ProjectType, a.Extra, "project_type")
	setExtra(&m.IsStaticSite, a.Extra, "is_static_site")
}

// applyImageAttrs covers image dimensions and EXIF (camera / GPS / exposure).
func applyImageAttrs(m *Match, a *celexpr.FileAttributes) {
	setExtra(&m.ImgWidth, a.Extra, "img_width")
	setExtra(&m.ImgHeight, a.Extra, "img_height")
	setExtra(&m.IsC2PA, a.Extra, "is_c2pa")
	setExtra(&m.C2PAClaimGenerator, a.Extra, "c2pa_claim_generator")
	setExtra(&m.C2PATitle, a.Extra, "c2pa_title")
	setExtra(&m.C2PAFormat, a.Extra, "c2pa_format")
	setExtra(&m.C2PAAIGenerated, a.Extra, "c2pa_ai_generated")
	setExtra(&m.C2PASignedBy, a.Extra, "c2pa_signed_by")
	if v, ok := a.Extra["c2pa_signed_at"].(time.Time); ok && !v.IsZero() {
		m.C2PASignedAt = v.Format(time.RFC3339)
	}
	setExtra(&m.CameraMake, a.Extra, "camera_make")
	setExtra(&m.CameraModel, a.Extra, "camera_model")
	setExtra(&m.Lens, a.Extra, "lens")
	if v, ok := a.Extra["taken_at"].(time.Time); ok && !v.IsZero() {
		m.TakenAt = v.Format(time.RFC3339)
	}
	setExtra(&m.Orientation, a.Extra, "orientation")
	setExtra(&m.GPSLat, a.Extra, "gps_lat")
	setExtra(&m.GPSLon, a.Extra, "gps_lon")
	setExtra(&m.ISO, a.Extra, "iso")
	setExtra(&m.FocalLength, a.Extra, "focal_length")
	setExtra(&m.FStop, a.Extra, "f_stop")
	setExtra(&m.ExposureTime, a.Extra, "exposure_time")
}

// applyMediaAttrs covers audio tags and video codec / colour / subtitle / replaygain attributes.
func applyMediaAttrs(m *Match, a *celexpr.FileAttributes) {
	setExtra(&m.Artist, a.Extra, "artist")
	setExtra(&m.Album, a.Extra, "album")
	setExtra(&m.AlbumArtist, a.Extra, "album_artist")
	setExtra(&m.Composer, a.Extra, "composer")
	setExtra(&m.Year, a.Extra, "year")
	setExtra(&m.Track, a.Extra, "track")
	setExtra(&m.Genre, a.Extra, "genre")
	setExtra(&m.Duration, a.Extra, "duration")
	setExtra(&m.Bitrate, a.Extra, "bitrate")
	setExtra(&m.SampleRate, a.Extra, "sample_rate")
	setExtra(&m.Channels, a.Extra, "channels")
	setExtra(&m.BitDepth, a.Extra, "bit_depth")
	setExtra(&m.NominalBitrate, a.Extra, "nominal_bitrate")
	setExtra(&m.VideoCodec, a.Extra, "video_codec")
	setExtra(&m.AudioCodec, a.Extra, "audio_codec")
	setExtra(&m.VideoWidth, a.Extra, "video_width")
	setExtra(&m.VideoHeight, a.Extra, "video_height")
	setExtra(&m.FrameRate, a.Extra, "frame_rate")
	setExtra(&m.Rotation, a.Extra, "rotation")
	setExtra(&m.ColorPrimaries, a.Extra, "color_primaries")
	setExtra(&m.ColorTransfer, a.Extra, "color_transfer")
	setExtra(&m.IsHDR, a.Extra, "is_hdr")
	setExtra(&m.Subtitles, a.Extra, "subtitles")
	if v, ok := a.Extra["subtitle_languages"].([]string); ok && len(v) > 0 {
		m.SubtitleLanguages = v
	}
	setExtra(&m.ReplayGainTrackGain, a.Extra, "replaygain_track_gain")
	setExtra(&m.ReplayGainAlbumGain, a.Extra, "replaygain_album_gain")
}

// applyArchiveBinaryAttrs covers archive entry stats and compiled-binary architecture attributes.
func applyArchiveBinaryAttrs(m *Match, a *celexpr.FileAttributes) {
	setExtra(&m.EntryCount, a.Extra, "entry_count")
	setExtra(&m.UncompressedSize, a.Extra, "uncompressed_size")
	if v, ok := a.Extra["top_level_entries"].([]string); ok && len(v) > 0 {
		m.TopLevelEntries = v
	}
	setExtra(&m.HasRootDir, a.Extra, "has_root_dir")
	if v, ok := a.Extra["architectures"].([]string); ok && len(v) > 0 {
		m.Architectures = v
	}
	setExtra(&m.Bitness, a.Extra, "bitness")
	setExtra(&m.BinaryFormat, a.Extra, "binary_format")
	setExtra(&m.BinaryType, a.Extra, "binary_type")
	setExtra(&m.IsDynamicallyLinked, a.Extra, "is_dynamically_linked")
	setExtra(&m.IsStripped, a.Extra, "is_stripped")
	setExtra(&m.EntryPoint, a.Extra, "entry_point")
}

// applyEmailSourceAttrs covers email headers, source LOC, and notebook cell counts.
func applyEmailSourceAttrs(m *Match, a *celexpr.FileAttributes) {
	if v, ok := a.Extra["email_to"].([]string); ok && len(v) > 0 {
		m.EmailTo = v
	}
	if v, ok := a.Extra["email_cc"].([]string); ok && len(v) > 0 {
		m.EmailCc = v
	}
	setExtra(&m.EmailMessageID, a.Extra, "email_message_id")
	setExtra(&m.EmailInReplyTo, a.Extra, "email_in_reply_to")
	if v, ok := a.Extra["sent_at"].(time.Time); ok && !v.IsZero() {
		m.SentAt = v.Format(time.RFC3339)
	}
	setExtra(&m.AttachmentCount, a.Extra, "attachment_count")
	setExtra(&m.EmailCount, a.Extra, "email_count")
	setExtra(&m.LOC, a.Extra, "loc")
	setExtra(&m.CommentLOC, a.Extra, "comment_loc")
	setExtra(&m.BlankLOC, a.Extra, "blank_loc")
	// imports / functions / type_names all live on Extra as []string
	// from the per-language extractors in
	// internal/content/source_symbols_*.go. Surfacing each to a typed
	// Match field unblocks fields: ["imports"|"functions"|"type_names"]
	// projection and gets the lists into the wire response.
	// #275 (imports), #278 (functions + type_names).
	setExtra(&m.Imports, a.Extra, "imports")
	setExtra(&m.Functions, a.Extra, "functions")
	setExtra(&m.TypeNames, a.Extra, "type_names")
	setExtra(&m.References, a.Extra, "references")
	setExtra(&m.MaxComplexity, a.Extra, "max_complexity")
	setExtra(&m.CellCount, a.Extra, "cell_count")
	setExtra(&m.CodeCellCount, a.Extra, "code_cell_count")
	setExtra(&m.MarkdownCellCount, a.Extra, "markdown_cell_count")
	setExtra(&m.Kernel, a.Extra, "kernel")
}

// applyFrontmatterAttrs covers markdown front-matter (format / tags / categories / draft / date).
func applyFrontmatterAttrs(m *Match, a *celexpr.FileAttributes) {
	setExtra(&m.FrontmatterFormat, a.Extra, "frontmatter_format")
	if v, ok := a.Extra["frontmatter"].(map[string]any); ok && len(v) > 0 {
		m.Frontmatter = v
	}
	if v, ok := a.Extra["tags"].([]string); ok && len(v) > 0 {
		m.Tags = v
	}
	if v, ok := a.Extra["categories"].([]string); ok && len(v) > 0 {
		m.Categories = v
	}
	setExtra(&m.Draft, a.Extra, "draft")
	if v, ok := a.Extra["date"].(time.Time); ok && !v.IsZero() {
		m.Date = v.Format(time.RFC3339)
	}
}

// applyDiskImageAttrs covers the disk-image family attributes.
func applyDiskImageAttrs(m *Match, a *celexpr.FileAttributes) {
	// Disk-image family.
	setExtra(&m.DiskImageFormat, a.Extra, "disk_image_format")
	setExtra(&m.VirtualSize, a.Extra, "virtual_size")
	setExtra(&m.DiskType, a.Extra, "disk_type")
	setExtra(&m.VolumeLabel, a.Extra, "volume_label")
	if v, ok := a.Extra["disk_image_created_at"].(time.Time); ok && !v.IsZero() {
		m.DiskImageCreatedAt = v.Format(time.RFC3339)
	}
	setExtra(&m.ClusterBits, a.Extra, "cluster_bits")
	setExtra(&m.IsEncrypted, a.Extra, "is_encrypted")
	setExtra(&m.ImageCount, a.Extra, "image_count")
}

// applyInstallPackageAttrs covers the install-package family attributes.
func applyInstallPackageAttrs(m *Match, a *celexpr.FileAttributes) {
	// Install-package family.
	setExtra(&m.PackageFormat, a.Extra, "package_format")
	setExtra(&m.PackageName, a.Extra, "package_name")
	setExtra(&m.PackageVersion, a.Extra, "package_version")
	setExtra(&m.PackageRelease, a.Extra, "package_release")
	setExtra(&m.PackageArch, a.Extra, "package_arch")
	setExtra(&m.PackageKind, a.Extra, "package_kind")
	setExtra(&m.AppImageVersion, a.Extra, "appimage_version")
}

// applyMiscAttrs covers license id, test-file detection, and symlink target.
func applyMiscAttrs(m *Match, a *celexpr.FileAttributes) {
	// License id.
	setExtra(&m.LicenseID, a.Extra, "license_id")

	// Test-file detection.
	setExtra(&m.IsTestFile, a.Extra, "is_test_file")

	// Symlink awareness — target_path lives in Extra; the two
	// booleans come from typed FileAttributes fields above.
	setExtra(&m.TargetPath, a.Extra, "target_path")
}

// applyCodesignAttrs covers the Mach-O code signature and entitlement attributes.
func applyCodesignAttrs(m *Match, a *celexpr.FileAttributes) {
	// Mach-O code signature (issue #187). Everything lives in Extra
	// since the parser is content-type-specific (not surfaced via
	// FileAttributes struct fields).
	setExtra(&m.IsCodesigned, a.Extra, "is_codesigned")
	setExtra(&m.IsAppleSigned, a.Extra, "is_apple_signed")
	setExtra(&m.IsThirdPartySigned, a.Extra, "is_third_party_signed")
	setExtra(&m.CodesignIdentifier, a.Extra, "codesign_identifier")
	setExtra(&m.CodesignTeamID, a.Extra, "codesign_team_id")
	setExtra(&m.CodesignHashType, a.Extra, "codesign_hash_type")
	setExtra(&m.CodesignHardenedRuntime, a.Extra, "codesign_hardened_runtime")
	setExtra(&m.CodesignLibraryValidation, a.Extra, "codesign_library_validation")
	setExtra(&m.CodesignKilled, a.Extra, "codesign_killed")
	setExtra(&m.CodesignAdhoc, a.Extra, "codesign_adhoc")
	if v, ok := a.Extra["entitlements"].([]string); ok && len(v) > 0 {
		m.Entitlements = v
	}
	setExtra(&m.EntitlementAppSandbox, a.Extra, "entitlement_app_sandbox")
	setExtra(&m.EntitlementFullDiskAccess, a.Extra, "entitlement_full_disk_access")
	setExtra(&m.EntitlementNetworkClient, a.Extra, "entitlement_network_client")
	setExtra(&m.EntitlementNetworkServer, a.Extra, "entitlement_network_server")
}

// applyPlistAttrs covers the Apple property-list attributes.
func applyPlistAttrs(m *Match, a *celexpr.FileAttributes) {
	// Apple property list (issue #185). is_plist comes from the typed
	// FileAttributes field above; the rest live in Extra.
	setExtra(&m.PlistFormat, a.Extra, "plist_format")
	setExtra(&m.PlistRootKind, a.Extra, "plist_root_kind")
	setExtra(&m.PlistKind, a.Extra, "plist_kind")
	setExtra(&m.PlistBundleIdentifier, a.Extra, "plist_bundle_identifier")
	setExtra(&m.PlistBundleName, a.Extra, "plist_bundle_name")
	setExtra(&m.PlistBundleVersion, a.Extra, "plist_bundle_version")
	setExtra(&m.PlistBundleShortVersion, a.Extra, "plist_bundle_short_version")
	setExtra(&m.PlistExecutable, a.Extra, "plist_executable")
	setExtra(&m.PlistMinOSVersion, a.Extra, "plist_min_os_version")
	setExtra(&m.PlistLabel, a.Extra, "plist_label")
	setExtra(&m.PlistProgram, a.Extra, "plist_program")
	if v, ok := a.Extra["plist_program_arguments"].([]string); ok && len(v) > 0 {
		m.PlistProgramArguments = v
	}
	setExtra(&m.PlistRunAtLoad, a.Extra, "plist_run_at_load")
	setExtra(&m.PlistKeepAlive, a.Extra, "plist_keep_alive")
}

// applyBytecodeAttrs covers the VM-bytecode family attributes.
func applyBytecodeAttrs(m *Match, a *celexpr.FileAttributes) {
	// VM-bytecode family attributes.
	setExtra(&m.BytecodeFormat, a.Extra, "bytecode_format")
	setExtra(&m.RuntimeVersion, a.Extra, "runtime_version")
	setExtra(&m.ClassName, a.Extra, "class_name")
	setExtra(&m.SuperClass, a.Extra, "super_class")
	if v, ok := a.Extra["interfaces"].([]string); ok && len(v) > 0 {
		m.Interfaces = v
	}
	setExtra(&m.MethodCount, a.Extra, "method_count")
	setExtra(&m.FieldCount, a.Extra, "field_count")
	if v, ok := a.Extra["access_flags"].([]string); ok && len(v) > 0 {
		m.AccessFlags = v
	}
	setExtra(&m.PythonVersion, a.Extra, "python_version")
	if v, ok := a.Extra["source_mtime"].(time.Time); ok && !v.IsZero() {
		m.SourceMtime = v.Format(time.RFC3339)
	}
	setExtra(&m.WasmVersion, a.Extra, "wasm_version")
	setExtra(&m.SectionCount, a.Extra, "section_count")
	setExtra(&m.ImportCount, a.Extra, "import_count")
	setExtra(&m.ExportCount, a.Extra, "export_count")
}

// applyScienceAttrs covers the science-data family (FITS / VOTable / HDF5 / PDS / CDF).
func applyScienceAttrs(m *Match, a *celexpr.FileAttributes) {
	// Science-data family.
	setExtra(&m.ScienceFormat, a.Extra, "science_format")
	setExtra(&m.Telescope, a.Extra, "telescope")
	setExtra(&m.Instrument, a.Extra, "instrument")
	setExtra(&m.Object, a.Extra, "object")
	setExtra(&m.Observer, a.Extra, "observer")
	setExtra(&m.DateObs, a.Extra, "date_obs")
	setExtra(&m.Exptime, a.Extra, "exptime")
	setExtra(&m.Filter, a.Extra, "filter")
	setExtra(&m.Airmass, a.Extra, "airmass")
	setExtra(&m.RA, a.Extra, "ra")
	setExtra(&m.Dec, a.Extra, "dec")
	setExtra(&m.Bitpix, a.Extra, "bitpix")
	setExtra(&m.Naxis, a.Extra, "naxis")
	setExtra(&m.Naxis1, a.Extra, "naxis1")
	setExtra(&m.Naxis2, a.Extra, "naxis2")
	setExtra(&m.HDUCount, a.Extra, "hdu_count")
	setExtra(&m.FITSKind, a.Extra, "fits_kind")

	// VOTable.
	setExtra(&m.VOTableVersion, a.Extra, "votable_version")
	setExtra(&m.TableCount, a.Extra, "table_count")
	setExtra(&m.TotalRows, a.Extra, "total_rows")
	if v, ok := a.Extra["field_names"].([]string); ok && len(v) > 0 {
		m.FieldNames = v
	}
	if v, ok := a.Extra["field_units"].([]string); ok && len(v) > 0 {
		m.FieldUnits = v
	}
	if v, ok := a.Extra["field_ucds"].([]string); ok && len(v) > 0 {
		m.FieldUCDs = v
	}
	setExtra(&m.VOTableDataFormat, a.Extra, "votable_data_format")

	// HDF5.
	setExtra(&m.HDF5FormatVersion, a.Extra, "hdf5_format_version")
	setExtra(&m.HDF5SizeOfOffsets, a.Extra, "hdf5_size_of_offsets")
	setExtra(&m.HDF5SizeOfLengths, a.Extra, "hdf5_size_of_lengths")

	// PDS.
	setExtra(&m.PDSVersion, a.Extra, "pds_version")
	setExtra(&m.MissionName, a.Extra, "mission_name")
	setExtra(&m.SpacecraftName, a.Extra, "spacecraft_name")
	setExtra(&m.InstrumentName, a.Extra, "instrument_name")
	setExtra(&m.TargetName, a.Extra, "target_name")
	setExtra(&m.ProductID, a.Extra, "product_id")
	setExtra(&m.StartTime, a.Extra, "start_time")

	// CDF.
	setExtra(&m.CDFVersion, a.Extra, "cdf_version")
	setExtra(&m.CDFEncoding, a.Extra, "cdf_encoding")
	setExtra(&m.CDFMajority, a.Extra, "cdf_majority")
	setExtra(&m.VariableCount, a.Extra, "variable_count")
	setExtra(&m.AttributeCount, a.Extra, "attribute_count")
}

// applyDatabaseAttrs covers the SQLite / database family attributes.
func applyDatabaseAttrs(m *Match, a *celexpr.FileAttributes) {
	// Database family.
	setExtra(&m.DatabaseFormat, a.Extra, "database_format")
	setExtra(&m.SQLitePageSize, a.Extra, "sqlite_page_size")
	setExtra(&m.SQLiteFormatVersion, a.Extra, "sqlite_format_version")
	setExtra(&m.SQLitePageCount, a.Extra, "sqlite_page_count")
	setExtra(&m.SQLiteSchemaVersion, a.Extra, "sqlite_schema_version")
	setExtra(&m.SQLiteTextEncoding, a.Extra, "sqlite_text_encoding")
	setExtra(&m.SQLiteUserVersion, a.Extra, "sqlite_user_version")
	setExtra(&m.SQLiteApplicationID, a.Extra, "sqlite_application_id")
	setExtra(&m.SQLiteApplicationName, a.Extra, "sqlite_application_name")
	setExtra(&m.SQLiteTableCount, a.Extra, "sqlite_table_count")
	setExtra(&m.SQLiteViewCount, a.Extra, "sqlite_view_count")
	setExtra(&m.SQLiteIndexCount, a.Extra, "sqlite_index_count")
	setExtra(&m.SQLiteTriggerCount, a.Extra, "sqlite_trigger_count")
	if v, ok := a.Extra["sqlite_table_names"].([]string); ok && len(v) > 0 {
		m.SQLiteTableNames = v
	}
	setExtra(&m.SQLiteSchemaFingerprint, a.Extra, "sqlite_schema_fingerprint")
	setExtra(&m.SQLiteFTSTableCount, a.Extra, "sqlite_fts_table_count")
	if v, ok := a.Extra["sqlite_fts_table_names"].([]string); ok && len(v) > 0 {
		m.SQLiteFTSTableNames = v
	}
	setExtra(&m.SQLiteWALFormatVersion, a.Extra, "sqlite_wal_format_version")
	setExtra(&m.SQLiteWALPageSize, a.Extra, "sqlite_wal_page_size")
	setExtra(&m.SQLiteWALCheckpointSeq, a.Extra, "sqlite_wal_checkpoint_seq")
	setExtra(&m.SQLiteWALFrameCount, a.Extra, "sqlite_wal_frame_count")
	setExtra(&m.SQLiteWALByteOrder, a.Extra, "sqlite_wal_byte_order")
}

// applyBookmarkAttrs covers the browser-bookmark attributes.
func applyBookmarkAttrs(m *Match, a *celexpr.FileAttributes) {
	// Browser bookmarks (issue #188). Bool predicates come from the
	// typed FileAttributes fields above; per-file attrs live in Extra.
	setExtra(&m.BookmarkCount, a.Extra, "bookmark_count")
	setExtra(&m.BookmarkFolderCount, a.Extra, "bookmark_folder_count")
	if v, ok := a.Extra["bookmark_folders"].([]string); ok && len(v) > 0 {
		m.BookmarkFolders = v
	}
	if v, ok := a.Extra["bookmark_urls"].([]string); ok && len(v) > 0 {
		m.BookmarkURLs = v
	}
	if v, ok := a.Extra["bookmark_titles"].([]string); ok && len(v) > 0 {
		m.BookmarkTitles = v
	}
	setExtra(&m.BrowserVendor, a.Extra, "browser_vendor")
	setExtra(&m.BookmarkProfile, a.Extra, "bookmark_profile")
}

// applyChatAttrs covers the chat-export attributes.
func applyChatAttrs(m *Match, a *celexpr.FileAttributes) {
	// Chat exports (issue #214). Bool predicates come from the typed
	// FileAttributes fields above; per-file attrs live in Extra.
	setExtra(&m.ChatMessageCount, a.Extra, "chat_message_count")
	if v, ok := a.Extra["chat_participants"].([]string); ok && len(v) > 0 {
		m.ChatParticipants = v
	}
	setExtra(&m.ChatChannel, a.Extra, "chat_channel")
	setExtra(&m.ChatWorkspace, a.Extra, "chat_workspace")
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
	setExtra(&m.IsVariableFont, a.Extra, "is_variable_font")
	setExtra(&m.IsColorFont, a.Extra, "is_color_font")
	setExtra(&m.IsMonospaceFont, a.Extra, "is_monospace_font")
	setExtra(&m.IsItalicFont, a.Extra, "is_italic_font")
	setExtra(&m.IsBoldFont, a.Extra, "is_bold_font")
	setExtra(&m.FontFormat, a.Extra, "font_format")
	setExtra(&m.FontOutlineKind, a.Extra, "font_outline_kind")
	setExtra(&m.FontFamily, a.Extra, "font_family")
	setExtra(&m.FontSubfamily, a.Extra, "font_subfamily")
	setExtra(&m.FontFullName, a.Extra, "font_full_name")
	setExtra(&m.FontVersion, a.Extra, "font_version")
	setExtra(&m.FontPostScriptName, a.Extra, "font_postscript_name")
	setExtra(&m.FontManufacturer, a.Extra, "font_manufacturer")
	setExtra(&m.FontDesigner, a.Extra, "font_designer")
	setExtra(&m.FontLicense, a.Extra, "font_license")
	setExtra(&m.FontLicenseURL, a.Extra, "font_license_url")
	setExtra(&m.FontTypographicFamily, a.Extra, "font_typographic_family")
	setExtra(&m.FontWeight, a.Extra, "font_weight")
	setExtra(&m.FontWidth, a.Extra, "font_width")
	setExtra(&m.FontEmbedding, a.Extra, "font_embedding")
	setExtra(&m.FontPanose, a.Extra, "font_panose")
	if v, ok := a.Extra["font_unicode_ranges"].([]string); ok && len(v) > 0 {
		m.FontUnicodeRanges = v
	}
	setExtra(&m.FontRevision, a.Extra, "font_revision")
	setExtra(&m.FontUnitsPerEm, a.Extra, "font_units_per_em")
	if v, ok := a.Extra["font_mac_style"].([]string); ok && len(v) > 0 {
		m.FontMacStyle = v
	}
	setExtra(&m.FontItalicAngle, a.Extra, "font_italic_angle")
	setExtra(&m.FontGlyphCount, a.Extra, "font_glyph_count")
	setExtra(&m.FontAxisCount, a.Extra, "font_axis_count")
	if v, ok := a.Extra["font_axes"].([]string); ok && len(v) > 0 {
		m.FontAxes = v
	}
	setExtra(&m.FontCollectionCount, a.Extra, "font_collection_count")
	if v, ok := a.Extra["font_collection_families"].([]string); ok && len(v) > 0 {
		m.FontCollectionFamilies = v
	}
	setExtra(&m.WOFF2TotalSfntSize, a.Extra, "woff2_total_sfnt_size")
	setExtra(&m.WOFF2TotalCompressedSize, a.Extra, "woff2_total_compressed_size")
}

// applyXattrAttrs covers the extended-attribute (xattr) family.
func applyXattrAttrs(m *Match, a *celexpr.FileAttributes) {
	// Extended attributes (issue #193). Bool umbrellas come from the
	// typed FileAttributes fields above; the rest live in Extra.
	if v, ok := a.Extra["xattr_keys"].([]string); ok && len(v) > 0 {
		m.XattrKeys = v
	}
	setExtra(&m.XattrCount, a.Extra, "xattr_count")
	setExtra(&m.QuarantineAgent, a.Extra, "quarantine_agent")
	setExtra(&m.QuarantineEventID, a.Extra, "quarantine_event_id")
	setExtra(&m.QuarantineSourceURL, a.Extra, "quarantine_source_url")
	setExtra(&m.QuarantineReferrerURL, a.Extra, "quarantine_referrer_url")
	setExtra(&m.QuarantineDownloadDate, a.Extra, "quarantine_download_date")
	setExtra(&m.QuarantineUserApproved, a.Extra, "quarantine_user_approved")
	if v, ok := a.Extra["finder_tags"].([]string); ok && len(v) > 0 {
		m.FinderTags = v
	}
	setExtra(&m.FinderColor, a.Extra, "finder_color")
	setExtra(&m.HasFinderComment, a.Extra, "has_finder_comment")
}

// setExtra copies a.Extra[key] onto *dst when the key is present and holds a
// value of dst's type. It captures the dominant attribute-plumbing idiom in
// this file — `if v, ok := a.Extra["k"].(T); ok { m.F = v }` — as one
// declarative line (issue #384). T is inferred from dst, so the call site
// never repeats the type. Pass-through only: sites with extra guards
// (`&& len(v) > 0`, `&& !v.IsZero()`) or transforms (`v.Format(...)`,
// `int(v)`) stay spelled out.
func setExtra[T any](dst *T, extra map[string]any, key string) {
	if v, ok := extra[key].(T); ok {
		*dst = v
	}
}
