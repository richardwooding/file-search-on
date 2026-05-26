package ocr

import (
	"context"
	"runtime"
	"slices"
	"testing"
)

// fakeProvider lets registry tests verify Register / Default / etc.
// without relying on the (build-tagged) real providers.
type fakeProvider struct {
	name      string
	available bool
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Available() bool { return f.available }
func (f *fakeProvider) Recognize(_ context.Context, _ string) (Result, error) {
	return Result{Provider: f.name}, nil
}

// withResetRegistry runs fn with an empty provider registry, then
// restores the previous state. Required because Register mutates
// package-level state shared across the whole binary's lifetime.
func withResetRegistry(t *testing.T, fn func()) {
	t.Helper()
	providersMu.Lock()
	saved := providers
	providers = nil
	providersMu.Unlock()

	t.Cleanup(func() {
		providersMu.Lock()
		providers = saved
		providersMu.Unlock()
	})

	fn()
}

func TestRegisterNilNoPanic(t *testing.T) {
	withResetRegistry(t, func() {
		Register(nil)
		if HasProvider() {
			t.Error("HasProvider should be false after Register(nil)")
		}
	})
}

func TestDefaultPicksFirstAvailable(t *testing.T) {
	withResetRegistry(t, func() {
		Register(&fakeProvider{name: "unavailable", available: false})
		Register(&fakeProvider{name: "ready", available: true})
		Register(&fakeProvider{name: "also-ready", available: true})

		got := Default()
		if got == nil {
			t.Fatal("Default returned nil")
		}
		if got.Name() != "ready" {
			t.Errorf("Default = %q, want %q (first Available)", got.Name(), "ready")
		}
	})
}

func TestHasProviderFalseWhenAllUnavailable(t *testing.T) {
	withResetRegistry(t, func() {
		Register(&fakeProvider{name: "no", available: false})
		if HasProvider() {
			t.Error("HasProvider should be false when no provider is Available")
		}
	})
}

func TestHasProviderFalseEmpty(t *testing.T) {
	withResetRegistry(t, func() {
		if HasProvider() {
			t.Error("HasProvider should be false on an empty registry")
		}
	})
}

func TestListProvidersIncludesUnavailable(t *testing.T) {
	withResetRegistry(t, func() {
		Register(&fakeProvider{name: "down", available: false})
		Register(&fakeProvider{name: "up", available: true})
		names := ListProviders()
		if len(names) != 2 {
			t.Fatalf("ListProviders = %v, want 2 entries", names)
		}
		// Order preserved per Register call ordering.
		if names[0] != "down" || names[1] != "up" {
			t.Errorf("ListProviders = %v, want [down up]", names)
		}
	})
}

// TestDarwinAutoRegisters verifies that on macOS the package's init()
// has registered the vision provider. The helper binary may or may
// not be installed (Available() varies); just confirm the name shows
// up in the registry.
func TestDarwinAutoRegisters(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("provider auto-registration is darwin-only")
	}
	// Don't reset the registry — we're inspecting the package's
	// real init() side effects.
	names := ListProviders()
	found := slices.Contains(names, "vision-macos")
	if !found {
		t.Errorf("expected vision-macos provider in registry; got %v", names)
	}
}

// TestNonDarwinNoAutoRegister verifies the !darwin stub doesn't
// register anything. On Linux / Windows, the registry is empty by
// default (unless the test imports something that registers).
func TestNonDarwinNoAutoRegister(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("only meaningful on non-darwin builds")
	}
	names := ListProviders()
	for _, n := range names {
		if n == "vision-macos" {
			t.Errorf("vision-macos should NOT register on %s", runtime.GOOS)
		}
	}
}
