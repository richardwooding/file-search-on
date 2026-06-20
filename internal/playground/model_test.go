package playground

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/richardwooding/file-search-on/internal/search"
)

// drive feeds a message through Update and returns the concrete model so tests
// can assert on its state without standing up a real terminal.
func drive(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{}
}

func loaded() loadedMsg {
	return loadedMsg{results: []search.Result{
		res("a.md", "markdown", 100, true),
		res("b.md", "markdown", 9000, true),
		res("c.go", "source/go", 500, false),
	}}
}

func TestModel_LoadAndFilterLive(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drive(m, loaded())

	if !m.loaded || len(m.results) != 3 {
		t.Fatalf("expected 3 loaded results, got loaded=%v n=%d", m.loaded, len(m.results))
	}
	// Empty expression matches everything.
	if len(m.matched) != 3 {
		t.Fatalf("empty expr should match all 3, got %d", len(m.matched))
	}

	// Type a predicate one rune at a time; the live filter narrows the set.
	for _, r := range "is_markdown" {
		m = drive(m, key(string(r)))
	}
	if len(m.matched) != 2 {
		t.Fatalf("is_markdown should match 2 (a.md, b.md), got %d", len(m.matched))
	}
}

func TestModel_CompileErrorKeepsPriorMatches(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, loaded())
	for _, r := range "is_markdown" {
		m = drive(m, key(string(r)))
	}
	prior := len(m.matched) // 2

	// A trailing operator is a compile error.
	m = drive(m, key(" "))
	m = drive(m, key("&"))
	m = drive(m, key("&"))

	if m.errMsg == "" {
		t.Error("expected a compile error to be surfaced")
	}
	if len(m.matched) != prior {
		t.Errorf("compile error should keep the prior match set (%d), got %d", prior, len(m.matched))
	}
}

func TestModel_NavigationClampsToMatches(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drive(m, loaded())

	m = drive(m, key("down"))
	m = drive(m, key("down"))
	if m.selected != 2 {
		t.Fatalf("selected should be 2 after two downs, got %d", m.selected)
	}
	// Past the end clamps.
	m = drive(m, key("down"))
	if m.selected != 2 {
		t.Errorf("selected should clamp at last index 2, got %d", m.selected)
	}
	// Up returns.
	m = drive(m, key("up"))
	if m.selected != 1 {
		t.Errorf("selected should be 1 after up, got %d", m.selected)
	}
}

func TestModel_EnterRecordsFinalExpr(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, loaded())
	for _, r := range "is_source" {
		m = drive(m, key(string(r)))
	}
	m = drive(m, key("enter"))
	if m.finalExpr != "is_source" {
		t.Errorf("enter should record the typed expression, got %q", m.finalExpr)
	}
}

func TestModel_TabTogglesSchema(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, loaded())
	if m.showSchema {
		t.Fatal("schema panel should start hidden")
	}
	m = drive(m, key("tab"))
	if !m.showSchema {
		t.Error("tab should reveal the schema panel")
	}
	m = drive(m, key("tab"))
	if m.showSchema {
		t.Error("tab again should hide the schema panel")
	}
}
