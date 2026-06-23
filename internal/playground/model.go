package playground

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
	"github.com/richardwooding/ollamaembed"
)

// RunOptions configures the playground. Opts carries the walk scope
// (Roots / Excludes / RespectGitignore / PruneBuildArtefacts / Workers /
// IncludeBody / BodyMaxBytes / Index); Run forces Expr="true" +
// IncludeAttributes so every file's attributes are snapshotted for in-memory
// filtering. Limit caps the snapshot (0 → a sane default).
//
// Semantic mode is opt-in: when Embedder is non-nil the TUI shows a
// natural-language query box above the CEL box. Typing a query and pressing
// Enter embeds it and re-walks with per-file cosine similarity, then the CEL
// box filters that snapshot live ("is_source && similarity > 0.6"). The
// remaining embedding fields are carried for the reproducible `search` command
// printed on exit.
type RunOptions struct {
	Opts     search.Options
	Registry *content.Registry
	Initial  string
	Limit    int

	Embedder            ollamaembed.Embedder
	EmbeddingModel      string
	EmbeddingServer     string
	SimilarityThreshold float64
	EmbedMaxBytes       int
	SemanticQuery       string
}

const defaultLimit = 5000

// defaultServer mirrors the CLI default so reproCommand can omit a redundant
// --embedding-server flag.
const defaultServer = "http://localhost:11434"

// Run launches the TUI and blocks until the user quits. It returns a string
// the caller prints so the query built here is reusable: in semantic mode a
// full `file-search-on search --semantic-query …` command, otherwise the bare
// CEL expression (the long-standing playground behaviour).
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
	return final.(model).finalOutput(), nil
}

// loadedMsg carries the plain (no-similarity) attribute snapshot.
type loadedMsg struct {
	results []search.Result
	capped  bool
	err     error
}

// semanticDoneMsg carries the result of an embed-the-query-then-walk pass. The
// query is echoed back so a stale in-flight search (the user kept typing) can
// be told apart from the current one if needed.
type semanticDoneMsg struct {
	query   string
	results []search.Result
	capped  bool
	err     error
}

// focusArea is which pane receives keystrokes: a text input, or the scrollable
// attributes panel.
type focusArea int

const (
	focusSem   focusArea = iota // natural-language query box (semantic mode)
	focusCEL                    // CEL filter box
	focusAttrs                  // the scrollable attributes panel
)

type model struct {
	ctx   context.Context
	opts  RunOptions
	input textinput.Model // CEL filter
	sem   textinput.Model // natural-language query (semantic mode only)
	attrs viewport.Model  // scrollable attributes reference (right panel)

	semantic bool // semantic mode enabled (opts.Embedder != nil)
	focus    focusArea

	results []search.Result
	loaded  bool
	loadErr error
	capped  bool

	searching     bool  // an embed+walk is in flight
	searchErr     error // last semantic search error
	hasSimilarity bool  // the current snapshot carries similarity scores

	matched  []int
	selected int
	top      int // scroll offset into matched

	errMsg     string // compile error for the current expression
	showSchema bool   // attributes panel is open
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

	m := model{ctx: ctx, opts: o, input: ti, finalExpr: o.Initial, width: 80, height: 24}
	m.semantic = o.Embedder != nil
	m.attrs = viewport.New(0, 0)

	if m.semantic {
		sem := textinput.New()
		sem.Placeholder = `how does authentication work`
		sem.Prompt = "› "
		sem.SetValue(o.SemanticQuery)
		sem.CursorEnd()
		sem.Focus() // start in the query box so the user can search first
		m.sem = sem
		m.focus = focusSem
		// An initial query means we go straight to a semantic walk; show the
		// busy state until semanticDoneMsg lands.
		if strings.TrimSpace(o.SemanticQuery) != "" {
			m.searching = true
		}
	} else {
		m.input.Focus()
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.semantic && strings.TrimSpace(m.opts.SemanticQuery) != "" {
		// Skip the plain snapshot — the semantic walk populates results.
		return tea.Batch(textinput.Blink, m.semanticSearchCmd(m.opts.SemanticQuery))
	}
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

// semanticSearchCmd embeds query once, then walks with per-file similarity
// computed and sorted descending. File embeddings cache in opts.Opts.Index, so
// re-queries over a warm tree only re-embed the query and recompute
// dot-products.
func (m model) semanticSearchCmd(query string) tea.Cmd {
	o := m.opts
	ctx := m.ctx
	embedder := o.Embedder
	return func() tea.Msg {
		vec, err := embedder.Embed(ctx, query)
		if err != nil {
			return semanticDoneMsg{query: query, err: fmt.Errorf("embed query: %w", err)}
		}
		ollamaembed.Normalize(vec)
		w := o.Opts
		w.Expr = "true"
		w.IncludeAttributes = true
		w.Limit = o.Limit
		w.Embedder = embedder
		w.SemanticQueryEmbedding = vec
		w.EmbedInputMaxBytes = o.EmbedMaxBytes
		w.Sort = "similarity"
		w.Order = "desc"
		res, walkErr := search.Walk(ctx, w, o.Registry)
		return semanticDoneMsg{query: query, results: res, capped: len(res) >= o.Limit, err: walkErr}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncAttrsViewport()
		return m, nil

	case loadedMsg:
		m.loaded = true
		m.loadErr = msg.err
		m.results = msg.results
		m.capped = msg.capped
		m.refilter()
		return m, nil

	case semanticDoneMsg:
		m.searching = false
		m.loaded = true
		if msg.err != nil {
			m.searchErr = msg.err
			return m, nil
		}
		m.searchErr = nil
		m.results = msg.results
		m.capped = msg.capped
		m.hasSimilarity = true
		m.refilter()
		return m, nil

	case tea.KeyMsg:
		return m.updateKey(msg)
	}

	// Non-key messages (cursor blink, etc.). Drive both inputs so the focused
	// box's cursor keeps blinking in semantic mode.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.semantic {
		var c2 tea.Cmd
		m.sem, c2 = m.sem.Update(msg)
		cmd = tea.Batch(cmd, c2)
	}
	return m, cmd
}

// updateKey handles a keystroke for both plain and semantic modes. Global keys
// (quit / panel toggle / focus cycle / submit) are handled first; otherwise the
// key is routed to whichever pane has focus — the attributes viewport scrolls,
// the match list navigates, or the focused text input edits.
func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.finalExpr = m.input.Value()
		return m, tea.Quit
	case "ctrl+a":
		return m, m.toggleAttrs()
	case "tab":
		return m, m.cycleFocus()
	case "enter":
		if m.semantic {
			if m.focus != focusSem {
				return m, nil // CEL filter is live; nothing to submit
			}
			q := strings.TrimSpace(m.sem.Value())
			if q == "" || m.searching {
				return m, nil
			}
			m.searching = true
			m.searchErr = nil
			return m, m.semanticSearchCmd(q)
		}
		// Plain mode preserves the original contract: enter copies the
		// expression and quits.
		m.finalExpr = m.input.Value()
		return m, tea.Quit
	}

	// Attributes panel focused → its viewport handles all scroll keys.
	if m.focus == focusAttrs {
		var cmd tea.Cmd
		m.attrs, cmd = m.attrs.Update(msg)
		return m, cmd
	}

	// Match-list navigation.
	switch msg.String() {
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

	// Otherwise edit the focused text input.
	if m.semantic && m.focus == focusSem {
		var cmd tea.Cmd
		m.sem, cmd = m.sem.Update(msg)
		return m, cmd
	}
	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.refilter()
	}
	return m, cmd
}

// toggleAttrs opens/closes the right-hand attributes panel. Opening focuses it
// (so ↑/↓ scroll immediately); closing returns focus to the default input.
func (m *model) toggleAttrs() tea.Cmd {
	m.showSchema = !m.showSchema
	if m.showSchema {
		m.syncAttrsViewport()
		m.focus = focusAttrs
		m.sem.Blur()
		m.input.Blur()
		return nil
	}
	if m.semantic {
		m.focus = focusSem
	} else {
		m.focus = focusCEL
	}
	return m.applyFocus()
}

// cycleFocus advances focus through the available panes: the query box (when
// semantic), the CEL box, and the attributes panel (when open).
func (m *model) cycleFocus() tea.Cmd {
	order := make([]focusArea, 0, 3)
	if m.semantic {
		order = append(order, focusSem)
	}
	order = append(order, focusCEL)
	if m.showSchema {
		order = append(order, focusAttrs)
	}
	cur := 0
	for i, f := range order {
		if f == m.focus {
			cur = i
		}
	}
	m.focus = order[(cur+1)%len(order)]
	return m.applyFocus()
}

// applyFocus blurs the inactive inputs and focuses the active one (the
// attributes panel takes keystrokes directly, not via a text input).
func (m *model) applyFocus() tea.Cmd {
	switch m.focus {
	case focusSem:
		m.input.Blur()
		return m.sem.Focus()
	case focusCEL:
		m.sem.Blur()
		return m.input.Focus()
	default: // focusAttrs
		m.sem.Blur()
		m.input.Blur()
		return nil
	}
}

// syncAttrsViewport sizes the attributes viewport to the current panel
// geometry and (re)builds its content truncated to that width.
func (m *model) syncAttrsViewport() {
	w := max(m.attrsPanelWidth(), 8)
	h := max(
		// one row for the panel heading
		m.listHeight()-1, 1)
	m.attrs.Width = w
	m.attrs.Height = h
	m.attrs.SetContent(attrsContent(w))
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

// finalOutput is what Run returns (and the caller prints) on quit. In semantic
// mode it's a reproducible `file-search-on search` command; otherwise the bare
// CEL expression, preserving the original playground contract.
func (m model) finalOutput() string {
	if !m.semantic {
		return m.finalExpr
	}
	return m.reproCommand()
}

func (m model) reproCommand() string {
	q := strings.TrimSpace(m.sem.Value())
	cel := strings.TrimSpace(m.input.Value())

	var b strings.Builder
	b.WriteString("file-search-on search")
	if q != "" {
		b.WriteString(" --semantic-query " + shellQuote(q))
	}
	if m.opts.EmbeddingModel != "" {
		b.WriteString(" --embedding-model " + shellQuote(m.opts.EmbeddingModel))
	}
	if m.opts.EmbeddingServer != "" && m.opts.EmbeddingServer != defaultServer {
		b.WriteString(" --embedding-server " + shellQuote(m.opts.EmbeddingServer))
	}
	fmt.Fprintf(&b, " --similarity-threshold %v", m.opts.SimilarityThreshold)
	for _, d := range m.opts.Opts.Roots {
		if d != "" && d != "." {
			b.WriteString(" -d " + shellQuote(d))
		}
	}
	if cel != "" {
		b.WriteString(" " + shellQuote(cel))
	}
	return b.String()
}

// shellQuote single-quotes s for a POSIX shell, escaping embedded quotes.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
