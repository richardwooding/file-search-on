//go:build darwin

package content

import (
	"bytes"
	"sort"

	"golang.org/x/sys/unix"
)

// Read extended attributes on macOS via the BSD-style listxattr +
// getxattr syscalls. Issue #193.
//
// Pure-parsing logic for the structured xattr values (quarantine
// string, kMDItemWhereFroms plist, _kMDItemUserTags plist) lives in
// the cross-platform xattrs_parse.go file so it's reachable from
// Linux / Windows CI runners for fuzz + unit tests. This file holds
// only the Darwin-specific syscall layer.

// Quarantine-related xattr keys.
const (
	xattrQuarantine    = "com.apple.quarantine"
	xattrWhereFroms    = "com.apple.metadata:kMDItemWhereFroms"
	xattrFinderTags    = "com.apple.metadata:_kMDItemUserTags"
	xattrFinderComment = "com.apple.metadata:kMDItemFinderComment"
)

// xattrKeyCap bounds the surface keys list. Real files rarely exceed
// 10 xattr keys; 50 is generous and stops a pathological filesystem
// dump from blowing up the JSON wire shape.
const xattrKeyCap = 50

// ReadXattrs is the celexpr-layer entry point. Returns extended-
// attribute attributes for the given OS path; empty on non-Darwin
// platforms or any syscall failure. Called from BuildAttributesWith
// only when BuildOptions.ReadExtendedAttributes is set.
func ReadXattrs(osPath string) Attributes {
	return readXattrs(osPath)
}

// readXattrs is the Darwin-specific entry point. Returns an empty
// Attributes (not nil) on any syscall failure — xattrs aren't critical
// to the walk and a permission denied on one file shouldn't fail the
// rest.
func readXattrs(osPath string) Attributes {
	keys, err := listXattrs(osPath)
	if err != nil || len(keys) == 0 {
		return Attributes{}
	}
	out := Attributes{
		"xattr_count": int64(len(keys)),
	}
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	if len(sorted) > xattrKeyCap {
		sorted = sorted[:xattrKeyCap]
	}
	out["xattr_keys"] = sorted
	out["is_xattr_rich"] = true

	for _, k := range keys {
		switch k {
		case xattrQuarantine:
			if v, err := getXattr(osPath, k); err == nil {
				mergeQuarantineAttrs(out, v)
			}
		case xattrWhereFroms:
			if v, err := getXattr(osPath, k); err == nil {
				mergeWhereFromsAttrs(out, v)
			}
		case xattrFinderTags:
			if v, err := getXattr(osPath, k); err == nil {
				mergeFinderTagAttrs(out, v)
			}
		case xattrFinderComment:
			out["has_finder_comment"] = true
		}
	}
	return out
}

// listXattrs returns all extended attribute keys on the file at
// osPath. Returns nil on EPERM / ENOENT / unsupported-filesystem
// rather than propagating — xattr support is best-effort.
func listXattrs(osPath string) ([]string, error) {
	size, err := unix.Listxattr(osPath, nil)
	if err != nil || size <= 0 {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := unix.Listxattr(osPath, buf)
	if err != nil {
		return nil, err
	}
	return splitNullBytes(buf[:n]), nil
}

// getXattr returns the value of one named xattr at osPath.
func getXattr(osPath, name string) ([]byte, error) {
	size, err := unix.Getxattr(osPath, name, nil)
	if err != nil || size < 0 {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := unix.Getxattr(osPath, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// splitNullBytes splits a NUL-separated byte slice into strings.
// Trailing empty element from the final NUL is dropped.
func splitNullBytes(buf []byte) []string {
	parts := bytes.Split(buf, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		out = append(out, string(p))
	}
	return out
}
