package content

import (
	"context"
	"io"
	"io/fs"
	"strings"
)

func init() {
	Register(&licenseType{})
	Register(&changelogType{})
	Register(&contributingType{})
	Register(&codeownersType{})
}

// licenseType matches the bare LICENSE / LICENCE / COPYING / UNLICENSE
// files that almost every repo has at its root. Attributes reads the
// body and scans for SPDX-license marker phrases (Apache "Apache
// License, Version 2.0", MIT "Permission is hereby granted, free of
// charge", GPL "GNU GENERAL PUBLIC LICENSE" + version, etc.) and
// surfaces the matched id as `license_id`.
type licenseType struct{}

func (*licenseType) Name() string         { return "repo/license" }
func (*licenseType) Extensions() []string { return nil }
func (*licenseType) MagicBytes() [][]byte { return nil }
func (*licenseType) Filenames() []string {
	return []string{"LICENSE", "LICENCE", "COPYING", "UNLICENSE"}
}

// licenseReadCap is the byte cap on the LICENSE body read. License
// boilerplates fit easily under 16 KiB (the longest is GPL-3.0 at
// ~35 KiB but the marker phrases are all in the first kilobyte).
const licenseReadCap = 16 * 1024

func (*licenseType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return Attributes{}, nil //nolint:nilerr // unreadable file → no license id, not error
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, licenseReadCap))
	if err != nil {
		return Attributes{}, nil //nolint:nilerr
	}
	id := detectLicenseID(string(b))
	if id == "" {
		return Attributes{}, nil
	}
	return Attributes{"license_id": id}, nil
}

// detectLicenseID scans body text for SPDX license markers. Returns
// the canonical SPDX identifier (e.g. "MIT", "Apache-2.0",
// "BSD-3-Clause") or "" if no recognised license fires.
//
// The marker phrases are unique-enough strings from each license's
// boilerplate. Where multiple licenses share boilerplate (BSD-2 vs
// BSD-3 differ only in the "Neither the name" clause), the order of
// checks distinguishes the variants.
//
// Recognised: MIT, Apache-2.0, BSD-3-Clause, BSD-2-Clause, ISC,
// GPL-3.0, GPL-2.0, LGPL-3.0, LGPL-2.1, AGPL-3.0, MPL-2.0, Unlicense,
// CC0-1.0, BSL-1.0. The list covers > 95% of real-world OSS LICENSE
// files (per ClearlyDefined / SPDX prevalence data).
func detectLicenseID(body string) string {
	// Case-insensitive scan via lowercase'd body. Marker phrases are
	// short enough that the lowercase pass is cheap. Skip the
	// allocation when the body is already empty.
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)

	// Order matters where licenses share boilerplate.
	switch {
	case strings.Contains(lower, "apache license") && strings.Contains(lower, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(lower, "gnu affero general public license"):
		return "AGPL-3.0"
	case strings.Contains(lower, "gnu lesser general public license") && strings.Contains(lower, "version 3"):
		return "LGPL-3.0"
	case strings.Contains(lower, "gnu lesser general public license") && (strings.Contains(lower, "version 2.1") || strings.Contains(lower, "version 2,")):
		return "LGPL-2.1"
	case strings.Contains(lower, "gnu general public license") && strings.Contains(lower, "version 3"):
		return "GPL-3.0"
	case strings.Contains(lower, "gnu general public license") && (strings.Contains(lower, "version 2") || strings.Contains(lower, "v2")):
		return "GPL-2.0"
	case strings.Contains(lower, "mozilla public license") && strings.Contains(lower, "2.0"):
		return "MPL-2.0"
	case strings.Contains(lower, "boost software license"):
		return "BSL-1.0"
	case strings.Contains(lower, "creative commons") && strings.Contains(lower, "cc0"):
		return "CC0-1.0"
	case strings.Contains(lower, "this is free and unencumbered software released into the public domain") ||
		strings.Contains(lower, "the unlicense"):
		return "Unlicense"
	case strings.Contains(lower, "permission to use, copy, modify, and/or distribute this software for any purpose"):
		// ISC's distinctive opening — distinct from MIT's "Permission
		// is hereby granted, free of charge".
		return "ISC"
	case strings.Contains(lower, "redistribution and use in source and binary forms"):
		// BSD family — disambiguate by counting clauses. BSD-3-Clause
		// has the "Neither the name" clause; BSD-2-Clause doesn't.
		if strings.Contains(lower, "neither the name") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	case strings.Contains(lower, "permission is hereby granted, free of charge"):
		// MIT's distinctive opening. Many MIT-adjacent permissive
		// licenses use the same phrase; the SPDX-canonical id is MIT.
		return "MIT"
	}
	return ""
}

// changelogType matches bare CHANGELOG. The CHANGELOG.md variant is
// caught by the markdown content type via extension — and the
// exact-name pass runs first, so this only fires for the extensionless
// case.
type changelogType struct{}

func (*changelogType) Name() string         { return "repo/changelog" }
func (*changelogType) Extensions() []string { return nil }
func (*changelogType) MagicBytes() [][]byte { return nil }
func (*changelogType) Filenames() []string  { return []string{"CHANGELOG", "HISTORY"} }
func (*changelogType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// contributingType matches bare CONTRIBUTING.
type contributingType struct{}

func (*contributingType) Name() string         { return "repo/contributing" }
func (*contributingType) Extensions() []string { return nil }
func (*contributingType) MagicBytes() [][]byte { return nil }
func (*contributingType) Filenames() []string  { return []string{"CONTRIBUTING"} }
func (*contributingType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// codeownersType matches GitHub/GitLab CODEOWNERS and OWNERS files.
// Lives under repo/ because it's repo-metadata rather than build /
// manifest / platform config.
type codeownersType struct{}

func (*codeownersType) Name() string         { return "repo/codeowners" }
func (*codeownersType) Extensions() []string { return nil }
func (*codeownersType) MagicBytes() [][]byte { return nil }
func (*codeownersType) Filenames() []string  { return []string{"CODEOWNERS", "OWNERS"} }
func (*codeownersType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
