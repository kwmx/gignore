package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/diffview"
	"github.com/kwmx/gignore/internal/version"
)

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type layout struct {
	list, selected, preview rect
}

const (
	headerRows = 1
	footerRows = 2
	minWidth   = 50
	minHeight  = 12
)

func (m *model) layout() layout {
	bodyH := max(m.height-headerRows-footerRows, 6)
	leftW := max(30, m.width*38/100)
	if m.width-leftW < 30 {
		leftW = m.width / 2
	}
	rightW := m.width - leftW
	selH := min(max(len(m.selected), 1), 7) + 2
	selH = min(selH, bodyH/2)
	return layout{
		list:     rect{0, headerRows, leftW, bodyH},
		selected: rect{leftW, headerRows, rightW, selH},
		preview:  rect{leftW, headerRows + selH, rightW, bodyH - selH},
	}
}

func (m *model) listRows() int     { return max(m.layout().list.h-3, 1) } // border x2 + search line
func (m *model) previewRows() int  { return max(m.layout().preview.h-2, 1) }
func (m *model) selectedRows() int { return max(m.layout().selected.h-2, 1) }
func (m *model) reviewRows() int   { return max(m.height-12, 1) } // frame, title, path, summary, hints, spacing

func (m *model) selOffset() int {
	rows := m.selectedRows()
	return clamp(m.selCursor-rows+1, 0, max(len(m.selected)-rows, 0))
}

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = version.Name + " · " + m.opts.DisplayPath
	if m.eng.Cfg.TUI.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if m.width == 0 {
		return v
	}
	if m.width < minWidth || m.height < minHeight {
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("Terminal too small (%d×%d).\nResize to at least %d×%d, or press ctrl+c.", m.width, m.height, minWidth, minHeight)))
		return v
	}
	l := m.layout()
	base := lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		lipgloss.JoinHorizontal(lipgloss.Top,
			m.listView(l.list),
			lipgloss.JoinVertical(lipgloss.Left, m.selectedView(l.selected), m.previewView(l.preview)),
		),
		m.footerView(),
	)
	if m.overlay != overlayNone {
		modal := m.overlayView()
		mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
		base = lipgloss.NewCompositor(
			lipgloss.NewLayer(base),
			lipgloss.NewLayer(modal).X(max((m.width-mw)/2, 0)).Y(max((m.height-mh)/2, 0)).Z(1),
		).Render()
	}
	v.SetContent(clip(base, m.width, m.height))
	return v
}

// box draws a rounded border of exactly w×h with title set into the top edge.
func (m *model) box(r rect, title string, focused bool, lines []string) string {
	bc := m.pal.Border
	if focused {
		bc = m.pal.Accent
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	ts := lipgloss.NewStyle().Foreground(m.pal.Muted)
	if focused {
		ts = lipgloss.NewStyle().Foreground(m.pal.Accent).Bold(true)
	}
	inner := max(r.w-2, 0)
	t := ansi.Truncate(" "+title+" ", max(inner-2, 0), "…")
	top := bs.Render("╭─") + ts.Render(t) + bs.Render(strings.Repeat("─", max(inner-1-lipgloss.Width(t), 0))+"╮")
	var b strings.Builder
	b.WriteString(top)
	for i := 0; i < r.h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = ansi.Truncate(line, inner, "…")
		pad := inner - lipgloss.Width(line)
		b.WriteString("\n" + bs.Render("│") + line + strings.Repeat(" ", max(pad, 0)) + bs.Render("│"))
	}
	b.WriteString("\n" + bs.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return b.String()
}

func (m *model) headerView() string {
	name := lipgloss.NewStyle().Foreground(m.pal.Accent).Bold(true).Render(" " + version.Name)
	muted := lipgloss.NewStyle().Foreground(m.pal.Muted)
	target := m.opts.DisplayPath
	if m.opts.Global {
		target = "global ignore file · " + target
	}
	var online string
	switch m.online {
	case onlineLoading:
		online = "loading online templates…"
	case onlineReady:
		online = fmt.Sprintf("%d templates", m.eng.Catalog.Len())
		if m.onlineNote != "" {
			online += " (" + m.onlineNote + ")"
		}
	case onlineFailed:
		online = fmt.Sprintf("%d templates · offline", m.eng.Catalog.Len())
	case onlineOff:
		online = fmt.Sprintf("%d templates · offline mode", m.eng.Catalog.Len())
	}
	left := name + muted.Render("  "+target)
	right := muted.Render(online + " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) listView(r rect) string {
	p := m.pal
	inner := r.w - 2
	prompt := lipgloss.NewStyle().Foreground(p.Accent).Render("› ")
	q := m.query
	cursor := lipgloss.NewStyle().Reverse(true).Render(" ")
	if m.focus != focusList {
		cursor = ""
	}
	if q == "" && m.focus == focusList {
		q = cursor + lipgloss.NewStyle().Foreground(p.Subtle).Render("type to search")
	} else {
		q += cursor
	}
	modeTag := lipgloss.NewStyle().Foreground(p.Muted).Render(m.mode.String())
	search := prompt + q
	if gap := inner - lipgloss.Width(search) - lipgloss.Width(modeTag) - 1; gap > 0 {
		search += strings.Repeat(" ", gap) + modeTag
	}
	lines := []string{search}
	if m.searchErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(p.Error).Render(" "+m.searchErr))
	} else if len(m.items) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(p.Muted).Render(" No templates match. ctrl+r changes the search mode."))
	}
	rows := m.listRows()
	for i := m.offset; i < min(m.offset+rows, len(m.items)); i++ {
		lines = append(lines, m.itemLine(m.items[i], i == m.cursor, inner))
	}
	title := fmt.Sprintf("Templates %d", len(m.items))
	if len(m.items) > rows {
		title += fmt.Sprintf(" · %d%%", (m.offset+rows)*100/len(m.items))
	}
	return m.box(r, title, m.focus == focusList, lines)
}

func (m *model) itemLine(it item, current bool, width int) string {
	p := m.pal
	// Style every segment from one base, rather than wrapping already-styled
	// text, so escape sequences never nest or get cut.
	base := lipgloss.NewStyle()
	switch {
	case current && m.focus == focusList:
		base = base.Background(p.Border)
	case current:
		base = base.Underline(true)
	}
	sel := slices.Contains(m.selected, it.t.Key)
	mark := base.Foreground(p.Subtle).Render(" ○ ")
	nameStyle := base.Foreground(p.Text)
	if sel {
		mark = base.Foreground(p.Selected).Bold(true).Render(" ● ")
		nameStyle = nameStyle.Foreground(p.Selected).Bold(true)
	}
	hl := nameStyle.Foreground(p.Highlight).Bold(true).Underline(true)
	var name strings.Builder
	for i, r := range []rune(it.t.Key) {
		if slices.Contains(it.matched, i) {
			name.WriteString(hl.Render(string(r)))
		} else {
			name.WriteString(nameStyle.Render(string(r)))
		}
	}
	tagText, tagColor := m.tagFor(it.t)
	used := 3 + lipgloss.Width(it.t.Key)
	gap := width - used - lipgloss.Width(tagText) - 1
	if gap < 1 {
		tagText, gap = "", max(width-used, 0)
	}
	pad := base
	if current && m.focus != focusList {
		pad = lipgloss.NewStyle() // don't underline the padding
	}
	tail := pad.Render(strings.Repeat(" ", gap))
	if tagText != "" {
		tail += base.Foreground(tagColor).Render(tagText) + pad.Render(" ")
	}
	return mark + name.String() + tail
}

func (m *model) tagFor(t *catalog.Template) (string, color.Color) {
	p := m.pal
	switch {
	case m.detected[t.Key].Template != "":
		return "★ " + m.detected[t.Key].Kind, p.Add
	case m.recent[t.Key]:
		return "recent", p.Info
	case t.Source == catalog.SourceLocal:
		return "local", p.Warn
	case t.Source == catalog.SourceOnline:
		return "online", p.Subtle
	case t.Group == "global" || t.Group == "community":
		return t.Group, p.Subtle
	}
	return "", p.Subtle
}

func (m *model) selectedView(r rect) string {
	p := m.pal
	var lines []string
	if len(m.selected) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(p.Muted).Render(" Nothing selected. Press space on a template to add it."))
	}
	off := m.selOffset()
	for i := off; i < min(off+m.selectedRows(), len(m.selected)); i++ {
		k := m.selected[i]
		base := lipgloss.NewStyle()
		if i == m.selCursor && m.focus == focusSelected {
			base = base.Background(p.Border)
		}
		numText := fmt.Sprintf(" %d. ", i+1)
		labelColor, detailColor := p.Selected, p.Muted
		var detailText string
		if t, ok := m.eng.Catalog.Get(k); ok {
			detailText = fmt.Sprintf("  %d rules · %s", t.Rules(), t.Source)
		} else if m.online == onlineLoading {
			detailText = "  waiting for online templates…"
		} else {
			labelColor, detailColor, detailText = p.Error, p.Error, "  not available"
		}
		text := numText + k + detailText
		pad := max(r.w-2-lipgloss.Width(text), 0)
		lines = append(lines, base.Foreground(p.Subtle).Render(numText)+
			base.Foreground(labelColor).Render(k)+
			base.Foreground(detailColor).Render(detailText)+
			base.Render(strings.Repeat(" ", pad)))
	}
	title := fmt.Sprintf("Selected %d", len(m.selected))
	if m.focus == focusSelected {
		title += " · shift+↑↓ reorder · x remove"
	}
	return m.box(r, title, m.focus == focusSelected, lines)
}

func (m *model) previewView(r rect) string {
	p := m.pal
	var lines []string
	switch {
	case m.buildErr != nil:
		lines = []string{lipgloss.NewStyle().Foreground(p.Error).Render(" " + m.buildErr.Error())}
	case len(m.previewLn) == 0:
		lines = []string{lipgloss.NewStyle().Foreground(p.Muted).Render(" The generated rules appear here.")}
	default:
		end := min(m.pOffset+m.previewRows(), len(m.previewLn))
		for _, l := range m.previewLn[m.pOffset:end] {
			lines = append(lines, " "+l)
		}
	}
	title := "Preview"
	if len(m.previewLn) > 0 {
		title += fmt.Sprintf(" · %d lines", len(m.previewLn))
		if m.build.Dropped > 0 {
			title += fmt.Sprintf(" · %d duplicate%s removed", m.build.Dropped, plural(m.build.Dropped))
		}
	}
	switch {
	case m.planErr != nil:
		title += " · can't merge with existing file"
	case m.diff != "":
		a, d := diffview.Stats(m.diff)
		title += fmt.Sprintf(" · +%d −%d unsaved", a, d)
	case m.plan.Path != "":
		title += " · saved"
	}
	return m.box(r, title, m.focus == focusPreview, lines)
}

func (m *model) footerView() string {
	p := m.pal
	var status string
	if m.status != "" {
		c := map[statusKind]lipgloss.Style{
			statusInfo: lipgloss.NewStyle().Foreground(p.Info),
			statusOK:   lipgloss.NewStyle().Foreground(p.Add),
			statusWarn: lipgloss.NewStyle().Foreground(p.Warn),
			statusErr:  lipgloss.NewStyle().Foreground(p.Error),
		}[m.statusKind]
		status = c.Render(" " + m.status)
	}
	keyS := lipgloss.NewStyle().Foreground(p.Accent)
	descS := lipgloss.NewStyle().Foreground(p.Muted)
	hints := [][2]string{{"space", "toggle"}, {"tab", "pane"}, {"ctrl+s", "review & save"}, {"ctrl+a", "add detected"}, {"ctrl+r", "search mode"}, {"f1", "help"}, {"esc", "quit"}}
	var parts []string
	for _, h := range hints {
		parts = append(parts, keyS.Render(h[0])+" "+descS.Render(h[1]))
	}
	help := " " + strings.Join(parts, descS.Render(" · "))
	return ansi.Truncate(status, m.width, "…") + "\n" + ansi.Truncate(help, m.width, "…")
}

func (m *model) overlayView() string {
	p := m.pal
	frame := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Accent).Padding(1, 2)
	title := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(p.Muted)
	keyS := lipgloss.NewStyle().Foreground(p.Accent).Width(16)
	switch m.overlay {
	case overlayHelp:
		sections := []struct {
			name string
			keys [][2]string
		}{
			{"Templates pane", [][2]string{{"type", "search"}, {"↑ ↓ pgup pgdn", "move"}, {"space / enter", "add or remove"}, {"ctrl+u / ctrl+w", "clear / delete word"}, {"ctrl+r", "fuzzy, exact, regex"}}},
			{"Selected pane", [][2]string{{"↑ ↓", "move"}, {"shift+↑ shift+↓", "reorder"}, {"x / delete", "remove"}}},
			{"Preview pane", [][2]string{{"↑ ↓ pgup pgdn", "scroll"}, {"g / G", "top / bottom"}}},
			{"Anywhere", [][2]string{{"tab / shift+tab", "switch pane"}, {"ctrl+s / ctrl+d", "review and save"}, {"ctrl+a", "add detected"}, {"ctrl+x", "clear selection"}, {"ctrl+y", "copy block"}, {"esc", "clear search or quit"}, {"ctrl+c", "quit, don't save"}}},
		}
		render := func(from, to int) string {
			var b strings.Builder
			for i := from; i < to; i++ {
				if i > from {
					b.WriteString("\n")
				}
				b.WriteString(lipgloss.NewStyle().Bold(true).Render(sections[i].name) + "\n")
				for _, k := range sections[i].keys {
					b.WriteString(keyS.Render(k[0]) + " " + k[1] + "\n")
				}
			}
			return strings.TrimRight(b.String(), "\n")
		}
		var body string
		if m.width >= 100 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, render(0, 3), "    ", render(3, 4))
		} else {
			body = render(0, 4)
		}
		content := title.Render("Keyboard shortcuts") + "\n\n" + body + "\n\n" +
			muted.Render("Later templates win when rules conflict. Rules outside\nthe gignore block are always kept. Press any key to close.")
		return frame.Padding(0, 2).Render(content)
	case overlayQuit:
		body := title.Render("Quit without saving?") + "\n\n" +
			"Your selection differs from " + m.savePath + ".\n\n" +
			keyS.Render("s") + " save and quit\n" + keyS.Render("y / enter") + " quit without saving\n" + keyS.Render("any other key") + " keep editing"
		return frame.Render(body)
	case overlayReview:
		w := min(m.width-6, 110)
		rows := m.reviewRows()
		pathLine := lipgloss.NewStyle().Foreground(p.Accent).Render("Save to › ") + m.savePath + lipgloss.NewStyle().Reverse(true).Render(" ")
		var summary string
		switch {
		case m.planErr != nil:
			summary = lipgloss.NewStyle().Foreground(p.Error).Render("Can't merge with this file.")
		case m.diff == "":
			summary = muted.Render("Already up to date.")
		default:
			a, d := diffview.Stats(m.diff)
			verb := "Update"
			if !m.plan.Exists {
				verb = "Create"
			}
			summary = fmt.Sprintf("%s file: %s %s. Rules outside the gignore block are kept.", verb,
				lipgloss.NewStyle().Foreground(p.Add).Render(fmt.Sprintf("+%d", a)),
				lipgloss.NewStyle().Foreground(p.Remove).Render(fmt.Sprintf("−%d", d)))
		}
		end := min(m.rOffset+rows, len(m.reviewLn))
		var body []string
		for _, l := range m.reviewLn[m.rOffset:end] {
			body = append(body, ansi.Truncate(l, w-6, "…"))
		}
		for len(body) < min(rows, len(m.reviewLn)) {
			body = append(body, "")
		}
		content := title.Render("Review changes") + "\n\n" + pathLine + "\n" + summary + "\n\n" +
			strings.Join(body, "\n") + "\n\n" +
			muted.Render("enter save · ↑↓ pgup pgdn scroll · type to edit the path · esc back")
		return frame.Padding(0, 2).Width(w).Render(content)
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// clip trims s to at most w columns and h rows, so an oversized overlay never
// pushes the layout off screen.
func clip(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}
