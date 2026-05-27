package celexpr

import "strings"

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
	case "chat/slack-export":
		attrs.IsSlackExport = true
	case "chat/discord-export":
		attrs.IsDiscordExport = true
	case "chat/signal-cli":
		attrs.IsSignalExport = true
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

	// 3D model content types. Per-format flag fires here; family flag
	// Is3DModel fires via the `model3d/` prefix block below.
	case "model3d/stl":
		attrs.IsSTL = true
	case "model3d/obj":
		attrs.IsOBJ = true
	case "model3d/gltf":
		attrs.IsGLTF = true
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
	if strings.HasPrefix(name, "chat/") {
		attrs.IsChatExport = true
	}
	if strings.HasPrefix(name, "font/") {
		attrs.IsFont = true
	}
	if strings.HasPrefix(name, "image/raw-") {
		attrs.IsRawPhoto = true
	}
	if strings.HasPrefix(name, "model3d/") {
		attrs.Is3DModel = true
	}
}
