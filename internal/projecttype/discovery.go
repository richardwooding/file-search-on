package projecttype

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ConfigFileName is the basename project-type YAML config files use
// in both the user-wide and per-project search locations. Wrapped in
// a `.file-search-on/` directory rather than living as a dotfile at
// the top level so future companion configs (cache prefs, default
// flags, …) can sit alongside without further filename inflation.
const ConfigFileName = "project-types.yaml"

// ConfigDirName is the per-platform user-config-dir subdirectory
// project-type configs live under. Joined with os.UserConfigDir() →
// $XDG_CONFIG_HOME/file-search-on / $HOME/Library/Application
// Support/file-search-on / %APPDATA%\file-search-on depending on
// platform.
const ConfigDirName = "file-search-on"

// PerProjectDirName is the directory project-type configs live in
// when colocated with a checkout. Walked from the current working
// directory only — git-style ancestor walk-up is a follow-up.
const PerProjectDirName = ".file-search-on"

// DiscoveryPaths returns the project-type config paths to search,
// in load order (later layers register on top of earlier ones).
//
//  1. User-wide: os.UserConfigDir() / file-search-on / project-types.yaml
//  2. Per-project (CWD only): ./.file-search-on/project-types.yaml
//
// Both are optional. Missing files are skipped silently by
// LoadDiscovered. Paths whose anchor can't be resolved
// (UserConfigDir unset, Getwd fails) are omitted entirely so the
// caller never sees an empty / suspicious entry.
func DiscoveryPaths() []string {
	var paths []string
	if cfgDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(cfgDir, ConfigDirName, ConfigFileName))
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, PerProjectDirName, ConfigFileName))
	}
	return paths
}

// LoadDiscovered loads every project-type config found via
// DiscoveryPaths into this registry, in precedence order. Missing
// files (most common case: the user has no custom types) are NOT
// errors. Returns the total number of types registered + the first
// real error encountered.
//
// Real errors (YAML parse failure, bad CEL, missing indicators) are
// fatal — they indicate a misconfiguration the user should fix
// before any subcommand runs.
func (r *Registry) LoadDiscovered() (int, error) {
	total := 0
	for _, p := range DiscoveryPaths() {
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			// Any other stat error (permission, etc.) — surface so
			// the user knows their config might be inaccessible.
			return total, fmt.Errorf("project-type config %s: %w", p, err)
		}
		n, err := r.LoadFromFile(p)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// LoadDiscovered is the package-level shortcut targeting
// DefaultRegistry(). The CLI's main() calls this before kong
// dispatches the subcommand (when --no-config-search isn't set).
func LoadDiscovered() (int, error) {
	return defaultRegistry.LoadDiscovered()
}
