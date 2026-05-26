//go:build !darwin

package ocr

import (
	"context"
	"errors"
)

// Non-darwin stub. No provider registers; HasProvider() returns false;
// consumer code's `if ocr.HasProvider()` gate short-circuits cleanly.
//
// When a Linux Tesseract or Windows.Media.Ocr provider lands, it adds
// its own build-tagged file (e.g. tesseract_linux.go) with an init()
// calling Register. This file stays untouched — it's the "everything
// else falls through to no-op" sentinel.

// ErrProviderUnavailable mirrors the darwin export so callers can
// reference the symbol unconditionally without build-tag gymnastics.
// Non-darwin code paths can `if errors.Is(err, ocr.ErrProviderUnavailable)`
// just as cleanly as the darwin path.
var ErrProviderUnavailable = errors.New("ocr: provider not available on this platform")

// unusedSink keeps imports referenced even when no provider registers,
// future-proofing against linters that flag unused imports in this
// stub.
var _ = context.Canceled
