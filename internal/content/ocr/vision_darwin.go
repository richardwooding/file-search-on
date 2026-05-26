//go:build darwin

package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Vision OCR provider for macOS. Shells out to the Swift helper
// (internal/content/ocr/vision-helper/main.swift, compiled separately
// — see Makefile) which uses VNRecognizeTextRequest + Natural Language
// language detection and emits JSON to stdout.
//
// Helper location search order:
//  1. $FILE_SEARCH_ON_OCR_HELPER env var (explicit override; testing /
//     non-standard installs).
//  2. Sibling to os.Executable() named "file-search-on-ocr-helper" —
//     the Homebrew cask install path lays them out this way.
//  3. PATH lookup for "file-search-on-ocr-helper" — covers `go install`
//     style flows where the helper lives in $GOBIN.
//
// The result is cached after the first successful resolution (sync.Once)
// so repeat Available()/Recognize() calls are essentially free.
//
// Issue #189.

const helperBinaryName = "file-search-on-ocr-helper"
const helperEnvOverride = "FILE_SEARCH_ON_OCR_HELPER"

func init() {
	Register(&visionProvider{})
}

type visionProvider struct {
	once       sync.Once
	helperPath string
	helperOK   bool
}

func (*visionProvider) Name() string { return "vision-macos" }

func (v *visionProvider) Available() bool {
	v.resolveHelper()
	return v.helperOK
}

// resolveHelper performs the three-tier search described in the file
// header. Cached via sync.Once so Available()/Recognize() can be called
// in tight loops without re-stating every time.
func (v *visionProvider) resolveHelper() {
	v.once.Do(func() {
		// 1. Env override.
		if env := os.Getenv(helperEnvOverride); env != "" {
			if isExecutable(env) {
				v.helperPath = env
				v.helperOK = true
				return
			}
		}
		// 2. Sibling to os.Executable() — Homebrew cask installs both
		// binaries into the same bin/ directory.
		if exe, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			candidate := filepath.Join(filepath.Dir(exe), helperBinaryName)
			if isExecutable(candidate) {
				v.helperPath = candidate
				v.helperOK = true
				return
			}
		}
		// 3. PATH lookup — covers `go install` to $GOBIN, custom dev
		// setups, etc.
		if found, err := exec.LookPath(helperBinaryName); err == nil {
			v.helperPath = found
			v.helperOK = true
			return
		}
	})
}

// isExecutable returns true when the path exists, is a regular file,
// and has at least one execute bit set.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// helperResponse is the wire format the Swift helper writes to stdout.
// Match field names + types exactly.
type helperResponse struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Language   string  `json:"language"`
}

// Recognize spawns the Swift helper, captures stdout, parses JSON,
// returns a Result. The caller's ctx carries the per-file timeout —
// when ctx is cancelled / times out, exec.CommandContext sends SIGKILL
// to the helper process.
//
// Errors:
//   - helper not Available: returns ErrProviderUnavailable
//   - helper exited non-zero: error wraps the stderr output
//   - JSON parse failure: error includes the raw stdout for triage
//   - ctx timeout: error wraps ctx.Err() — caller can detect via errors.Is
//
// A successful return with empty Result.Text indicates the image was
// OCRed cleanly but contained no recognizable text (blank screenshot,
// non-text imagery).
func (v *visionProvider) Recognize(ctx context.Context, osPath string) (Result, error) {
	v.resolveHelper()
	if !v.helperOK {
		return Result{}, ErrProviderUnavailable
	}

	cmd := exec.CommandContext(ctx, v.helperPath, osPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// ctx-cancellation paths surface here too — preserve the
		// underlying ctx error so callers can distinguish timeout from
		// helper-internal failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, fmt.Errorf("ocr: %w (helper stderr: %s)", ctxErr, stderr.String())
		}
		return Result{}, fmt.Errorf("ocr helper: %w (stderr: %s)", err, stderr.String())
	}

	if stdout.Len() == 0 {
		// Helper returned 0 exit with empty stdout — shouldn't happen
		// but treat as "no text" rather than failing the walk.
		return Result{Provider: v.Name()}, nil
	}

	var resp helperResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return Result{}, fmt.Errorf("ocr: parse helper output: %w (stdout: %q)", err, stdout.String())
	}

	return Result{
		Text:       resp.Text,
		Confidence: resp.Confidence,
		Language:   resp.Language,
		Provider:   v.Name(),
	}, nil
}

// ErrProviderUnavailable is returned by Recognize when the provider's
// helper isn't installed / locatable. Callers can ignore this and fall
// through to "no body" — same contract as every other best-effort body
// extractor.
var ErrProviderUnavailable = errors.New("ocr: provider not available")
