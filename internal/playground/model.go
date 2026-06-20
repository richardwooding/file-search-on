package playground

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// RunOptions configures the playground. Opts carries the walk scope
// (Roots / Excludes / RespectGitignore / PruneBuildArtefacts / Workers /
// IncludeBody / BodyMaxBytes); Run forces Expr="true" + IncludeAttributes so
// every file's attributes are snapshotted for in-memory filtering. Limit
// caps the snapshot (0 → a sane default).
type RunOptions struct {
	Opts     search.Options
	Registry *content.Registry
	Initial  string
	Limit    int
}

const defaultLimit = 5000

// Run launches the TUI and blocks until the user quits, returning the final
// CEL expression they had typed (the caller prints it so a query built here
// is reusable).
func Run(ctx context.Context, o RunOptions) (string, error) {
	if o.Limit <= 0 {
		o.Limit = defaultLimit
	}
	if o.Registry == nil {
		o.Registry = content.DefaultRegistry()
	}
	m := newModel(ctx, o)
	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	return final.(model).finalExpr, nil
}

type loadedMsg struct {
	results []search.Result
	capped  bool
	err     error
}

type model struct {
	ctx   context.Context
	opts  RunOptions
	input textinput.Model

	results []search.Result
	loaded  bool
	loadErr error
	capped  bool

	matched  []int
	selected int
	top      int // scroll offset into matched

	errMsg     string // compile error for the current expression
	showSchema bool
	finalExpr  string
	width      int
	height     int
}

func newModel(ctx context.Context, o RunOptions) model {
	ti := textinput.New()
	ti.Placeholder = `is_markdown && word_count > 500`
	ti.Prompt = "› "
	ti.SetValue(o.Initial)
	ti.CursorEnd()
	ti.Focus()
	return model{ctx: ctx, opts: o, input: ti, finalExpr: o.Initial, width: 80, height: 24}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.loadCmd())
}

func (m model) loadCmd() tea.Cmd {
	o := m.opts
	return func() tea.Msg {
		w := o.Opts
		w.Expr = "true"
		w.IncludeAttributes = true
		w.Limit = o.Limit
		res, err := search.Walk(m.ctx, w, o.Registry)
		return loadedMsg{results: res, capped: len(res) >= o.Limit, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case loadedMsg:
		m.loaded = true
		m.loadErr = msg.err
		m.results = msg.results
		m.capped = msg.capped
		m.refilter()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "enter":
			m.finalExpr = m.input.Value()
			return m, tea.Quit
		case "tab":
			m.showSchema = !m.showSchema
			return m, nil
		case "up":
			m.move(-1)
			return m, nil
		case "down":
			m.move(1)
			return m, nil
		case "pgup":
			m.move(-m.listHeight())
			return m, nil
		case "pgdown":
			m.move(m.listHeight())
			return m, nil
		}
		// Everything else edits the expression.
		prev := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != prev {
			m.refilter()
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// refilter recompiles the current expression and recomputes the match set.
// A compile error is shown and the previous match set is kept.
func (m *model) refilter() {
	idx, err := filter(m.results, m.input.Value())
	if err != nil {
		m.errMsg = compileErr(err)
		return
	}
	m.errMsg = ""
	m.matched = idx
	if m.selected >= len(m.matched) {
		m.selected = len(m.matched) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.clampScroll()
}

func (m *model) move(delta int) {
	if len(m.matched) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.matched) {
		m.selected = len(m.matched) - 1
	}
	m.clampScroll()
}

func (m *model) clampScroll() {
	h := m.listHeight()
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+h {
		m.top = m.selected - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// compileErr trims cel-go's multi-line compile error to its first line for a
// compact one-line status.
func compileErr(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
