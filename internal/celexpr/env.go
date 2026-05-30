package celexpr

import (
	"fmt"
	"github.com/google/cel-go/cel"
)

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
		// Chat-export content types (issue #214).
		cel.Variable("is_chat_export", cel.BoolType),
		cel.Variable("is_slack_export", cel.BoolType),
		cel.Variable("is_discord_export", cel.BoolType),
		cel.Variable("is_signal_export", cel.BoolType),
		cel.Variable("chat_message_count", cel.IntType),
		cel.Variable("chat_participants", cel.ListType(cel.StringType)),
		cel.Variable("chat_channel", cel.StringType),
		cel.Variable("chat_workspace", cel.StringType),
		cel.Variable("chat_start_at", cel.TimestampType),
		cel.Variable("chat_end_at", cel.TimestampType),
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
		// Image OCR (issue #189). Populated only when BuildOptions.
		// OCRImages is set AND a provider (macOS Vision today) is
		// registered + Available. `body` reuses the existing CEL var
		// so `body.contains(...)` queries work the same as for
		// markdown / SQLite FTS / etc.
		cel.Variable("ocr_confidence", cel.DoubleType),
		cel.Variable("ocr_language", cel.StringType),
		cel.Variable("ocr_provider", cel.StringType),
		// Perceptual image hash (issue #208). 16-char hex string;
		// empty unless --with-phash is set or the expression
		// references image_similar_to(...).
		cel.Variable("phash", cel.StringType),
		// 3D model content types (issue #213).
		cel.Variable("is_3d_model", cel.BoolType),
		cel.Variable("is_stl", cel.BoolType),
		cel.Variable("is_obj", cel.BoolType),
		cel.Variable("is_gltf", cel.BoolType),
		cel.Variable("model3d_format", cel.StringType),
		cel.Variable("vertex_count", cel.IntType),
		cel.Variable("face_count", cel.IntType),
		cel.Variable("has_normals", cel.BoolType),
		cel.Variable("has_textures", cel.BoolType),
		cel.Variable("materials", cel.ListType(cel.StringType)),
		cel.Variable("bounding_box", cel.ListType(cel.DoubleType)),
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
		cel.Variable("functions", cel.ListType(cel.StringType)),
		cel.Variable("type_names", cel.ListType(cel.StringType)),
		cel.Variable("imports", cel.ListType(cel.StringType)),
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
	opts = append(opts, imageFunctions()...)
	opts = append(opts, secretFunctions()...)
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
