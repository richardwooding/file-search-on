package content

import (
	"context"
	"io/fs"
)

func init() {
	Register(&gitignoreType{})
	Register(&dockerignoreType{})
}

// gitignoreType matches Git's ignore + attribute control files. Same
// gitignore syntax is used for both, so they share one content type
// (subtype distinction is encoded in the filename itself if a caller
// wants it).
type gitignoreType struct{}

func (*gitignoreType) Name() string         { return "ignore/git" }
func (*gitignoreType) Extensions() []string { return nil }
func (*gitignoreType) MagicBytes() [][]byte { return nil }
func (*gitignoreType) Filenames() []string  { return []string{".gitignore", ".gitattributes"} }
func (*gitignoreType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// dockerignoreType matches Docker's build-context exclude file. Same
// syntax family as .gitignore but a separate ecosystem.
type dockerignoreType struct{}

func (*dockerignoreType) Name() string         { return "ignore/docker" }
func (*dockerignoreType) Extensions() []string { return nil }
func (*dockerignoreType) MagicBytes() [][]byte { return nil }
func (*dockerignoreType) Filenames() []string  { return []string{".dockerignore"} }
func (*dockerignoreType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
