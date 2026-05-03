package content

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Detect detects the content type of a file using extension first, then magic bytes
func (r *Registry) Detect(path string) ContentType {
	ext := strings.ToLower(filepath.Ext(path))
	for _, ct := range r.types {
		for _, e := range ct.Extensions() {
			if e == ext {
				return ct
			}
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	buf = buf[:n]
	for _, ct := range r.types {
		for _, magic := range ct.MagicBytes() {
			if bytes.HasPrefix(buf, magic) {
				return ct
			}
		}
	}
	return nil
}
