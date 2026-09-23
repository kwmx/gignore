// Package compose joins templates into one block of ignore rules.
package compose

import (
	"strings"

	"github.com/kwmx/gignore/internal/catalog"
)

// Options controls how templates are combined.
type Options struct {
	// Dedupe drops a rule already emitted by an earlier template, as long as no
	// negation ("!pattern") was emitted in between. That condition keeps the
	// result equivalent: a later duplicate only matters when it re-ignores a path
	// that a negation un-ignored.
	Dedupe bool
}

// Result is the composed text plus what was removed.
type Result struct {
	Text    string
	Dropped int // duplicate rules removed by Dedupe
}

// SectionPrefix starts the header line gignore writes above each template.
const SectionPrefix = "### "

// Compose renders templates in order, each under a "### Name ###" header.
func Compose(ts []*catalog.Template, o Options) Result {
	var b strings.Builder
	firstSeen := map[string]int{} // rule -> index of the negation counter when first emitted
	negations := 0
	dropped := 0
	for i, t := range ts {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(SectionPrefix + t.Name + " ###\n")
		lines := strings.Split(normalize(t.Content), "\n")
		lines = trimBlank(lines)
		prevBlank := false
		for _, line := range lines {
			rule := strings.TrimRight(line, " \t")
			isRule := rule != "" && !strings.HasPrefix(rule, "#")
			if isRule && o.Dedupe {
				if n, ok := firstSeen[rule]; ok && n == negations {
					dropped++
					continue
				}
				firstSeen[rule] = negations
			}
			if isRule && strings.HasPrefix(rule, "!") {
				negations++
			}
			blank := strings.TrimSpace(line) == ""
			if blank && prevBlank {
				continue
			}
			prevBlank = blank
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return Result{Text: b.String(), Dropped: dropped}
}

// SectionAt returns the template name whose section contains line (0-based) of
// text, or "" when the line precedes every section.
func SectionAt(text string, line int) string {
	lines := strings.Split(text, "\n")
	for i := min(line, len(lines)-1); i >= 0; i-- {
		if name, ok := ParseSection(lines[i]); ok {
			return name
		}
	}
	return ""
}

// ParseSection reports whether line is a section header written by Compose.
func ParseSection(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, SectionPrefix) || !strings.HasSuffix(line, " ###") || len(line) < 9 {
		return "", false
	}
	return strings.TrimSpace(line[4 : len(line)-4]), true
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
