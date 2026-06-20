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
)

// chromeRows is the number of non-list rows (title, input box, status,
// footer, detail panel) so listHeight can give the rest to the match list.
const chromeRows = 13

func (m model) listHeight() int {
	h := m.height - chromeRows
	if h < 1 {
		return 1
	}
	return h
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("CEL playground") + dimStyle.Render("  —  live filter, type a CEL expression") + "\n")
	b.WriteString(boxStyle.Width(min(m.width-2, 100)).Render(m.input.View()) + "\n")

	// Status / error line.
	switch {
	case !m.loaded:
		b.WriteString(dimStyle.Render("scanning…") + "\n")
	case m.loadErr != nil:
		b.WriteString(errStyle.Render("scan error: "+m.loadErr.Error()) + "\n")
	case m.errMsg != "":
		b.WriteString(errStyle.Render("✗ "+m.errMsg) + "\n")
	default:
		status := okStyle.Render(fmt.Sprintf("%d", len(m.matched))) + dimStyle.Render(fmt.Sprintf("/%d match", len(m.results)))
		if m.capped {
			status += dimStyle.Render(fmt.Sprintf("  (first %d shown — narrow with -d / --limit)", len(m.results)))
		}
		b.WriteString(status + "\n")
	}

	// Match list (windowed).
	b.WriteString(m.renderList())

	// Detail / schema panel.
	if m.showSchema {
		b.WriteString(panelStyle.Render(m.renderSchema()))
	} else {
		b.WriteString(panelStyle.Render(m.renderDetail()))
	}

	b.WriteString("\n" + dimStyle.Render("↑/↓ PgUp/PgDn navigate · tab attributes · enter/esc copy expr & quit"))
	return b.String()
}

func (m model) renderList() string {
	h := m.listHeight()
	if len(m.matched) == 0 {
		if m.loaded {
			return dimStyle.Render("(no matches)") + strings.Repeat("\n", h)
		}
		return strings.Repeat("\n", h)
	}
	var lines []string
	for i := m.top; i < len(m.matched) && i < m.top+h; i++ {
		r := m.results[m.matched[i]]
		meta := r.Attrs.ContentType
		if r.Attrs.Size > 0 {
			meta += dimStyle.Render(fmt.Sprintf("  %s", humanSize(r.Attrs.Size)))
		}
		line := fmt.Sprintf("%-*s %s", max(20, m.width/2), truncate(r.Path, m.width/2), meta)
		if i == m.selected {
			line = selStyle.Render(truncate(line, m.width-1))
		} else {
			line = truncate(line, m.width-1)
		}
		lines = append(lines, line)
	}
	// Pad to a stable height so the panel below doesn't jump.
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
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

func (m model) renderSchema() string {
	s := celexpr.Schema()
	var names []string
	for _, a := range s.Common {
		names = append(names, a.Name)
	}
	for _, a := range s.TypeSpecific {
		names = append(names, a.Name)
	}
	line := strings.Join(names, " · ")
	return titleStyle.Render("attributes") + "\n" + dimStyle.Render(truncate(line, (m.width-2)*4))
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
