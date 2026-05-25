package content

import (
	"os"
	"path/filepath"
	"strings"
)

// Apple Live Photos pair an HEIC still image with a short MOV video
// that captures the moment around the shutter press. On disk both
// files sit side-by-side with the same basename:
//
//	IMG_1234.HEIC   <- still
//	IMG_1234.MOV    <- ~3-second video
//
// PairLivePhoto checks for the sibling file at the same path, trying
// each candidate extension in `wantExts` in order. Returns the
// absolute sibling path + its byte size on the first hit; "" / 0 /
// false when no candidate exists.
//
// Extensions are tried verbatim (no case-folding inside the function)
// so the caller controls the search order — iPhone exports use
// uppercase `.HEIC` / `.MOV` by convention, so callers should put the
// uppercase variant first to skip a needless stat on the common case.
//
// Issue #194.
func PairLivePhoto(displayPath string, wantExts []string) (siblingPath string, siblingSize int64, found bool) {
	if displayPath == "" || len(wantExts) == 0 {
		return "", 0, false
	}
	// Strip the trailing extension and try each candidate. filepath.Ext
	// returns the LAST extension including the leading dot — fine for
	// `IMG_1234.HEIC` (no inner dots in real iPhone exports). A pathol-
	// ogical input like `IMG.x.HEIC` will look for `IMG.x.MOV` rather
	// than `IMG.MOV` — matches Apple's behaviour, since the still and
	// video always share the FULL basename.
	ext := filepath.Ext(displayPath)
	if ext == "" {
		return "", 0, false
	}
	stem := strings.TrimSuffix(displayPath, ext)
	for _, candidate := range wantExts {
		path := stem + candidate
		if path == displayPath {
			// Self-reference (caller passed its own ext in wantExts);
			// skip — a file can't pair with itself.
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, info.Size(), true
	}
	return "", 0, false
}

// Canonical case variants. Uppercase first — iPhone exports use
// `.HEIC` / `.MOV` by default; trying the common case first skips a
// needless stat for the dominant workflow.
var (
	livePhotoVideoExts = []string{".MOV", ".mov"}
	livePhotoImageExts = []string{".HEIC", ".heic"}
)

// FindLivePhotoVideo looks for the MOV sibling of an HEIC.
func FindLivePhotoVideo(heicPath string) (path string, size int64, found bool) {
	return PairLivePhoto(heicPath, livePhotoVideoExts)
}

// FindLivePhotoImage looks for the HEIC sibling of a MOV. The size of
// the still isn't returned — typical agent queries care about the
// video's size, not the image's (the still is small and similar
// across all Live Photos).
func FindLivePhotoImage(movPath string) (path string, found bool) {
	p, _, ok := PairLivePhoto(movPath, livePhotoImageExts)
	return p, ok
}

// IsLivePhotoVideoExt reports whether the path's extension makes it a
// candidate Live Photo VIDEO — `.mov` / `.MOV`. The `video/mp4`
// content type covers .mp4, .mov, .m4v, but Apple's Live Photo videos
// are always .mov; gating on extension avoids false positives where
// an `IMG_1234.mp4` next to `IMG_1234.HEIC` would incorrectly fire
// `is_live_photo_video`.
func IsLivePhotoVideoExt(displayPath string) bool {
	ext := filepath.Ext(displayPath)
	return ext == ".mov" || ext == ".MOV"
}
