package celexpr

import (
	"time"

	"github.com/google/cel-go/cel"
)

// fileAttrsActivation is a cel.Activation backed directly by
// *FileAttributes — no per-Evaluate map allocation. Replaces the
// 100-entry map literal that dominated walker allocations after the
// markdown scanner was right-sized (#90 + #91 profiles flagged
// Evaluate as the next-biggest contributor at ~35%).
//
// Struct fields short-circuit first; Extra-driven attributes come
// next; declared variables that the matched content-type family
// didn't populate fall back to a static zero default.
//
// Sharing the zero defaults map across calls is safe: cel-go treats
// activation inputs as read-only, so the immutable empty slice /
// empty map sentinels never get mutated through it.
type fileAttrsActivation struct {
	attrs *FileAttributes
}

// ResolveName returns the CEL-visible value for the named variable.
// Returns false only for names that aren't declared in the env —
// which shouldn't happen since the env declares the full set up
// front and the CEL compiler rejects expressions referencing
// undeclared vars.
func (a *fileAttrsActivation) ResolveName(name string) (any, bool) {
	switch name {
	case "name":
		return a.attrs.Name, true
	case "path":
		return a.attrs.Path, true
	case "dir":
		return a.attrs.Dir, true
	case "size":
		return a.attrs.Size, true
	case "ext":
		return a.attrs.Ext, true
	case "content_type":
		return a.attrs.ContentType, true
	case "mod_time":
		return a.attrs.ModTime, true
	case "is_markdown":
		return a.attrs.IsMarkdown, true
	case "is_json":
		return a.attrs.IsJSON, true
	case "is_xml":
		return a.attrs.IsXML, true
	case "is_html":
		return a.attrs.IsHTML, true
	case "is_pdf":
		return a.attrs.IsPDF, true
	case "is_image":
		return a.attrs.IsImage, true
	case "is_text":
		return a.attrs.IsText, true
	case "is_csv":
		return a.attrs.IsCSV, true
	case "is_epub":
		return a.attrs.IsEPUB, true
	case "is_office":
		return a.attrs.IsOffice, true
	case "is_audio":
		return a.attrs.IsAudio, true
	case "is_video":
		return a.attrs.IsVideo, true
	case "is_archive":
		return a.attrs.IsArchive, true
	case "is_binary":
		return a.attrs.IsBinary, true
	case "is_email":
		return a.attrs.IsEmail, true
	case "is_source":
		return a.attrs.IsSource, true
	case "is_notebook":
		return a.attrs.IsNotebook, true
	case "is_yaml":
		return a.attrs.IsYAML, true
	case "is_toml":
		return a.attrs.IsTOML, true
	case "is_dockerfile":
		return a.attrs.IsDockerfile, true
	case "is_makefile":
		return a.attrs.IsMakefile, true
	case "is_justfile":
		return a.attrs.IsJustfile, true
	case "is_rakefile":
		return a.attrs.IsRakefile, true
	case "is_license":
		return a.attrs.IsLicense, true
	case "is_changelog":
		return a.attrs.IsChangelog, true
	case "is_contributing":
		return a.attrs.IsContributing, true
	case "is_codeowners":
		return a.attrs.IsCodeowners, true
	case "is_gitignore":
		return a.attrs.IsGitignore, true
	case "is_dockerignore":
		return a.attrs.IsDockerignore, true
	case "is_gomod":
		return a.attrs.IsGomod, true
	case "is_node_manifest":
		return a.attrs.IsNodeManifest, true
	case "is_cargo_manifest":
		return a.attrs.IsCargoManifest, true
	case "is_pipfile":
		return a.attrs.IsPipfile, true
	case "is_python_reqs":
		return a.attrs.IsPythonReqs, true
	case "is_gemfile":
		return a.attrs.IsGemfile, true
	case "is_procfile":
		return a.attrs.IsProcfile, true
	case "is_vagrantfile":
		return a.attrs.IsVagrantfile, true
	case "is_build":
		return a.attrs.IsBuild, true
	case "is_repo_meta":
		return a.attrs.IsRepoMeta, true
	case "is_ignore":
		return a.attrs.IsIgnore, true
	case "is_manifest":
		return a.attrs.IsManifest, true
	case "is_platform":
		return a.attrs.IsPlatform, true
	case "is_ds_store":
		return a.attrs.IsDSStore, true
	case "is_localized":
		return a.attrs.IsLocalized, true
	case "is_thumbs_db":
		return a.attrs.IsThumbsDB, true
	case "is_desktop_ini":
		return a.attrs.IsDesktopIni, true
	case "is_kde_directory":
		return a.attrs.IsKDEDirectory, true
	case "is_macos_metadata":
		return a.attrs.IsMacOSMetadata, true
	case "is_windows_metadata":
		return a.attrs.IsWindowsMetadata, true
	case "is_linux_metadata":
		return a.attrs.IsLinuxMetadata, true
	case "is_system_metadata":
		return a.attrs.IsSystemMetadata, true
	case "is_dmg":
		return a.attrs.IsDMG, true
	case "is_iso":
		return a.attrs.IsISO, true
	case "is_vhd":
		return a.attrs.IsVHD, true
	case "is_vhdx":
		return a.attrs.IsVHDX, true
	case "is_vmdk":
		return a.attrs.IsVMDK, true
	case "is_qcow2":
		return a.attrs.IsQCOW2, true
	case "is_wim":
		return a.attrs.IsWIM, true
	case "is_disk_image":
		return a.attrs.IsDiskImage, true
	case "is_pkg":
		return a.attrs.IsPkg, true
	case "is_deb":
		return a.attrs.IsDeb, true
	case "is_rpm":
		return a.attrs.IsRPM, true
	case "is_appimage":
		return a.attrs.IsAppImage, true
	case "is_install_package":
		return a.attrs.IsInstallPackage, true
	case "is_class":
		return a.attrs.IsClass, true
	case "is_pyc":
		return a.attrs.IsPyc, true
	case "is_wasm":
		return a.attrs.IsWasm, true
	case "is_bytecode":
		return a.attrs.IsBytecode, true
	case "is_fits":
		return a.attrs.IsFITS, true
	case "is_votable":
		return a.attrs.IsVotable, true
	case "is_hdf5":
		return a.attrs.IsHDF5, true
	case "is_pds3":
		return a.attrs.IsPDS3, true
	case "is_pds4":
		return a.attrs.IsPDS4, true
	case "is_pds":
		return a.attrs.IsPDS, true
	case "is_cdf":
		return a.attrs.IsCDF, true
	case "is_science_data":
		return a.attrs.IsScienceData, true
	case "is_symlink":
		return a.attrs.IsSymlink, true
	case "is_broken_symlink":
		return a.attrs.IsBrokenSymlink, true
	case "md5":
		return a.attrs.MD5, true
	case "sha1":
		return a.attrs.SHA1, true
	case "sha256":
		return a.attrs.SHA256, true
	case "created_at":
		return a.attrs.CreatedAt, true
	case "metadata_changed_at":
		return a.attrs.MetadataChangedAt, true
	case "is_btime_anomaly":
		return a.attrs.IsBtimeAnomaly, true
	case "magic_content_type":
		return a.attrs.MagicContentType, true
	case "extension_content_type":
		return a.attrs.ExtensionContentType, true
	case "is_disguised":
		return a.attrs.IsDisguised, true
	case "is_known_good":
		return a.attrs.IsKnownGood, true
	case "is_known_bad":
		return a.attrs.IsKnownBad, true
	case "similarity":
		return a.attrs.Similarity, true
	}
	if v, ok := a.attrs.Extra[name]; ok {
		return v, true
	}
	if v, ok := zeroDefaults[name]; ok {
		return v, true
	}
	return nil, false
}

// Parent returns nil — fileAttrsActivation is a leaf binding, the
// only activation cel-go sees per Evaluate call.
func (a *fileAttrsActivation) Parent() cel.Activation {
	return nil
}

// zeroDefaults holds the type-appropriate zero value for every
// declared CEL variable that's populated through FileAttributes.Extra.
// Built once at package init and never mutated.
var zeroDefaults = map[string]any{
	"title":                 "",
	"body":                  "",
	"word_count":            int64(0),
	"line_count":            int64(0),
	"column_count":          int64(0),
	"csv_columns":           []string{},
	"language":              "",
	"page_count":            int64(0),
	"author":                "",
	"root_element":          "",
	"json_kind":             "",
	"yaml_kind":             "",
	"yaml_document_count":   int64(0),
	"module":                "",
	"go_version":            "",
	"base_image":            "",
	"project_types":         []string{},
	"project_type":          "",
	"is_static_site":        false,
	"img_width":             int64(0),
	"img_height":            int64(0),
	"camera_make":           "",
	"camera_model":          "",
	"lens":                  "",
	"taken_at":              time.Time{},
	"orientation":           int64(0),
	"gps_lat":               float64(0),
	"gps_lon":               float64(0),
	"iso":                   int64(0),
	"focal_length":          float64(0),
	"f_stop":                float64(0),
	"exposure_time":         float64(0),
	"artist":                "",
	"album":                 "",
	"album_artist":          "",
	"composer":              "",
	"year":                  int64(0),
	"track":                 int64(0),
	"genre":                 "",
	"duration":              float64(0),
	"bitrate":               int64(0),
	"sample_rate":           int64(0),
	"channels":              int64(0),
	"bit_depth":             int64(0),
	"video_codec":           "",
	"audio_codec":           "",
	"video_width":           int64(0),
	"video_height":          int64(0),
	"frame_rate":            float64(0),
	"rotation":              int64(0),
	"nominal_bitrate":       int64(0),
	"color_primaries":       "",
	"color_transfer":        "",
	"is_hdr":                false,
	"subtitles":             false,
	"subtitle_languages":    []string{},
	"replaygain_track_gain": float64(0),
	"replaygain_album_gain": float64(0),
	"entry_count":           int64(0),
	"uncompressed_size":     int64(0),
	"top_level_entries":     []string{},
	"has_root_dir":          false,
	"architectures":         []string{},
	"bitness":               int64(0),
	"binary_format":         "",
	"binary_type":           "",
	"is_dynamically_linked": false,
	"is_stripped":           false,
	"entry_point":           int64(0),
	"email_to":              []string{},
	"email_cc":              []string{},
	"email_message_id":      "",
	"email_in_reply_to":     "",
	"sent_at":               time.Time{},
	"attachment_count":      int64(0),
	"email_count":           int64(0),
	"loc":                   int64(0),
	"comment_loc":           int64(0),
	"blank_loc":             int64(0),
	"cell_count":            int64(0),
	"code_cell_count":       int64(0),
	"markdown_cell_count":   int64(0),
	"kernel":                "",
	"frontmatter":           map[string]any{},
	"frontmatter_format":    "",
	"tags":                  []string{},
	"categories":            []string{},
	"draft":                 false,
	"date":                  time.Time{},

	// Disk-image family.
	"disk_image_format": "",
	"virtual_size":      int64(0),
	"disk_type":         "",
	"volume_label":          "",
	"disk_image_created_at": time.Time{},
	"cluster_bits":          int64(0),
	"is_encrypted":      false,
	"image_count":       int64(0),

	// Install-package family.
	"package_format":   "",
	"package_name":     "",
	"package_version":  "",
	"package_release":  "",
	"package_arch":     "",
	"package_kind":     "",
	"appimage_version": int64(0),

	// License + test-file detection.
	"license_id":   "",
	"is_test_file": false,

	// Symlink awareness — populated by BuildAttributesWith via Lstat.
	"target_path": "",

	// VM-bytecode family.
	"bytecode_format": "",
	"runtime_version": "",
	"class_name":      "",
	"super_class":     "",
	"interfaces":      []string{},
	"method_count":    int64(0),
	"field_count":     int64(0),
	"access_flags":    []string{},
	"python_version":  "",
	"source_mtime":    time.Time{},
	"wasm_version":    int64(0),
	"section_count":   int64(0),
	"import_count":    int64(0),
	"export_count":    int64(0),

	// Science-data family (issue #158).
	"science_format": "",
	"telescope":      "",
	"instrument":     "",
	"object":         "",
	"observer":       "",
	"date_obs":       "",
	"exptime":        float64(0),
	"filter":         "",
	"airmass":        float64(0),
	"ra":             float64(0),
	"dec":            float64(0),
	"bitpix":         int64(0),
	"naxis":          int64(0),
	"naxis1":         int64(0),
	"naxis2":         int64(0),
	"hdu_count":      int64(0),
	"fits_kind":      "",

	// VOTable (issue #160).
	"votable_version":     "",
	"table_count":         int64(0),
	"total_rows":          int64(0),
	"field_names":         []string{},
	"field_units":         []string{},
	"field_ucds":          []string{},
	"votable_data_format": "",

	// HDF5 (issue #161). Superblock-only attributes for v1.
	"hdf5_format_version":  int64(0),
	"hdf5_size_of_offsets": int64(0),
	"hdf5_size_of_lengths": int64(0),

	// PDS (issue #162). Shared across PDS3 PVL + PDS4 XML variants.
	"pds_version":     "",
	"mission_name":    "",
	"spacecraft_name": "",
	"instrument_name": "",
	"target_name":     "",
	"product_id":      "",
	"start_time":      "",

	// CDF (issue #163). NASA Common Data Format for heliophysics.
	"cdf_version":     "",
	"cdf_encoding":    "",
	"cdf_majority":    "",
	"variable_count":  int64(0),
	"attribute_count": int64(0),
}
