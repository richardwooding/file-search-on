package content

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

// Apple property list — both the binary (bplist00) and XML variants
// land here. Detection is magic-based for binary (the 8-byte
// `bplist00` prefix at offset 0 is unambiguous) and extension-based
// for XML (the generic `<?xml` prefix would over-fire if registered
// as magic). The same parser handles both; howett.net/plist sniffs
// the encoding on Decode.
//
// Surfaces bundle identity (CFBundleIdentifier / CFBundleVersion /
// CFBundleShortVersionString / CFBundleName / CFBundleExecutable),
// LaunchAgent / LaunchDaemon persistence keys (Label / Program /
// ProgramArguments / RunAtLoad / KeepAlive), and an aggregate
// `plist_kind` label from a small path-based registry.
//
// Read-only by design — howett.net/plist's Encode side stays unused.

var plistBinaryMagic = []byte("bplist00")

const (
	// plistMagicLen is the length of `bplist00` — the binary v1.x
	// prefix. Older v0 binary plists used a different prefix that
	// is effectively non-existent in the wild on modern macOS.
	plistMagicLen = 8

	// plistReadCap bounds disk reads. 1 MiB covers virtually every
	// real plist (Info.plist files are typically a few KB; preferences
	// rarely exceed 100 KB; the largest plists in the wild are app
	// state caches which still fit in 1 MiB). Larger plists surface
	// only their top-level metadata or, on extreme size, an empty
	// attribute set — degrades gracefully without OOMing the walker.
	plistReadCap = 1 << 20
)

func init() {
	Register(&plistType{})
}

type plistType struct{}

func (*plistType) Name() string         { return "system/plist" }
func (*plistType) Extensions() []string { return []string{".plist"} }
func (*plistType) MagicBytes() [][]byte { return [][]byte{plistBinaryMagic} }

func (*plistType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf, err := io.ReadAll(io.LimitReader(f, plistReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	return parsePlist(buf, path), nil
}

// parsePlist decodes a property-list payload and surfaces the
// CEL-visible attributes. Pure function so the fuzz target exercises
// it directly. Malformed input returns an empty attribute map per the
// walker contract — a broken plist doesn't fail the walk.
func parsePlist(data []byte, path string) Attributes {
	if len(data) == 0 {
		return Attributes{}
	}
	out := Attributes{
		"plist_format": plistFormatOf(data),
	}

	var root any
	if _, err := plist.Unmarshal(data, &root); err != nil {
		// XML / binary corrupt — still surface the format hint plus
		// any path-based kind classification (LaunchAgents directory
		// presence is meaningful even when the file body is broken).
		if kind := plistKindFromPath(path, nil); kind != "" {
			out["plist_kind"] = kind
		}
		return out
	}
	out["plist_root_kind"] = plistRootKindOf(root)

	dict, ok := root.(map[string]any)
	if !ok {
		// Non-dict root (rare — happens for .webloc which is a dict,
		// but plist arrays exist e.g. in MobileMeAccounts). Path
		// registry still applies.
		if kind := plistKindFromPath(path, nil); kind != "" {
			out["plist_kind"] = kind
		}
		return out
	}

	if v := plistString(dict, "CFBundleIdentifier"); v != "" {
		out["plist_bundle_identifier"] = v
	}
	// CFBundleDisplayName beats CFBundleName when both present —
	// matches what Finder shows.
	if v := plistString(dict, "CFBundleDisplayName"); v != "" {
		out["plist_bundle_name"] = v
	} else if v := plistString(dict, "CFBundleName"); v != "" {
		out["plist_bundle_name"] = v
	}
	if v := plistString(dict, "CFBundleVersion"); v != "" {
		out["plist_bundle_version"] = v
	}
	if v := plistString(dict, "CFBundleShortVersionString"); v != "" {
		out["plist_bundle_short_version"] = v
	}
	if v := plistString(dict, "CFBundleExecutable"); v != "" {
		out["plist_executable"] = v
	}
	if v := plistString(dict, "LSMinimumSystemVersion"); v != "" {
		out["plist_min_os_version"] = v
	}

	// LaunchAgent / LaunchDaemon keys.
	if v := plistString(dict, "Label"); v != "" {
		out["plist_label"] = v
	}
	args := plistStringSlice(dict, "ProgramArguments")
	if len(args) > 0 {
		out["plist_program_arguments"] = args
	}
	// `Program` takes precedence over `ProgramArguments[0]` per
	// launchd.plist(5) — explicit Program is the canonical executable.
	if v := plistString(dict, "Program"); v != "" {
		out["plist_program"] = v
	} else if len(args) > 0 {
		out["plist_program"] = args[0]
	}
	if v, ok := plistBool(dict, "RunAtLoad"); ok {
		out["plist_run_at_load"] = v
	}
	if v, ok := plistBool(dict, "KeepAlive"); ok {
		out["plist_keep_alive"] = v
	}

	out["plist_kind"] = plistKindFromPath(path, dict)
	return out
}

// plistFormatOf reports `"binary"` or `"xml"` based on the magic
// prefix. The howett.net/plist decoder accepts both transparently;
// this is just for the surface attribute.
func plistFormatOf(data []byte) string {
	if len(data) >= plistMagicLen && bytes.HasPrefix(data, plistBinaryMagic) {
		return "binary"
	}
	// XML plists begin with `<?xml` (most common) or, in some macOS
	// versions, jump straight to `<!DOCTYPE plist`. Either way the
	// content is text — that's enough for the attribute.
	return "xml"
}

// plistRootKindOf classifies the parsed root object by Go type. Maps
// to the plist type names from the Property List Programming Guide:
// dictionary / array / string / integer / real / boolean / date /
// data. Empty when we can't classify (shouldn't happen for a well-
// formed plist).
func plistRootKindOf(root any) string {
	switch root.(type) {
	case map[string]any:
		return "dict"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "bool"
	case []byte:
		return "data"
	case int64, uint64, int, uint:
		return "integer"
	case float32, float64:
		return "real"
	}
	// Plist time.Time and other shapes fall through; not worth a
	// per-type case until we surface them as attributes.
	return ""
}

// plistKindFromPath labels the plist's role via basename + content
// checks only. Path-based dimensions (LaunchAgents/, LaunchDaemons/,
// Preferences/) cannot fire here because ContentType.Attributes is
// handed an fs.FS-relative `fsPath` that loses everything above the
// search root — when an agent runs `-d ~/Library/LaunchAgents`, the
// path is just `com.example.agent.plist`. Path-based dimensions are
// handled at the celexpr layer via LookupPlistKindFromPath where the
// absolute displayPath is available — same architecture as #177's
// SQLite app-name registry.
func plistKindFromPath(path string, dict map[string]any) string {
	base := filepath.Base(path)

	// Info.plist sits inside a `<App>.app/Contents/` bundle. Filename
	// alone is enough — visible regardless of search root.
	if base == "Info.plist" {
		return "info"
	}

	// .webloc files are Safari saved-page links — they store a single
	// `URL` key. Content check disambiguates from the (very rare)
	// non-webloc plist named with a .webloc extension.
	if strings.HasSuffix(strings.ToLower(base), ".webloc") {
		if dict != nil {
			if _, ok := dict["URL"]; ok {
				return "webloc"
			}
		}
	}

	// Content-only classification: a dict carrying CFBundleIdentifier
	// is conventionally an Info.plist-shaped file (e.g. Info.plist
	// inside a kext, or a generated version manifest).
	if dict != nil {
		if _, ok := dict["CFBundleIdentifier"]; ok {
			return "info"
		}
	}
	return ""
}

// LookupPlistKindFromPath is the celexpr-layer hook for path-based
// plist_kind classification. Takes the absolute display path; returns
// the matched kind or "" when no path dimension fires. Called from
// BuildAttributesWith after parsePlist sets the content-based kind,
// overriding when a more-specific path signal applies.
//
// Precedence (path beats content): LaunchAgents > LaunchDaemons >
// Preferences. A plist physically located under LaunchAgents/ IS a
// LaunchAgent by definition regardless of what its content says — the
// directory placement is what makes launchd load it.
func LookupPlistKindFromPath(displayPath string) string {
	switch {
	case strings.Contains(displayPath, "/LaunchAgents/"):
		return "launch-agent"
	case strings.Contains(displayPath, "/LaunchDaemons/"):
		return "launch-daemon"
	case strings.Contains(displayPath, "/Preferences/"):
		return "preferences"
	}
	return ""
}

// plistString returns dict[key] as a string, or "" when missing /
// wrong type. Type-assertion-only — no conversion across plist types
// because callers expect string values to be string-shaped at the
// source (CFBundleVersion is a string per the spec; an integer there
// would be a malformed plist).
func plistString(dict map[string]any, key string) string {
	if v, ok := dict[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// plistBool returns (value, true) when dict[key] is a bool, or
// (false, false) when missing. Distinguished from "absent" because
// the absent-vs-false distinction matters for the LaunchAgent
// persistence indicators — agents reading the JSON wire shape want
// `omitempty` to drop the field when not set rather than emit a
// confusing `false`.
func plistBool(dict map[string]any, key string) (bool, bool) {
	if v, ok := dict[key]; ok {
		if b, ok := v.(bool); ok {
			return b, true
		}
	}
	return false, false
}

// plistStringSlice returns dict[key] as a []string when the value is
// a plist array of strings. Non-string elements are silently dropped
// (rare for ProgramArguments, which is spec'd as an array of strings).
func plistStringSlice(dict map[string]any, key string) []string {
	v, ok := dict[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
