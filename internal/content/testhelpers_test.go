package content_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/richardwooding/file-search-on/internal/content"
)

// detectAt opens an absolute filesystem path through os.DirFS, splitting
// the path into its directory and base. Convenience for tests that already
// write fixtures to t.TempDir() and want to keep the call shape close to
// the pre-FS-refactor `Detect(path)` form.
func detectAt(path string) content.ContentType {
	dir := filepath.Dir(path)
	return content.DefaultRegistry().Detect(os.DirFS(dir), filepath.Base(path))
}

// attributesAt is the Attributes counterpart of detectAt.
func attributesAt(ctx context.Context, ct content.ContentType, path string) (content.Attributes, error) {
	dir := filepath.Dir(path)
	return ct.Attributes(ctx, os.DirFS(dir), filepath.Base(path))
}
