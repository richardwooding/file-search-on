package celexpr

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/richardwooding/file-search-on/internal/fingerprint"
)

// imageFunctions returns the cel.EnvOption set for image-similarity
// helpers. Currently a single primitive: image_similar_to. Signature:
//
//	image_similar_to(phash, reference_path, threshold) -> bool
//
// Returns true when the file's perceptual hash (passed via the `phash`
// CEL variable) and the reference image's pHash differ by ≤
// (1 - threshold) × 64 bits. Reference images are loaded + hashed once
// per process via a process-wide sync.Map keyed by absolute path —
// scales cleanly across worker-parallel walks.
//
// Issue #208.
func imageFunctions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("image_similar_to",
			cel.Overload("image_similar_to_string_string_double",
				[]*cel.Type{cel.StringType, cel.StringType, cel.DoubleType},
				cel.BoolType,
				cel.FunctionBinding(imageSimilarToBinding),
			),
		),
	}
}

// referencePHashCache caches the pHash of reference image paths so
// repeated calls within a walk skip the decode + DCT. Keyed by the
// caller-supplied path (after ~ expansion). Concurrency-safe — many
// workers can hit the same reference simultaneously.
var referencePHashCache sync.Map // map[string]referencePHashResult

type referencePHashResult struct {
	hash uint64
	err  error
}

// loadReferencePHash returns the pHash of the reference image at
// path, caching the result across evaluator instances. ~ expansion
// is performed so users can write image_similar_to(phash, "~/Pictures/
// ref.jpg", 0.85) without pre-expanding.
func loadReferencePHash(path string) (uint64, error) {
	expanded := expandTilde(path)
	if cached, ok := referencePHashCache.Load(expanded); ok {
		r := cached.(referencePHashResult)
		return r.hash, r.err
	}

	f, err := os.Open(expanded)
	if err != nil {
		referencePHashCache.Store(expanded, referencePHashResult{err: err})
		return 0, err
	}
	defer func() { _ = f.Close() }()

	hash, err := fingerprint.PHash(f)
	referencePHashCache.Store(expanded, referencePHashResult{hash: hash, err: err})
	return hash, err
}

// expandTilde converts a leading "~/" to $HOME/. Anything else is
// returned unchanged. Mirrors the convention used elsewhere in the
// codebase for user-friendly path inputs.
func expandTilde(path string) string {
	if len(path) < 2 || path[0] != '~' {
		return path
	}
	if path[1] != '/' && path[1] != filepath.Separator {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// imageSimilarToBinding adapts the CEL ref.Val args, loads the
// reference pHash (cached), and compares against the file's pHash
// via Hamming distance.
//
// Returns false (silent failure) when:
//   - the file's phash is empty (non-image, or --with-phash off)
//   - the reference image can't be opened or decoded
//   - the reference pHash is zero (degenerate image)
//
// Silent failure matches the "best effort" contract of every other
// pHash-adjacent CEL primitive — callers see "no match" rather than
// runtime errors that would abort the walk.
func imageSimilarToBinding(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.False
	}
	phashHex, ok := args[0].Value().(string)
	if !ok || phashHex == "" {
		return types.False
	}
	refPath, ok := args[1].Value().(string)
	if !ok || refPath == "" {
		return types.False
	}
	threshold, ok := args[2].Value().(float64)
	if !ok {
		return types.False
	}

	filePHash, err := fingerprint.PHashFromHex(phashHex)
	if err != nil {
		return types.False
	}
	refPHash, err := loadReferencePHash(refPath)
	if err != nil || refPHash == 0 {
		return types.False
	}

	// Convert the 0..1 similarity threshold to a 0..64 Hamming
	// distance cap. threshold=1.0 → cap=0 (must be identical);
	// threshold=0.85 → cap=9 (≈ the SimHash convention).
	if threshold > 1.0 {
		threshold = 1.0
	}
	if threshold < 0.0 {
		threshold = 0.0
	}
	maxBits := int((1.0 - threshold) * 64.0)
	d := fingerprint.Distance(filePHash, refPHash)
	return types.Bool(d <= maxBits)
}
