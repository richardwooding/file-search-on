// Package ocr abstracts over OCR providers — macOS Vision today,
// Linux Tesseract / Windows.Media.Ocr deferred.
//
// Design (issue #189):
//   - One Provider interface; per-platform impls register themselves in
//     init() under //go:build tags. Consumer code (internal/content/
//     body.go + internal/celexpr/body.go) calls Default()/HasProvider()
//     and gets a clean no-op on platforms without a registered
//     provider.
//   - Result is provider-neutral (Text + Confidence + Language +
//     Provider name). Future Tesseract / Windows-OCR providers populate
//     the same shape; the CEL surface (ocr_confidence / ocr_language /
//     ocr_provider) doesn't change.
//   - The vision_darwin.go impl shells out to a small Swift helper
//     binary (internal/content/ocr/vision-helper/main.swift) since
//     Vision is a Swift-only Cocoa framework and we keep the Go side
//     pure-Go / no-CGO.
package ocr

import (
	"context"
	"sync"
)

// Result is the structured output of a single OCR call. All fields are
// best-effort: Text is the joined recognized lines (may be empty for
// images with no text); Confidence is the per-line confidence averaged
// across all observations (0..1); Language is a BCP-47 code like "en"
// or "zh-Hans" (empty when the recognizer can't decide); Provider is
// the registered name of the impl that produced the result (e.g.
// "vision-macos") — informational, lets agents filter by engine.
type Result struct {
	Text       string
	Confidence float64
	Language   string
	Provider   string
}

// Provider abstracts over a single OCR backend. Implementations live
// in build-tagged files (vision_darwin.go, vision_other.go, future
// tesseract_linux.go, etc.) and self-register via init() calling
// Register on this package.
type Provider interface {
	// Name is the canonical provider identifier surfaced as the
	// `ocr_provider` CEL attribute. Lowercase, hyphen-separated:
	// "vision-macos", "tesseract", "win-media-ocr".
	Name() string

	// Available reports whether this provider is ready to use right
	// now — required helper binaries / libraries present, runtime
	// permissions granted, etc. Cheap to call (cache the result in
	// the impl when expensive).
	Available() bool

	// Recognize runs OCR against the file at the given OS path. The
	// caller's ctx carries the per-file timeout. Returns a zero
	// Result + nil error when the image was successfully processed
	// but contains no recognizable text (common for blank
	// screenshots).
	Recognize(ctx context.Context, osPath string) (Result, error)
}

// providers is the package-level registry. Concurrency-safe via
// the RWMutex; populated at init() time by build-tagged provider
// files and read by Default() / HasProvider() at walk time.
var (
	providersMu sync.RWMutex
	providers   []Provider
)

// Register adds a provider to the registry. Called from the init() of
// each build-tagged impl. Order matters — earlier registrations win
// when multiple providers are Available(). On non-target platforms
// (the !darwin stub today), nothing registers.
func Register(p Provider) {
	if p == nil {
		return
	}
	providersMu.Lock()
	providers = append(providers, p)
	providersMu.Unlock()
}

// Default returns the first Available() registered provider, or nil
// when nothing is registered / available. The walker calls this once
// per file; results aren't cached at this level (impls cache
// availability internally where appropriate).
func Default() Provider {
	providersMu.RLock()
	defer providersMu.RUnlock()
	for _, p := range providers {
		if p.Available() {
			return p
		}
	}
	return nil
}

// HasProvider is the shorthand "is OCR usable right now?" check.
// Equivalent to Default() != nil but communicates intent more clearly
// at call sites that don't need the provider handle.
func HasProvider() bool {
	return Default() != nil
}

// ListProviders returns the names of all REGISTERED providers (whether
// Available or not). Used by --list and the MCP list_attributes
// surface so agents can discover which engines this build supports.
func ListProviders() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.Name())
	}
	return out
}
