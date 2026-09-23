// Package ui holds terminal styling shared by the CLI and the TUI.
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette is a set of semantic colors.
type Palette struct {
	Accent, Subtle, Muted, Text, Add, Remove, Warn, Error, Info, Selected, Border, Highlight color.Color
}

// Dark and Light are tuned for dark and light terminal backgrounds.
var (
	Dark = Palette{
		Accent: lipgloss.Color("#7AA2F7"), Subtle: lipgloss.Color("#565F89"), Muted: lipgloss.Color("#737AA2"),
		Text: lipgloss.Color("#C0CAF5"), Add: lipgloss.Color("#9ECE6A"), Remove: lipgloss.Color("#F7768E"),
		Warn: lipgloss.Color("#E0AF68"), Error: lipgloss.Color("#F7768E"), Info: lipgloss.Color("#7DCFFF"),
		Selected: lipgloss.Color("#BB9AF7"), Border: lipgloss.Color("#3B4261"), Highlight: lipgloss.Color("#FF9E64"),
	}
	Light = Palette{
		Accent: lipgloss.Color("#2E5CB8"), Subtle: lipgloss.Color("#8C8FA1"), Muted: lipgloss.Color("#6C6F85"),
		Text: lipgloss.Color("#1F2335"), Add: lipgloss.Color("#40741B"), Remove: lipgloss.Color("#C0392B"),
		Warn: lipgloss.Color("#8C5A00"), Error: lipgloss.Color("#C0392B"), Info: lipgloss.Color("#0F6A8B"),
		Selected: lipgloss.Color("#7847BD"), Border: lipgloss.Color("#B4B7C9"), Highlight: lipgloss.Color("#B35900"),
	}
	// ANSI uses the terminal's own 16 colors, so it matches any theme without
	// querying the terminal. The CLI uses it.
	ANSI = Palette{
		Accent: lipgloss.Color("4"), Subtle: lipgloss.Color("8"), Muted: lipgloss.Color("8"), Text: lipgloss.NoColor{},
		Add: lipgloss.Color("2"), Remove: lipgloss.Color("1"), Warn: lipgloss.Color("3"), Error: lipgloss.Color("1"),
		Info: lipgloss.Color("6"), Selected: lipgloss.Color("5"), Border: lipgloss.Color("8"), Highlight: lipgloss.Color("3"),
	}
	// Mono uses no color, only the terminal default, for NO_COLOR-style setups.
	Mono = Palette{
		Accent: lipgloss.NoColor{}, Subtle: lipgloss.NoColor{}, Muted: lipgloss.NoColor{}, Text: lipgloss.NoColor{},
		Add: lipgloss.NoColor{}, Remove: lipgloss.NoColor{}, Warn: lipgloss.NoColor{}, Error: lipgloss.NoColor{},
		Info: lipgloss.NoColor{}, Selected: lipgloss.NoColor{}, Border: lipgloss.NoColor{}, Highlight: lipgloss.NoColor{},
	}
)

// PaletteFor picks a palette from a theme name and the detected background.
func PaletteFor(theme string, darkBackground bool) Palette {
	switch theme {
	case "dark":
		return Dark
	case "light":
		return Light
	case "mono":
		return Mono
	}
	if darkBackground {
		return Dark
	}
	return Light
}

// ColorizeDiff styles a unified diff line by line.
func ColorizeDiff(p Palette, diff string) string {
	add := lipgloss.NewStyle().Foreground(p.Add)
	del := lipgloss.NewStyle().Foreground(p.Remove)
	hunk := lipgloss.NewStyle().Foreground(p.Info)
	head := lipgloss.NewStyle().Bold(true)
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			lines[i] = head.Render(l)
		case strings.HasPrefix(l, "@@"):
			lines[i] = hunk.Render(l)
		case strings.HasPrefix(l, "+"):
			lines[i] = add.Render(l)
		case strings.HasPrefix(l, "-"):
			lines[i] = del.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// HighlightGitignore colors comments, section headers, negations, and
// directory rules in .gitignore text.
func HighlightGitignore(p Palette, text string) string {
	comment := lipgloss.NewStyle().Foreground(p.Subtle)
	section := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	neg := lipgloss.NewStyle().Foreground(p.Add)
	dir := lipgloss.NewStyle().Foreground(p.Info)
	glob := lipgloss.NewStyle().Foreground(p.Text)
	marker := lipgloss.NewStyle().Foreground(p.Selected).Bold(true)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "# >>> gignore") || strings.HasPrefix(t, "# <<< gignore"):
			lines[i] = marker.Render(l)
		case strings.HasPrefix(t, "### ") && strings.HasSuffix(t, " ###"):
			lines[i] = section.Render(l)
		case strings.HasPrefix(t, "#"):
			lines[i] = comment.Render(l)
		case strings.HasPrefix(t, "!"):
			lines[i] = neg.Render(l)
		case strings.HasSuffix(t, "/"):
			lines[i] = dir.Render(l)
		case t != "":
			lines[i] = glob.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}
