package content

import (
	"context"
	"io/fs"
)

func init() {
	Register(&licenseType{})
	Register(&changelogType{})
	Register(&contributingType{})
	Register(&codeownersType{})
}

// licenseType matches the bare LICENSE / LICENCE / COPYING files that
// almost every repo has at its root. SPDX detection (content-based
// fuzzy match against known license texts) is a follow-up.
type licenseType struct{}

func (*licenseType) Name() string         { return "repo/license" }
func (*licenseType) Extensions() []string { return nil }
func (*licenseType) MagicBytes() [][]byte { return nil }
func (*licenseType) Filenames() []string  { return []string{"LICENSE", "LICENCE", "COPYING"} }
func (*licenseType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
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
