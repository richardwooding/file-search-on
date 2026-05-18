package mcpserver_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/mcpserver"
)

// pickPort returns a free localhost port for the test to bind.
func pickPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestRunHTTPStartsAndShutsDown(t *testing.T) {
	addr := pickPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- mcpserver.RunHTTP(ctx, "test", addr, "/", index.NewMemory(), 0, mcpserver.EmbedDefaults{})
	}()

	// Give the listener a moment to bind.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunHTTP returned error after shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunHTTP did not return within shutdown timeout")
	}
}

func TestRunSSEStartsAndShutsDown(t *testing.T) {
	addr := pickPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- mcpserver.RunSSE(ctx, "test", addr, "/", index.NewMemory(), 0, mcpserver.EmbedDefaults{})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunSSE returned error after shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunSSE did not return within shutdown timeout")
	}
}

func TestRunHTTPInUseAddressFails(t *testing.T) {
	// Bind a listener so the address is occupied.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	// Try to start the HTTP transport on the same address; expect a real error.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = mcpserver.RunHTTP(ctx, "test", l.Addr().String(), "/", index.NewMemory(), 0, mcpserver.EmbedDefaults{})
	if err == nil {
		t.Fatal("expected error binding to busy address, got nil")
	}
	if !strings.Contains(err.Error(), "address already in use") &&
		!strings.Contains(err.Error(), "bind") {
		t.Logf("got error: %v (not asserting exact phrasing across platforms)", err)
	}
}
