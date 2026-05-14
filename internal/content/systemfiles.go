package content

import (
	"context"
	"io/fs"
)

// OS-generated metadata files — the cruft that file managers leave
// behind everywhere. Detection only; the binary formats (.DS_Store,
// Thumbs.db) require non-trivial parsers and the INI ones
// (Desktop.ini, .directory) don't carry agent-interesting attributes
// today. Lives under the system/ family alongside future Windows /
// Linux additions. The corresponding family predicates
// (is_macos_metadata / is_windows_metadata / is_linux_metadata /
// is_system_metadata) plus per-type predicates (is_ds_store /
// is_localized / is_thumbs_db / is_desktop_ini / is_kde_directory)
// are wired in internal/celexpr/evaluator.go's setTypeFlags.

func init() {
	Register(&dsStoreType{})
	Register(&localizedType{})
	Register(&thumbsDBType{})
	Register(&desktopIniType{})
	Register(&kdeDirectoryType{})
}

// dsStoreType matches macOS Finder's window-state file. Binary
// (Apple's proprietary format with B-tree records); parser out of
// scope.
type dsStoreType struct{}

func (*dsStoreType) Name() string         { return "system/macos-ds-store" }
func (*dsStoreType) Extensions() []string { return nil }
func (*dsStoreType) MagicBytes() [][]byte { return nil }
func (*dsStoreType) Filenames() []string  { return []string{".DS_Store"} }
func (*dsStoreType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// localizedType matches macOS's empty marker file that tells Finder
// to use a localized version of the containing folder's display name.
// Typically zero bytes; nothing to parse.
type localizedType struct{}

func (*localizedType) Name() string         { return "system/macos-localized" }
func (*localizedType) Extensions() []string { return nil }
func (*localizedType) MagicBytes() [][]byte { return nil }
func (*localizedType) Filenames() []string  { return []string{".localized"} }
func (*localizedType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// thumbsDBType matches Windows thumbnail caches. Thumbs.db is the
// classic XP/Vista cache; ehthumbs.db / ehthumbs_vista.db are
// Explorer-helper variants. All three are OLE compound documents;
// parser out of scope.
type thumbsDBType struct{}

func (*thumbsDBType) Name() string         { return "system/windows-thumbs-db" }
func (*thumbsDBType) Extensions() []string { return nil }
func (*thumbsDBType) MagicBytes() [][]byte { return nil }
func (*thumbsDBType) Filenames() []string {
	return []string{"Thumbs.db", "ehthumbs.db", "ehthumbs_vista.db"}
}
func (*thumbsDBType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// desktopIniType matches Windows folder-customisation files. Plain
// INI; IconResource / [.ShellClassInfo] parsing left for follow-up.
type desktopIniType struct{}

func (*desktopIniType) Name() string         { return "system/windows-desktop-ini" }
func (*desktopIniType) Extensions() []string { return nil }
func (*desktopIniType) MagicBytes() [][]byte { return nil }
func (*desktopIniType) Filenames() []string  { return []string{"Desktop.ini"} }
func (*desktopIniType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// kdeDirectoryType matches KDE Dolphin's per-folder properties file.
// Plain INI; parsing left for follow-up. Per-type predicate is named
// is_kde_directory rather than is_directory to avoid the misleading
// "is this a directory" reading.
type kdeDirectoryType struct{}

func (*kdeDirectoryType) Name() string         { return "system/linux-directory" }
func (*kdeDirectoryType) Extensions() []string { return nil }
func (*kdeDirectoryType) MagicBytes() [][]byte { return nil }
func (*kdeDirectoryType) Filenames() []string  { return []string{".directory"} }
func (*kdeDirectoryType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
