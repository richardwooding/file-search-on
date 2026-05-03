package content

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Detect detects the content type of a file using extension first, then magic bytes
func (r *Registry) Detect(path string) ContentType {
	r.mu.RLock()
	types := make([]ContentType, len(r.types))
	copy(types, r.types)
	r.mu.RUnlock()

	ext := strings.ToLower(filepath.Ext(path))
	for _, ct := range types {
		if slices.Contains(ct.Extensions(), ext) {
			return ct
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
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
