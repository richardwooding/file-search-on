package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// registrySubdir is the per-user cache location for monitor-instance
// registration files, mirroring the os.UserConfigDir()/file-search-on/…
// convention used by internal/projecttype for config discovery.
const registrySubdir = "file-search-on/monitors"

// Entry is one running dashboard instance's registration record, written
// as <pid>.json in the registry directory. Concurrent file-search-on
// processes read each other's entries to build the peer switcher.
type Entry struct {
	PID        int       `json:"pid"`
	URL        string    `json:"url"`  // http://127.0.0.1:<port>/
	Mode       string    `json:"mode"` // mcp-stdio | mcp-http | mcp-sse | watch
	Version    string    `json:"version"`
	StartedAt  time.Time `json:"started_at"`
	WorkingDir string    `json:"working_dir"`
	IndexPath  string    `json:"index_path,omitempty"` // "" = in-memory
	// IsSelf is set only on the /api/peers response for the entry that
	// matches the serving instance; it is never persisted (the registry
	// files omit it via the zero value).
	IsSelf bool `json:"is_self,omitempty"`
}

// registryDir returns the monitor registry directory, creating it if
// needed. Falls back to os.TempDir() when the user cache dir is
// unavailable so registration degrades rather than fails.
func registryDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, registrySubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Register writes e as <pid>.json in the registry directory and returns
// a deregister func that removes it. Registration failures are returned
// so the caller can log-and-continue; a missing registry only disables
// peer discovery, not the dashboard itself.
func Register(e Entry) (func(), error) {
	dir, err := registryDir()
	if err != nil {
		return func() {}, err
	}
	path := filepath.Join(dir, entryFile(e.PID))
	data, err := json.Marshal(e)
	if err != nil {
		return func() {}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return func() {}, err
	}
	return func() { _ = os.Remove(path) }, nil
}

// Peers returns every live registered instance, sorted by StartedAt.
// Entries whose process is no longer alive are pruned from disk as a
// side effect (best-effort), so a crashed instance self-heals out of
// every peer's switcher on the next read.
func Peers() []Entry {
	dir, err := registryDir()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil
	}
	var out []Entry
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			// Corrupt / partially-written file — drop it.
			_ = os.Remove(path)
			continue
		}
		if e.PID <= 0 || !processAlive(e.PID) {
			_ = os.Remove(path)
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// entryFile is the registration filename for a PID.
func entryFile(pid int) string {
	return strconv.Itoa(pid) + ".json"
}
