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

// DiscoveryEntry annotates a single discovery path with its scope
// ("user-wide" or "per-project") so consumers can label / order
// without re-deriving the meaning from the path itself.
type DiscoveryEntry struct {
	Scope string // "user-wide" | "per-project"
	Path  string
}

// DiscoveryEntries returns the project-type config search locations
// in load order (later layers register on top of earlier), each
// annotated with its scope. Paths whose anchor can't be resolved
// (UserConfigDir unset, Getwd fails) are omitted so the caller
// never sees an empty / suspicious entry.
func DiscoveryEntries() []DiscoveryEntry {
	var out []DiscoveryEntry
	if cfgDir, err := os.UserConfigDir(); err == nil {
		out = append(out, DiscoveryEntry{
			Scope: "user-wide",
			Path:  filepath.Join(cfgDir, ConfigDirName, ConfigFileName),
		})
	}
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, DiscoveryEntry{
			Scope: "per-project",
			Path:  filepath.Join(cwd, PerProjectDirName, ConfigFileName),
		})
	}
	return out
}

// DiscoveryPaths is the legacy plain-paths shortcut over
// DiscoveryEntries — kept because LoadDiscovered (and external
// callers) only need the path strings. New code that wants scope
// labels should call DiscoveryEntries directly.
func DiscoveryPaths() []string {
	entries := DiscoveryEntries()
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
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
