package playground

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	selStyle    = lipgloss.NewStyle().Reverse(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	panelStyle  = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, false, false).MarginTop(1)
	// attrsPanelStyle is the scrollable right-hand attributes panel: a single
	// vertical rule on its left, a column of padding, and a left margin that
	// separates it from the match list. The 3 cols it adds (margin+border+pad)
	// are accounted for in listWidth.
	attrsPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("8")).
			PaddingLeft(1).
			MarginLeft(1)
)

// baseChromeRows is the number of non-list rows (title, input box, status,
// footer, detail panel) so listHeight can give the rest to the match list.
// Semantic mode adds a second input box + its label.
const baseChromeRows = 13
const semanticExtraRows = 4

func (m model) chromeRows() int {
	if m.semantic {
		return baseChromeRows + semanticExtraRows
	}
	return baseChromeRows
}

func (m model) listHeight() int {
	h := m.height - m.chromeRows()
	if h < 1 {
		return 1
	}
	return h
}

func (m model) View() string {
	var b strings.Builder

	if m.semantic {
		b.WriteString(titleStyle.Render("semantic playground") + dimStyle.Render("  —  natural-language search + live CEL filter") + "\n")
		b.WriteString(focusLabel("semantic query", m.focus == focusSem) + "\n")
		b.WriteString(boxStyle.Width(min(m.width-2, 100)).Render(m.sem.View()) + "\n")
		b.WriteString(focusLabel("CEL filter", m.focus == focusCEL) + "\n")
		b.WriteString(boxStyle.Width(min(m.width-2, 100)).Render(m.input.View()) + "\n")
	} else {
		b.WriteString(titleStyle.Render("CEL playground") + dimStyle.Render("  —  live filter, type a CEL expression") + "\n")
		b.WriteString(boxStyle.Width(min(m.width-2, 100)).Render(m.input.View()) + "\n")
	}

	// Status / error line.
	switch {
	case m.searching:
		b.WriteString(accentStyle.Render("embedding & searching…") + dimStyle.Render("  ("+m.opts.EmbeddingModel+")") + "\n")
	case m.searchErr != nil:
		b.WriteString(errStyle.Render("✗ "+semErrLine(m.searchErr)) + "\n")
	case !m.loaded:
		b.WriteString(dimStyle.Render("scanning…") + "\n")
	case m.loadErr != nil:
		b.WriteString(errStyle.Render("scan error: "+m.loadErr.Error()) + "\n")
	case m.errMsg != "":
		b.WriteString(errStyle.Render("✗ "+m.errMsg) + "\n")
	default:
		status := okStyle.Render(fmt.Sprintf("%d", len(m.matched))) + dimStyle.Render(fmt.Sprintf("/%d match", len(m.results)))
		if m.semantic && m.opts.EmbeddingModel != "" {
			status += dimStyle.Render("  · model " + m.opts.EmbeddingModel)
		}
		if m.capped {
			status += dimStyle.Render(fmt.Sprintf("  (first %d shown — narrow with -d / --limit)", len(m.results)))
		}
		b.WriteString(status + "\n")
	}

	// Middle row: the match list, with the scrollable attributes panel docked
	// on the right when it's open.
	left := m.renderList()
	if m.showSchema {
		left = lipgloss.NewStyle().Width(m.listWidth()).Render(left)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, m.renderAttrsPanel()))
	} else {
		b.WriteString(left)
	}
	b.WriteString("\n")

	// Selected-file detail panel (always at the bottom now).
	b.WriteString(panelStyle.Render(m.renderDetail()))

	b.WriteString("\n" + dimStyle.Render(m.footer()))
	return b.String()
}

// footer is the keybinding hint line; the navigate keys' meaning depends on
// whether the attributes panel currently has focus.
func (m model) footer() string {
	nav := "↑/↓ PgUp/PgDn navigate"
	if m.focus == focusAttrs {
		nav = "↑/↓ PgUp/PgDn scroll attrs"
	}
	if m.semantic {
		return "tab focus · enter (on query) search · " + nav + " · ctrl+a attrs · esc copy cmd & quit"
	}
	return nav + " · tab focus · ctrl+a attrs · enter/esc copy expr & quit"
}

// focusLabel renders a box label, marked with an accent arrow when focused.
func focusLabel(name string, focused bool) string {
	if focused {
		return accentStyle.Render("▸ " + name)
	}
	return dimStyle.Render("  " + name)
}

// semErrLine flattens a semantic-search error to one line and appends the
// usual "is Ollama up?" hint (mirrors search_cmd.go's footer warning).
func semErrLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s + "  (is Ollama running and the model pulled?)"
}

// renderList returns exactly listHeight lines (no trailing newline) so it can
// be joined horizontally with the attributes panel without misalignment.
func (m model) renderList() string {
	h := m.listHeight()
	w := m.listWidth()
	var lines []string
	switch {
	case len(m.matched) == 0 && m.loaded:
		lines = append(lines, dimStyle.Render("(no matches)"))
	case len(m.matched) > 0:
		pathW := w / 2
		for i := m.top; i < len(m.matched) && i < m.top+h; i++ {
			r := m.results[m.matched[i]]
			meta := r.Attrs.ContentType
			if r.Attrs.Size > 0 {
				meta += dimStyle.Render(fmt.Sprintf("  %s", humanSize(r.Attrs.Size)))
			}
			var prefix string
			if m.hasSimilarity {
				prefix = okStyle.Render(fmt.Sprintf("%.3f", r.Attrs.Similarity)) + " "
			}
			line := prefix + fmt.Sprintf("%-*s %s", max(20, pathW), truncate(r.Path, pathW), meta)
			if i == m.selected {
				line = selStyle.Render(truncate(line, w-1))
			} else {
				line = truncate(line, w-1)
			}
			lines = append(lines, line)
		}
	}
	// Pad / clamp to a stable height so neighbouring panels don't jump.
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// listWidth is the horizontal budget for the match list, shrunk to leave room
// for the attributes panel (and its 3 cols of margin/border/padding) when open.
func (m model) listWidth() int {
	if m.showSchema {
		w := max(m.width-m.attrsPanelWidth()-3, 10)
		return w
	}
	return m.width
}

// attrsPanelWidth is the inner content width of the right-hand attributes
// panel: roughly a third of the screen, clamped to a readable band and never
// so wide it starves the list.
func (m model) attrsPanelWidth() int {
	w := min(max(m.width/3, 24), 46)
	if cap := m.width - 24; w > cap {
		w = cap
	}
	if w < 0 {
		w = 0
	}
	return w
}

func (m model) renderDetail() string {
	if len(m.matched) == 0 || m.selected >= len(m.matched) {
		return dimStyle.Render("select a file to inspect its attributes")
	}
	a := m.results[m.matched[m.selected]].Attrs
	var b strings.Builder
	b.WriteString(accentStyle.Render(a.Path) + "\n")
	fmt.Fprintf(&b, "content_type=%s  size=%d", a.ContentType, a.Size)
	if !a.ModTime.IsZero() {
		fmt.Fprintf(&b, "  mod_time=%s", a.ModTime.Format(time.DateOnly))
	}
	// A few populated type-specific attributes from Extra.
	if len(a.Extra) > 0 {
		keys := make([]string, 0, len(a.Extra))
		for k := range a.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			if len(parts) >= 6 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s=%v", k, a.Extra[k]))
		}
		if len(parts) > 0 {
			b.WriteString("\n" + dimStyle.Render(strings.Join(parts, "  ")))
		}
	}
	return b.String()
}

// renderAttrsPanel draws the scrollable attributes viewport with a heading.
func (m model) renderAttrsPanel() string {
	scroll := "↑/↓ scroll"
	if m.focus != focusAttrs {
		scroll = "tab to scroll"
	}
	head := titleStyle.Render("attributes") + dimStyle.Render("  "+scroll)
	inner := head + "\n" + m.attrs.View()
	return attrsPanelStyle.Height(m.listHeight()).Render(inner)
}

// attrsContent builds the full attribute/function reference, one entry per line
// (with a wrapped description line), each pre-truncated to w so the viewport
// scrolls vertically without horizontal clipping. Styling is applied AFTER
// truncation so ANSI escapes are never cut mid-sequence.
func attrsContent(w int) string {
	s := celexpr.Schema()
	var b strings.Builder
	section := func(title string, docs []celexpr.AttributeDoc) {
		if len(docs) == 0 {
			return
		}
		b.WriteString(titleStyle.Render(title) + "\n")
		for _, a := range docs {
			b.WriteString(accentStyle.Render(truncate(a.Name+" ("+a.Type+")", w)) + "\n")
			if a.Description != "" {
				b.WriteString(dimStyle.Render(truncate("  "+a.Description, w)) + "\n")
			}
		}
		b.WriteString("\n")
	}
	section("COMMON", s.Common)
	section("TYPE-SPECIFIC", s.TypeSpecific)
	section("FRONTMATTER", s.Frontmatter)
	if len(s.Functions) > 0 {
		b.WriteString(titleStyle.Render("FUNCTIONS") + "\n")
		for _, f := range s.Functions {
			b.WriteString(accentStyle.Render(truncate(f.Signature, w)) + "\n")
			if f.Description != "" {
				b.WriteString(dimStyle.Render(truncate("  "+f.Description, w)) + "\n")
			}
		}
	}
	return b.String()
}

func humanSize(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	// Byte-trim is fine for the ASCII-ish paths/attrs we render.
	for len(s) > 0 && lipgloss.Width(s) > w-1 {
		s = s[:len(s)-1]
	}
	return s + "…"
}
