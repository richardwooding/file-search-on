//go:build !darwin

package content

// readXattrs is the non-Darwin stub. Linux + Windows both have their
// own extended-attribute schemes (Linux xattrs via setfattr/getfattr,
// Windows alternate data streams) but neither is the agentic-triage
// surface that macOS xattrs are — end-user files on Linux / Windows
// rarely carry quarantine / tag metadata that maps cleanly onto our
// surface. We return empty attrs unconditionally so the celexpr hook
// stays a single-line call that's free on these platforms.
//
// Issue #193 deliberately scopes v1 to Darwin. Linux xattr support
// (security.capability, security.selinux, user.* arbitrary keys) is
// a sensible follow-up but each xattr family needs its own decoder
// to be useful as a CEL attribute.
// ReadXattrs is the celexpr-layer entry point. Empty on non-Darwin.
func ReadXattrs(osPath string) Attributes {
	return readXattrs(osPath)
}

func readXattrs(osPath string) Attributes {
	return Attributes{}
}
