package content

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"strings"
)

// FilenameMatcher is an optional interface a ContentType can implement
// to advertise exact basenames it matches — for files like Dockerfile,
// Makefile, LICENSE, .gitignore, go.mod that have no useful extension.
// Detection by exact name takes precedence over extension matching, so
// e.g. package.json detects as manifest/node (more specific) rather
// than generic json. Comparison is case-insensitive, matching the
// existing extension-detection convention.
type FilenameMatcher interface {
	Filenames() []string
}

// Detect detects the content type of a file using extension first, then
// magic bytes. Path is an fs.FS-style key (forward slashes); fsys is the
// filesystem that provides Open access for magic-byte sniffing when no
// extension matches. fsys may be nil when the caller knows extension
// matching alone is sufficient.
func (r *Registry) Detect(fsys fs.FS, p string) ContentType {
	r.mu.RLock()
	types := make([]ContentType, len(r.types))
	copy(types, r.types)
	r.mu.RUnlock()

	// Exact-basename pass first. Lets types like build/dockerfile,
	// repo/license, manifest/gomod identify files that either have no
	// extension at all (LICENSE, .gitignore, Dockerfile) or whose
	// extension would otherwise dispatch to a less-specific parser
	// (package.json → manifest/node, not json).
	base := path.Base(p)
	for _, ct := range types {
		fm, ok := ct.(FilenameMatcher)
		if !ok {
			continue
		}
		for _, name := range fm.Filenames() {
			if strings.EqualFold(base, name) {
				return ct
			}
		}
	}

	// Two-pass extension match: prefer multi-component suffixes (e.g.
	// ".tar.gz") over single-component fallbacks (".gz") so registered
	// types like archive/tar+gzip win against archive/gzip when both
	// match. The longest registered extension that case-insensitively
	// suffix-matches the path wins.
	pLower := strings.ToLower(p)
	var best ContentType
	bestLen := 0
	for _, ct := range types {
		for _, e := range ct.Extensions() {
			if !strings.HasSuffix(pLower, e) {
				continue
			}
			if len(e) > bestLen {
				best = ct
				bestLen = len(e)
			}
		}
	}
	if best != nil {
		return best
	}
	if fsys == nil {
		return nil
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}
	buf = buf[:n]
	for _, ct := range types {
		for _, magic := range ct.MagicBytes() {
			if bytes.HasPrefix(buf, magic) {
				return ct
			}
		}
	}
	return nil
}
