package content

import (
	"context"
	"io/fs"
	"sync"
)

// Attributes is a map of attribute name to value for CEL evaluation
type Attributes map[string]any

// ContentType represents a detectable file content type
type ContentType interface {
	// Name returns the content type name (e.g., "markdown", "json")
	Name() string
	// Extensions returns file extensions this type handles (lowercase with dot, e.g. ".md")
	Extensions() []string
	// MagicBytes returns magic byte sequences for detection (nil if not used)
	MagicBytes() [][]byte
	// Attributes extracts type-specific attributes from the file at path
	// on fsys. Path is interpreted as an fs.FS-style key (forward slashes,
	// relative to the FS root). Implementations should check ctx.Err() at
	// entry and (for loop-bound readers) periodically during scanning.
	// Returning ctx.Err() on cancellation lets the walker terminate cleanly.
	//
	// Production threads `os.DirFS(root)` here; tests can pass embed.FS or
	// fstest.MapFS for hermetic execution.
	Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error)
}

// Registry holds all registered content types
type Registry struct {
	mu    sync.RWMutex
	types []ContentType
}

var defaultRegistry = &Registry{}

// Register adds a content type to the default registry
func Register(ct ContentType) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.types = append(defaultRegistry.types, ct)
}

// DefaultRegistry returns the default registry
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Types returns all registered content types
func (r *Registry) Types() []ContentType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ContentType, len(r.types))
	copy(result, r.types)
	return result
}
