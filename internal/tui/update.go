package tui

import (
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/kwmx/gignore/internal/ui"
)

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampList()
		m.clampPreview()
		return m, nil

	case tea.BackgroundColorMsg:
		m.pal = ui.PaletteFor("auto", msg.IsDark())
		m.recompute()
		return m, nil

	case onlineMsg:
		m.eng.SetOnline(msg.res, msg.err)
		switch {
		case msg.err != nil && (m.opts.Offline || !m.eng.Cfg.Online.Enabled):
			m.online = onlineOff
		case msg.err != nil:
			m.online = onlineFailed
			m.onlineNote = msg.err.Error()
		default:
			m.online = onlineReady
			if msg.res.Stale {
				m.onlineNote = "cached copy; refresh failed"
			}
		}
		m.refreshItems()
		m.recompute()
		if m.online == onlineFailed && len(m.missing) > 0 {
			return m, m.setStatus(statusErr, "Online templates unavailable: "+m.onlineNote)
		}
		return m, nil

	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
		return m, nil

	case tea.PasteMsg:
		switch {
		case m.overlay == overlayReview:
			m.savePath += strings.TrimSpace(msg.Content)
			m.recompute()
			m.buildReview()
		case m.overlay == overlayNone && m.focus == focusList:
			m.query += strings.TrimSpace(msg.Content)
			m.refreshItems()
		}
		return m, nil

	case tea.MouseWheelMsg:
		return m, m.onWheel(msg)

	case tea.MouseClickMsg:
		return m, m.onClick(msg)

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.overlay {
	case overlayHelp:
		m.overlay = overlayNone
		return m, nil
	case overlayQuit:
		switch key {
		case "y", "enter":
			return m, tea.Quit
		case "s":
			m.overlay = overlayNone
			if cmd := m.save(); m.result.Saved != "" && !m.dirty() {
				return m, tea.Sequence(cmd, tea.Quit)
			} else {
				return m, cmd
			}
		default:
			m.overlay = overlayNone
		}
		return m, nil
	case overlayReview:
		return m, m.onReviewKey(k)
	}

	// Keys that work in every pane.
	switch key {
	case "tab":
		m.focus = (m.focus + 1) % 3
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return m, nil
	case "ctrl+s":
		return m, m.openReview()
	case "ctrl+d":
		return m, m.openReview()
	case "ctrl+r":
		m.mode = m.mode.Next()
		m.refreshItems()
		return m, m.setStatus(statusInfo, "Search mode: "+m.mode.String())
	case "ctrl+x":
		if len(m.selected) == 0 {
			return m, nil
		}
		m.selected = nil
		m.touched = true
		m.recompute()
		return m, m.setStatus(statusInfo, "Cleared the selection")
	case "ctrl+a":
		return m, m.addDetected()
	case "ctrl+y":
		if m.build.Block == "" {
			return m, m.setStatus(statusWarn, "Nothing to copy yet")
		}
		return m, tea.Batch(tea.SetClipboard(m.build.Block), m.setStatus(statusOK, "Copied the generated block to the clipboard"))
	case "f1":
		m.overlay = overlayHelp
		return m, nil
	case "esc":
		if m.focus == focusList && m.query != "" {
			m.query = ""
			m.refreshItems()
			return m, nil
		}
		return m.quit()
	}

	switch m.focus {
	case focusList:
		return m, m.onListKey(k)
	case focusSelected:
		return m.onSelectedKey(k)
	default:
		return m.onPreviewKey(k)
	}
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	if m.dirty() {
		m.overlay = overlayQuit
		return m, nil
	}
	return m, tea.Quit
}

func (m *model) onListKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "up", "ctrl+p":
		m.moveCursor(-1)
	case "down", "ctrl+n":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.listRows())
	case "pgdown":
		m.moveCursor(m.listRows())
	case "home":
		m.moveCursor(-len(m.items))
	case "end":
		m.moveCursor(len(m.items))
	case "space", "enter":
		if m.cursor < len(m.items) {
			m.toggle(m.items[m.cursor].t.Key)
			if k.String() == "enter" && m.query != "" {
				// After picking from a search, start the next search fresh.
				m.query = ""
				m.refreshItems()
			}
		}
	case "backspace":
		if m.query != "" {
			_, size := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-size]
			m.refreshItems()
		}
	case "ctrl+u":
		m.query = ""
		m.refreshItems()
	case "ctrl+w", "alt+backspace":
		q := strings.TrimRight(m.query, " ")
		if i := strings.LastIndexAny(q, " -_.+"); i >= 0 {
			m.query = q[:i]
		} else {
			m.query = ""
		}
		m.refreshItems()
	case "?":
		if m.query == "" {
			m.overlay = overlayHelp
			return nil
		}
		m.query += "?"
		m.refreshItems()
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			m.query += k.Text
			m.refreshItems()
		}
	}
	return nil
}

func (m *model) onSelectedKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.selected)
	switch k.String() {
	case "up", "k":
		m.selCursor = max(m.selCursor-1, 0)
	case "down", "j":
		m.selCursor = min(m.selCursor+1, max(n-1, 0))
	case "shift+up", "K", "alt+up":
		if m.selCursor > 0 && n > 0 {
			m.selected[m.selCursor], m.selected[m.selCursor-1] = m.selected[m.selCursor-1], m.selected[m.selCursor]
			m.selCursor--
			m.touched = true
			m.recompute()
		}
	case "shift+down", "J", "alt+down":
		if m.selCursor < n-1 {
			m.selected[m.selCursor], m.selected[m.selCursor+1] = m.selected[m.selCursor+1], m.selected[m.selCursor]
			m.selCursor++
			m.touched = true
			m.recompute()
		}
	case "space", "enter", "x", "d", "delete", "backspace":
		if n > 0 {
			m.toggle(m.selected[m.selCursor])
		}
	case "q":
		return m.quit()
	case "/":
		m.focus = focusList
	}
	return m, nil
}

func (m *model) onPreviewKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h := m.previewRows()
	switch k.String() {
	case "up", "k":
		m.pOffset--
	case "down", "j":
		m.pOffset++
	case "pgup", "b":
		m.pOffset -= h
	case "pgdown", "f", "space":
		m.pOffset += h
	case "ctrl+u":
		m.pOffset -= h / 2
	case "home", "g":
		m.pOffset = 0
	case "end", "G":
		m.pOffset = len(m.previewLn)
	case "q":
		return m.quit()
	case "/":
		m.focus = focusList
	}
	m.clampPreview()
	return m, nil
}

func (m *model) addDetected() tea.Cmd {
	var added []string
	for _, k := range m.orderedDetected() {
		if !slices.Contains(m.selected, k) {
			m.selected = append(m.selected, k)
			added = append(added, k)
		}
	}
	if len(added) == 0 {
		if len(m.detected) == 0 {
			return m.setStatus(statusInfo, "Detection found no known project files")
		}
		return m.setStatus(statusInfo, "All detected templates are already selected")
	}
	m.touched = true
	m.recompute()
	return m.setStatus(statusOK, "Added detected: "+strings.Join(added, ", "))
}

// Review overlay: shows the diff and lets the user edit the path before saving.

func (m *model) openReview() tea.Cmd {
	if len(m.selected) == 0 {
		return m.setStatus(statusWarn, "Select at least one template first")
	}
	m.overlay = overlayReview
	m.rOffset = 0
	m.buildReview()
	return nil
}

func (m *model) buildReview() {
	switch {
	case m.planErr != nil:
		m.reviewLn = []string{m.planErr.Error()}
	case m.diff == "":
		m.reviewLn = []string{"No changes: the file already matches the selection."}
	default:
		m.reviewLn = strings.Split(ui.ColorizeDiff(m.pal, m.diff), "\n")
	}
}

func (m *model) onReviewKey(k tea.KeyPressMsg) tea.Cmd {
	rows := m.reviewRows()
	switch k.String() {
	case "esc":
		m.overlay = overlayNone
		return nil
	case "enter", "ctrl+s":
		return m.save()
	case "up":
		m.rOffset--
	case "down":
		m.rOffset++
	case "pgup":
		m.rOffset -= rows
	case "pgdown":
		m.rOffset += rows
	case "backspace":
		if m.savePath != "" {
			_, size := utf8.DecodeLastRuneInString(m.savePath)
			m.savePath = m.savePath[:len(m.savePath)-size]
			m.recompute()
			m.buildReview()
		}
	case "ctrl+u":
		m.savePath = ""
		m.recompute()
		m.buildReview()
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			m.savePath += k.Text
			m.recompute()
			m.buildReview()
		}
	}
	m.rOffset = clamp(m.rOffset, 0, max(len(m.reviewLn)-rows, 0))
	return nil
}

// Mouse.

func (m *model) onWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if !m.eng.Cfg.TUI.Mouse {
		return nil
	}
	mo := msg.Mouse()
	delta := 3
	if mo.Button == tea.MouseWheelUp {
		delta = -3
	} else if mo.Button != tea.MouseWheelDown {
		return nil
	}
	if m.overlay == overlayReview {
		m.rOffset = clamp(m.rOffset+delta, 0, max(len(m.reviewLn)-m.reviewRows(), 0))
		return nil
	}
	l := m.layout()
	switch {
	case l.list.contains(mo.X, mo.Y):
		m.offset = clamp(m.offset+delta, 0, max(len(m.items)-m.listRows(), 0))
		m.cursor = clamp(m.cursor, m.offset, m.offset+m.listRows()-1)
	case l.preview.contains(mo.X, mo.Y):
		m.pOffset += delta
		m.clampPreview()
	}
	return nil
}

func (m *model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	if !m.eng.Cfg.TUI.Mouse || msg.Button != tea.MouseLeft {
		return nil
	}
	if m.overlay != overlayNone {
		if m.overlay == overlayHelp {
			m.overlay = overlayNone
		}
		return nil
	}
	mo := msg.Mouse()
	l := m.layout()
	switch {
	case l.list.contains(mo.X, mo.Y):
		m.focus = focusList
		row := mo.Y - l.list.y - 2 // border + search line
		if row >= 0 {
			i := m.offset + row
			if i < len(m.items) {
				m.cursor = i
				m.toggle(m.items[i].t.Key)
			}
		}
	case l.selected.contains(mo.X, mo.Y):
		m.focus = focusSelected
		row := mo.Y - l.selected.y - 1
		if i := m.selOffset() + row; row >= 0 && i < len(m.selected) {
			m.selCursor = i
		}
	case l.preview.contains(mo.X, mo.Y):
		m.focus = focusPreview
	}
	return nil
}

// Scrolling helpers.

func (m *model) moveCursor(d int) {
	m.cursor += d
	m.clampList()
}

func (m *model) clampList() {
	m.cursor = clamp(m.cursor, 0, max(len(m.items)-1, 0))
	rows := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = clamp(m.offset, 0, max(len(m.items)-rows, 0))
}

func (m *model) clampPreview() {
	m.pOffset = clamp(m.pOffset, 0, max(len(m.previewLn)-m.previewRows(), 0))
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return max(lo, min(v, hi))
}
