# Recipes — Install packages

Install-package content types: `install/pkg` (macOS XAR `.pkg`), `install/deb` (Debian `.deb` — ar archive), `install/rpm` (Red Hat `.rpm`), `install/appimage` (Linux AppImage — ELF + appended SquashFS). Umbrella boolean `is_install_package`.

Each parser reads the format's fixed-offset header only — no extraction, no payload walk. Pure stdlib, no third-party libs.

## All install packages under a directory

```sh
file-search-on 'is_install_package' -d ~/Downloads
file-search-on 'is_install_package' -d /var/cache/yum   # RPM cache
file-search-on 'is_install_package' -d /var/cache/apt   # APT cache
```

By format:

```sh
file-search-on 'is_pkg'      -d ~/Downloads               # macOS installers
file-search-on 'is_deb'      -d ~/Downloads
file-search-on 'is_rpm'      -d ~/Downloads
file-search-on 'is_appimage' -d ~/Applications
```

## RPM metadata reconnaissance

The RPM Lead carries enough for triage — name, version, release, architecture, and binary-vs-source classification:

```sh
# Find specific package by name
file-search-on 'is_rpm && package_name == "openssh-clients"' -d /var/cache/yum

# All x86_64 RPMs
file-search-on 'is_rpm && package_arch == "x86_64"' -d ~/Downloads

# Source RPMs only (.src.rpm)
file-search-on 'is_rpm && package_kind == "source"' -d ~/Downloads

# Find a specific build (RHEL 9 builds tagged with `.el9` release suffix)
file-search-on 'is_rpm && package_release.contains("el9")' -d ~/Downloads

# Sort RPMs by name for a clean audit listing
file-search-on 'is_rpm' -d ~/Downloads --sort-by package_name -o verbose
```

## AppImage triage

AppImages are portable Linux apps that bundle their dependencies. The `appimage_version` attribute distinguishes the on-disk format (v1 was superseded in 2017; v2 is current):

```sh
# Find legacy AppImage v1 files (candidates for re-bundling)
file-search-on 'is_appimage && appimage_version == 1' -d ~/Applications

# All v2 AppImages
file-search-on 'is_appimage && appimage_version == 2' -d ~/Applications

# Big AppImages (~hundreds of MB is common for Electron-based ones)
file-search-on 'is_appimage' -d ~/Applications --sort-by size --order desc --limit 10
```

## Disk-usage audits

Pair with `stats` to summarise what's eating space:

```sh
# How much disk space is each package format using?
file-search-on stats 'is_install_package' -d ~/Downloads --group-by package_format

# What package_kind dominates the cache?
file-search-on stats 'is_rpm' -d /var/cache/yum --group-by package_kind

# Per-architecture breakdown
file-search-on stats 'is_rpm' -d /var/cache/yum --group-by package_arch
```

## Cross-format queries

```sh
# Any non-x86_64 packages in this download dir (cross-arch builds)
file-search-on 'is_install_package && package_arch != "" && package_arch != "x86_64"' -d ~/Downloads

# Packages with no version metadata extractable (DEB / PKG / AppImage today)
file-search-on 'is_install_package && package_version == ""' -d ~/Downloads
```

## Caveats

- **DEB and PKG don't currently expose name + version.** The Debian control file lives inside `control.tar.{gz,xz,zst}` (a TAR inside the outer ar); the XAR Table-of-Contents is compressed XML. Both are doable but each adds a nested-archive walk. The v1 surface is `package_format = "deb"` / `"xar"` plus `package_kind`. Follow-up tracked separately.
- **AppImage detection is by extension only.** The 4-byte AppImage marker lives at file offset 8 (overlaid on the underlying ELF's e_ident padding), out of reach of the start-of-file magic sniffer. A file named `foo.AppImage` without the correct marker surfaces as `install/appimage` content type but empty attributes (the parser bails on missing marker).
- **No `.msi` support.** Microsoft Installer files use the Compound File Binary Format (a structured-storage container, complex enough to warrant its own dependency). Out of scope for now.
- **`.snap` / `.flatpak` are out of scope.** Snap is a SquashFS, Flatpak is an OSTree commit — different worlds. Tracked as potential follow-ups.
- **Reading files INSIDE the package** (the actual installer payload) is out of scope. This recipe page is for finding and triaging install-package files themselves.
