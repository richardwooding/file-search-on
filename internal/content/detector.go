package content

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
)

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

	ext := strings.ToLower(path.Ext(p))
	for _, ct := range types {
		if slices.Contains(ct.Extensions(), ext) {
			return ct
		}
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
