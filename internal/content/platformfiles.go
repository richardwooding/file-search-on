package content

import (
	"context"
	"io/fs"
)

func init() {
	Register(&procfileType{})
	Register(&vagrantfileType{})
}

// procfileType matches Heroku-style Procfile process declarations.
// Used by Foreman, Honcho, and most PaaS providers that accept
// Procfile-formatted release manifests.
type procfileType struct{}

func (*procfileType) Name() string         { return "platform/procfile" }
func (*procfileType) Extensions() []string { return nil }
func (*procfileType) MagicBytes() [][]byte { return nil }
func (*procfileType) Filenames() []string  { return []string{"Procfile"} }
func (*procfileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// vagrantfileType matches Vagrant's VM dev-environment manifest.
type vagrantfileType struct{}

func (*vagrantfileType) Name() string         { return "platform/vagrant" }
func (*vagrantfileType) Extensions() []string { return nil }
func (*vagrantfileType) MagicBytes() [][]byte { return nil }
func (*vagrantfileType) Filenames() []string  { return []string{"Vagrantfile"} }
func (*vagrantfileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
