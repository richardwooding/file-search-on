package content

import (
	"bytes"
	"context"
	"testing"
	"testing/fstest"

	"howett.net/plist"
)

// encodePlist marshals the dict in the chosen format and returns the
// bytes. Used by every test to synthesise fixtures without needing
// real on-disk Apple files.
func encodePlist(t *testing.T, root any, format int) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := plist.NewEncoderForFormat(&buf, format)
	if err := enc.Encode(root); err != nil {
		t.Fatalf("plist encode: %v", err)
	}
	return buf.Bytes()
}

func TestPlistType_DetectionBinaryMagic(t *testing.T) {
	data := encodePlist(t, map[string]any{"hello": "world"}, plist.BinaryFormat)
	fsys := fstest.MapFS{"random.bin": {Data: data}}

	ct := DefaultRegistry().Detect(fsys, "random.bin")
	if ct == nil {
		t.Fatal("Detect returned nil for binary plist with no .plist extension")
	}
	if ct.Name() != "system/plist" {
		t.Errorf("Detect.Name() = %s, want system/plist", ct.Name())
	}
}

func TestPlistType_DetectionByExtension(t *testing.T) {
	data := encodePlist(t, map[string]any{"k": "v"}, plist.XMLFormat)
	fsys := fstest.MapFS{"prefs.plist": {Data: data}}

	ct := DefaultRegistry().Detect(fsys, "prefs.plist")
	if ct == nil {
		t.Fatal("Detect returned nil for XML plist")
	}
	if ct.Name() != "system/plist" {
		t.Errorf("Detect.Name() = %s, want system/plist", ct.Name())
	}
}

func TestParsePlist_InfoPlist(t *testing.T) {
	root := map[string]any{
		"CFBundleIdentifier":         "com.example.app",
		"CFBundleName":               "Example",
		"CFBundleDisplayName":        "Example App",
		"CFBundleVersion":            "1234.5",
		"CFBundleShortVersionString": "1.2.3",
		"CFBundleExecutable":         "Example",
		"LSMinimumSystemVersion":     "14.0",
	}
	data := encodePlist(t, root, plist.BinaryFormat)
	path := "/Applications/Example.app/Contents/Info.plist"

	attrs := parsePlist(data, path)

	checks := map[string]any{
		"plist_format":               "binary",
		"plist_root_kind":            "dict",
		"plist_kind":                 "info",
		"plist_bundle_identifier":    "com.example.app",
		"plist_bundle_name":          "Example App", // display name wins
		"plist_bundle_version":       "1234.5",
		"plist_bundle_short_version": "1.2.3",
		"plist_executable":           "Example",
		"plist_min_os_version":       "14.0",
	}
	for k, want := range checks {
		if got := attrs[k]; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
}

func TestParsePlist_DisplayNameFallsBackToBundleName(t *testing.T) {
	root := map[string]any{
		"CFBundleIdentifier": "com.example.foo",
		"CFBundleName":       "Foo",
		// no CFBundleDisplayName
	}
	data := encodePlist(t, root, plist.XMLFormat)
	attrs := parsePlist(data, "/Applications/Foo.app/Contents/Info.plist")

	if got := attrs["plist_bundle_name"]; got != "Foo" {
		t.Errorf("plist_bundle_name = %v, want Foo (fallback to CFBundleName)", got)
	}
}

func TestParsePlist_LaunchAgent(t *testing.T) {
	root := map[string]any{
		"Label": "com.anthropic.claudefordesktop",
		"ProgramArguments": []any{
			"/Applications/Claude.app/Contents/MacOS/claude-helper",
			"--background",
		},
		"RunAtLoad": true,
		"KeepAlive": true,
	}
	data := encodePlist(t, root, plist.BinaryFormat)
	// Bare basename — the path-based kind override happens at the
	// celexpr layer with displayPath; parsePlist's input here mirrors
	// what ContentType.Attributes sees during a narrow `-d LaunchAgents`
	// walk (no directory prefix in the path).
	path := "com.anthropic.claudefordesktop.plist"

	attrs := parsePlist(data, path)

	// Content-only extraction surfaces the LaunchAgent payload but
	// does NOT classify it as `launch-agent` — that's path-anchored
	// and done at the celexpr layer.
	if got := attrs["plist_label"]; got != "com.anthropic.claudefordesktop" {
		t.Errorf("plist_label = %v", got)
	}
	if got := attrs["plist_program"]; got != "/Applications/Claude.app/Contents/MacOS/claude-helper" {
		t.Errorf("plist_program = %v", got)
	}
	args, ok := attrs["plist_program_arguments"].([]string)
	if !ok || len(args) != 2 {
		t.Fatalf("plist_program_arguments = %v (%T)", attrs["plist_program_arguments"], attrs["plist_program_arguments"])
	}
	if args[1] != "--background" {
		t.Errorf("args[1] = %v, want --background", args[1])
	}
	if got := attrs["plist_run_at_load"]; got != true {
		t.Errorf("plist_run_at_load = %v, want true", got)
	}
	if got := attrs["plist_keep_alive"]; got != true {
		t.Errorf("plist_keep_alive = %v, want true", got)
	}
}

func TestParsePlist_LaunchDaemonContent(t *testing.T) {
	root := map[string]any{
		"Label":   "com.example.daemon",
		"Program": "/usr/local/bin/example-daemon",
	}
	data := encodePlist(t, root, plist.BinaryFormat)
	// Bare basename — same rationale as the LaunchAgent test above.
	path := "com.example.daemon.plist"

	attrs := parsePlist(data, path)

	// Program key wins over ProgramArguments[0] — explicit Program is
	// canonical per launchd.plist(5).
	if got := attrs["plist_program"]; got != "/usr/local/bin/example-daemon" {
		t.Errorf("plist_program = %v", got)
	}
}

func TestLookupPlistKindFromPath(t *testing.T) {
	tests := []struct {
		displayPath string
		want        string
	}{
		{"/Users/alice/Library/LaunchAgents/com.example.agent.plist", "launch-agent"},
		{"/Library/LaunchDaemons/com.example.daemon.plist", "launch-daemon"},
		{"/Users/alice/Library/Preferences/com.apple.Safari.plist", "preferences"},
		{"/Applications/Foo.app/Contents/Info.plist", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := LookupPlistKindFromPath(tc.displayPath)
		if got != tc.want {
			t.Errorf("LookupPlistKindFromPath(%q) = %q, want %q",
				tc.displayPath, got, tc.want)
		}
	}
}

func TestParsePlist_Webloc(t *testing.T) {
	root := map[string]any{
		"URL": "https://example.com",
	}
	data := encodePlist(t, root, plist.XMLFormat)
	path := "/Users/x/Desktop/saved-page.webloc"

	attrs := parsePlist(data, path)

	if got := attrs["plist_kind"]; got != "webloc" {
		t.Errorf("plist_kind = %v, want webloc", got)
	}
}

func TestParsePlist_ContentBasedInfoFallback(t *testing.T) {
	// A plist with CFBundleIdentifier but no path signal — should
	// still classify as info via the content fallback.
	root := map[string]any{
		"CFBundleIdentifier": "com.example.framework",
	}
	data := encodePlist(t, root, plist.BinaryFormat)
	attrs := parsePlist(data, "/some/random/path.plist")

	if got := attrs["plist_kind"]; got != "info" {
		t.Errorf("plist_kind = %v, want info (content-based)", got)
	}
}

func TestParsePlist_MalformedReturnsEmptyAttrs(t *testing.T) {
	junk := []byte("bplist00" + "not really a binary plist")
	attrs := parsePlist(junk, "/some/path.plist")
	// plist_format will still be set; other attrs may or may not be
	// present. The contract is: no panic.
	if got := attrs["plist_format"]; got != "binary" {
		t.Errorf("plist_format = %v, want binary (magic still detectable)", got)
	}
}

func TestParsePlist_EmptyInputReturnsEmptyAttrs(t *testing.T) {
	attrs := parsePlist(nil, "/some/path.plist")
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for nil input, got %v", attrs)
	}
}

func TestPlistType_AttributesViaRegistry(t *testing.T) {
	root := map[string]any{
		"CFBundleIdentifier":         "com.test.app",
		"CFBundleShortVersionString": "2.0",
	}
	data := encodePlist(t, root, plist.BinaryFormat)
	fsys := fstest.MapFS{
		"fake.app/Contents/Info.plist": {Data: data},
	}

	ct := DefaultRegistry().Detect(fsys, "fake.app/Contents/Info.plist")
	if ct == nil {
		t.Fatal("registry.Detect returned nil")
	}
	attrs, err := ct.Attributes(context.Background(), fsys, "fake.app/Contents/Info.plist")
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["plist_bundle_identifier"]; got != "com.test.app" {
		t.Errorf("plist_bundle_identifier = %v", got)
	}
	if got := attrs["plist_kind"]; got != "info" {
		t.Errorf("plist_kind = %v, want info", got)
	}
}

func TestPlistFormatOf(t *testing.T) {
	bin := []byte("bplist00\x00\x00\x00")
	xml := []byte(`<?xml version="1.0"?><plist><dict/></plist>`)
	if got := plistFormatOf(bin); got != "binary" {
		t.Errorf("plistFormatOf(bplist) = %v, want binary", got)
	}
	if got := plistFormatOf(xml); got != "xml" {
		t.Errorf("plistFormatOf(xml) = %v, want xml", got)
	}
}
