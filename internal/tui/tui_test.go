package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kwmx/gignore/internal/config"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/ui"
)

func newTestModel(t *testing.T, files map[string]string, initial ...string) (*model, string) {
	t.Helper()
	t.Setenv("GIGNORE_CONFIG_DIR", t.TempDir())
	t.Setenv("GIGNORE_CACHE_DIR", t.TempDir())
	t.Setenv("GIGNORE_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Online.Enabled = false
	cfg.TUI.Theme = "mono"
	cfg.TUI.ShowOS = false
	e, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, ".gitignore")
	m := newModel(context.Background(), Options{Engine: e, Target: target, DisplayPath: ".gitignore", ProjectRoot: dir, Initial: initial, Offline: true})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(onlineMsg{err: errOffline{}})
	return m, target
}

type errOffline struct{}

func (errOffline) Error() string { return "offline" }

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: ' ', Text: " "}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "ctrl+s", "ctrl+x", "ctrl+a", "ctrl+r":
			msg = tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
		case "f1":
			msg = tea.KeyPressMsg{Code: tea.KeyF1}
		default:
			for _, r := range k {
				m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			continue
		}
		m.Update(msg)
	}
}

func screen(m *model) string { return ansi.Strip(m.View().Content) }

func TestDetectionPreselects(t *testing.T) {
	m, _ := newTestModel(t, map[string]string{"go.mod": "module x\n", "package.json": "{}"})
	if got := strings.Join(m.selected, ","); got != "go,node" {
		t.Fatalf("selected = %q, want go,node", got)
	}
	s := screen(m)
	for _, want := range []string{"Templates", "Selected 2", "### Go ###", "★ project"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen missing %q\n%s", want, s)
		}
	}
	if os.Getenv("GIGNORE_SHOW_SCREEN") != "" {
		t.Log("\n" + s)
	}
}

func TestSearchToggleAndSave(t *testing.T) {
	m, target := newTestModel(t, map[string]string{".gitignore": "keep-me.txt\n"})
	if len(m.selected) != 0 {
		t.Fatalf("expected empty selection, got %v", m.selected)
	}
	press(m, "rust")
	if m.items[0].t.Key != "rust" {
		t.Fatalf("top search hit = %q, want rust", m.items[0].t.Key)
	}
	press(m, "enter")
	if m.query != "" || len(m.selected) != 1 {
		t.Fatalf("after enter: query=%q selected=%v", m.query, m.selected)
	}
	press(m, "ctrl+s")
	if m.overlay != overlayReview {
		t.Fatal("ctrl+s should open the review overlay")
	}
	s := screen(m)
	if n := len(strings.Split(s, "\n")); n != 30 {
		t.Errorf("review screen has %d lines, want 30", n)
	}
	if !strings.Contains(s, "enter save") {
		t.Errorf("review overlay footer is cut off:\n%s", s)
	}
	if !strings.Contains(s, "Review changes") || !strings.Contains(s, "+### Rust ###") {
		t.Fatalf("review overlay missing diff:\n%s", s)
	}
	if os.Getenv("GIGNORE_SHOW_SCREEN") != "" {
		t.Log("\n" + s)
	}
	press(m, "enter")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# >>> gignore start: rust") || !strings.Contains(string(b), "keep-me.txt") {
		t.Fatalf("saved file wrong:\n%s", b)
	}
	if m.dirty() || !strings.Contains(m.result.Saved, "Updated .gitignore") {
		t.Fatalf("after save: dirty=%v result=%q", m.dirty(), m.result.Saved)
	}
}

func TestLoadsExistingBlockAndReorders(t *testing.T) {
	m, _ := newTestModel(t, map[string]string{".gitignore": "# >>> gignore start: go, rust\n# x\n# <<< gignore end\n"})
	if strings.Join(m.selected, ",") != "go,rust" {
		t.Fatalf("selected = %v", m.selected)
	}
	press(m, "tab", "down")
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	if strings.Join(m.selected, ",") != "rust,go" {
		t.Fatalf("after reorder = %v", m.selected)
	}
	if !m.dirty() {
		t.Fatal("reorder should make the model dirty")
	}
	press(m, "esc")
	if m.overlay != overlayQuit {
		t.Fatal("esc with unsaved changes should ask to confirm")
	}
}

func TestSmallTerminalAndHelp(t *testing.T) {
	m, _ := newTestModel(t, nil)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(screen(m), "Terminal too small") {
		t.Fatal("expected the small-terminal message")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, "f1")
	if !strings.Contains(screen(m), "Keyboard shortcuts") {
		t.Fatal("F1 should open help")
	}
	press(m, "x")
	if m.overlay != overlayNone {
		t.Fatal("any key should close help")
	}
}

func TestNoBrokenEscapes(t *testing.T) {
	m, _ := newTestModel(t, map[string]string{"go.mod": "module x\n"})
	m.eng.Cfg.TUI.Theme = "dark"
	m.pal = paletteForTest()
	m.recompute()
	for _, f := range []focus{focusList, focusSelected, focusPreview} {
		m.focus = f
		if s := screen(m); strings.Contains(s, "[1;") || strings.Contains(s, "[m") || strings.Contains(s, "\x1b") {
			t.Fatalf("focus %d: escape sequence leaked into the text:\n%s", f, s)
		}
	}
}

func TestLinesFitWidth(t *testing.T) {
	m, _ := newTestModel(t, map[string]string{"go.mod": "module x\n"})
	for _, size := range [][2]int{{50, 12}, {80, 24}, {120, 40}, {200, 60}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(screen(m), "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: got %d lines", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
	}
}

func TestOverlaysFitScreen(t *testing.T) {
	m, _ := newTestModel(t, map[string]string{"go.mod": "module x\n"})
	for _, size := range [][2]int{{50, 12}, {80, 24}, {120, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, ov := range []overlay{overlayHelp, overlayReview, overlayQuit} {
			m.overlay = ov
			m.buildReview()
			lines := strings.Split(screen(m), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d overlay %d: got %d lines", size[0], size[1], ov, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d overlay %d: line %d is %d wide", size[0], size[1], ov, i, w)
				}
			}
		}
	}
}

func paletteForTest() ui.Palette { return ui.Dark }
