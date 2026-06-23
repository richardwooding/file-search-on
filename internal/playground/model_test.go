package playground

import (
	"context"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	case "ctrl+a":
		return tea.KeyMsg{Type: tea.KeyCtrlA}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
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

func TestRenderList_RowsNeverExceedWidth(t *testing.T) {
	// Long paths + a dim-styled size column previously got byte-truncated as a
	// whole styled string, corrupting ANSI escapes. Each rendered row's visible
	// width must stay within the list budget so it never wraps.
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 50, Height: 20})
	long := "internal/search/very/deeply/nested/directory/structure/walker_orchestrator.go"
	m = drive(m, loadedMsg{results: []search.Result{
		res(long, "source/go", 123456, false),
		res(long+"/again.go", "source/go", 42, false),
	}})

	for line := range strings.SplitSeq(m.renderList(), "\n") {
		if w := lipgloss.Width(line); w > m.listWidth() {
			t.Errorf("row width %d exceeds listWidth %d: %q", w, m.listWidth(), line)
		}
		// Stripping complete SGR sequences must leave no stray ESC behind; a
		// dangling ESC means an escape was sliced mid-sequence.
		if stripped := sgrRE.ReplaceAllString(line, ""); strings.ContainsRune(stripped, '\x1b') {
			t.Errorf("row has a corrupted ANSI escape: %q", line)
		}
	}
}

// sgrRE matches a complete ANSI SGR (colour/style) escape sequence.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestView_AttrsPanelRendersContent(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = drive(m, loaded())
	m = drive(m, key("ctrl+a")) // open the panel

	out := m.View()
	if !strings.Contains(out, "attributes") {
		t.Error("attributes panel heading should render")
	}
	if !strings.Contains(out, "content_type") {
		t.Error("a known attribute name should appear in the scrollable panel")
	}
}

func TestModel_AttrsPanelScrollsWhenFocused(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = drive(m, loaded())
	m = drive(m, key("ctrl+a")) // open + focus the panel
	before := m.attrs.YOffset
	m = drive(m, key("pgdown"))
	if m.attrs.YOffset <= before {
		t.Errorf("focused attributes panel should scroll on pgdown, offset %d → %d", before, m.attrs.YOffset)
	}
	// With focus on the panel, navigation keys must NOT move the list selection.
	if m.selected != 0 {
		t.Errorf("list selection should stay put while the panel is focused, got %d", m.selected)
	}
}

func TestModel_CtrlATogglesAttrsPanel(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drive(m, loaded())
	if m.showSchema {
		t.Fatal("attributes panel should start hidden")
	}
	m = drive(m, key("ctrl+a"))
	if !m.showSchema {
		t.Error("ctrl+a should reveal the attributes panel")
	}
	if m.focus != focusAttrs {
		t.Error("opening the panel should focus it so ↑/↓ scroll immediately")
	}
	m = drive(m, key("ctrl+a"))
	if m.showSchema {
		t.Error("ctrl+a again should hide the attributes panel")
	}
	if m.focus != focusCEL {
		t.Error("closing the panel should return focus to the CEL box")
	}
}

func TestModel_TabFocusesAttrsPanelWhenOpen(t *testing.T) {
	m := newModel(context.Background(), RunOptions{})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = drive(m, loaded())
	m = drive(m, key("ctrl+a")) // open + focus panel
	m = drive(m, key("tab"))    // cycle: panel → CEL
	if m.focus != focusCEL {
		t.Errorf("tab from the panel should return to the CEL box, got %v", m.focus)
	}
	m = drive(m, key("tab")) // CEL → panel
	if m.focus != focusAttrs {
		t.Errorf("tab should cycle back to the attributes panel, got %v", m.focus)
	}
}
