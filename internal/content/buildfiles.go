package content

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"strings"
)

func init() {
	Register(&dockerfileType{})
	Register(&makefileType{})
	Register(&justfileType{})
	Register(&rakefileType{})
}

// dockerfileType matches Dockerfile and Containerfile (Podman's
// equivalent). Extracts base_image from the first FROM directive — a
// reasonable signal for "is this a python image" / "is this alpine"
// queries. Multi-stage builds carry several FROM directives; only the
// first is surfaced here (deeper parsing is a follow-up).
type dockerfileType struct{}

func (*dockerfileType) Name() string         { return "build/dockerfile" }
func (*dockerfileType) Extensions() []string { return nil }
func (*dockerfileType) MagicBytes() [][]byte { return nil }
func (*dockerfileType) Filenames() []string  { return []string{"Dockerfile", "Containerfile"} }

func (*dockerfileType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readAll(fsys, path)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())
	var baseImage string
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Match "FROM <image>" or "FROM --platform=... <image>".
		// Case-insensitive per the Dockerfile spec.
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "FROM ") && !strings.HasPrefix(upper, "FROM\t") {
			continue
		}
		fields := strings.Fields(line)
		// Walk past "FROM" and any --flag arguments to find the image.
		for i := 1; i < len(fields); i++ {
			if strings.HasPrefix(fields[i], "--") {
				continue
			}
			baseImage = fields[i]
			break
		}
		break
	}
	if baseImage == "" {
		return Attributes{}, nil
	}
	return Attributes{"base_image": baseImage}, nil
}

// makefileType matches Make's canonical filename and its variants.
// Detection only — extracting target lists is left as a follow-up.
type makefileType struct{}

func (*makefileType) Name() string         { return "build/makefile" }
func (*makefileType) Extensions() []string { return nil }
func (*makefileType) MagicBytes() [][]byte { return nil }
func (*makefileType) Filenames() []string {
	return []string{"Makefile", "GNUmakefile", "BSDmakefile", "makefile"}
}
func (*makefileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// justfileType matches Just task runner files (casey/just).
type justfileType struct{}

func (*justfileType) Name() string         { return "build/justfile" }
func (*justfileType) Extensions() []string { return nil }
func (*justfileType) MagicBytes() [][]byte { return nil }
func (*justfileType) Filenames() []string  { return []string{"Justfile", "justfile"} }
func (*justfileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}

// rakefileType matches Ruby's Rake build scripts. Conceptually closer
// to Makefile than to a Ruby Gemfile manifest.
type rakefileType struct{}

func (*rakefileType) Name() string         { return "build/rakefile" }
func (*rakefileType) Extensions() []string { return nil }
func (*rakefileType) MagicBytes() [][]byte { return nil }
func (*rakefileType) Filenames() []string  { return []string{"Rakefile"} }
func (*rakefileType) Attributes(ctx context.Context, _ fs.FS, _ string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Attributes{}, nil
}
