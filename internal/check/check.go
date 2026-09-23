// Package check finds problems in a .gitignore file.
package check

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
)

// Severity ranks findings.
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	}
	return "info"
}

// MarshalText makes Severity readable in JSON output.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding is one problem.
type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Line     int      `json:"line,omitempty"` // 1-based; 0 when not tied to a line
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
}

// Input is everything the checks need. Fields left empty skip their checks.
type Input struct {
	Content        string
	Regenerated    func(keys []string) (string, error) // current body for keys, to detect stale blocks
	Catalog        *catalog.Catalog
	TrackedIgnored []string // from git ls-files; nil when unknown
	Detected       []string // template keys detection suggests
}

var escapeRe = regexp.MustCompile(`\\[A-Za-z0-9]`)

// Run checks in.Content and returns findings sorted by line.
func Run(in Input) []Finding {
	var out []Finding
	add := func(sev Severity, code string, line int, msg, fix string) {
		out = append(out, Finding{Severity: sev, Code: code, Line: line, Message: msg, Fix: fix})
	}
	doc, err := block.Parse(in.Content)
	if err != nil {
		add(Error, "malformed-block", 0, err.Error(), "Delete the stray start marker, or rerun gignore generate with --replace.")
	}
	lines := strings.Split(strings.ReplaceAll(in.Content, "\r\n", "\n"), "\n")
	blockStart, blockEnd := -1, -1
	if doc.Found {
		blockStart = doc.BodyLineOffset() - 2
		blockEnd = doc.BodyLineOffset() + strings.Count(doc.Body, "\n") + 1
	}
	inBlock := func(i int) bool { return i >= blockStart && i <= blockEnd && blockStart >= 0 }

	// Managed block: unknown templates and stale content.
	if doc.Found && in.Catalog != nil {
		var unknown []string
		for _, k := range doc.Templates {
			if _, ok := in.Catalog.Get(k); !ok {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			add(Error, "unknown-template", blockStart+1,
				fmt.Sprintf("block lists templates that no source provides: %s", strings.Join(unknown, ", ")),
				"Enable online templates, add a local template with that name, or remove it with gignore remove.")
		} else if in.Regenerated != nil {
			if body, err := in.Regenerated(doc.Templates); err == nil && strings.TrimRight(body, "\n") != strings.TrimRight(doc.Body, "\n") {
				add(Warning, "stale-block", blockStart+1, "generated block differs from the current templates", "Run gignore update.")
			}
		}
	}

	// Rule-level checks.
	seenUser := map[string]int{}
	blockRules := map[string]bool{}
	type exclusion struct {
		dir  string
		line int
	}
	var dirExcludes []exclusion
	for i, raw := range lines {
		rule := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(rule)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if inBlock(i) {
			blockRules[t] = true
		}
		ln := i + 1
		if !inBlock(i) {
			if strings.HasSuffix(rule, " ") && !strings.HasSuffix(rule, "\\ ") {
				add(Warning, "trailing-space", ln, fmt.Sprintf("rule %q ends with a space, which git treats as part of the pattern", rule),
					"Remove the trailing space, or escape it as \"\\ \" if it is intended.")
			}
			if escapeRe.MatchString(t) && !strings.Contains(t, "/") {
				add(Warning, "backslash", ln, fmt.Sprintf("rule %q uses a backslash; .gitignore paths use forward slashes", t),
					"Replace \\ with /.")
			}
			if first, ok := seenUser[t]; ok {
				add(Info, "duplicate", ln, fmt.Sprintf("rule %q repeats line %d", t, first), "Delete one of them.")
			} else {
				seenUser[t] = ln
			}
			if blockRules[t] && !strings.HasPrefix(t, "!") {
				add(Info, "redundant", ln, fmt.Sprintf("rule %q is already in the generated block", t), "Delete it.")
			}
		}
		if strings.HasPrefix(t, "!") {
			if inBlock(i) {
				continue // template authors' problem, not something the user can fix here
			}
			neg := strings.TrimPrefix(strings.TrimPrefix(t, "!"), "/")
			for _, ex := range dirExcludes {
				if strings.HasPrefix(neg, ex.dir+"/") {
					add(Warning, "unreachable-negation", ln,
						fmt.Sprintf("%q can't re-include anything: line %d ignores the whole %s/ directory, and git never looks inside ignored directories", t, ex.line, ex.dir),
						fmt.Sprintf("Change line %d to %s/* (or %s/**) so the directory itself isn't ignored.", ex.line, ex.dir, ex.dir))
					break
				}
			}
			continue
		}
		// A pattern like "build/" or "/build" ignores the directory itself.
		d := strings.TrimSuffix(strings.TrimPrefix(t, "/"), "/")
		if d != "" && !strings.ContainsAny(d, "*?[") && (strings.HasSuffix(t, "/") || !strings.Contains(path.Base(d), ".")) {
			dirExcludes = append(dirExcludes, exclusion{dir: d, line: ln})
		}
	}

	if n := len(in.TrackedIgnored); n > 0 {
		shown := in.TrackedIgnored
		if len(shown) > 5 {
			shown = shown[:5]
		}
		more := ""
		if n > 5 {
			more = fmt.Sprintf(" and %d more", n-5)
		}
		add(Warning, "tracked-ignored", 0,
			fmt.Sprintf("%d tracked file(s) match ignore rules and stay in the repository: %s%s", n, strings.Join(shown, ", "), more),
			"Run gignore untrack to remove them from the index (files stay on disk).")
	}

	if len(in.Detected) > 0 && doc.Found {
		have := map[string]bool{}
		for _, k := range doc.Templates {
			if t, ok := in.Catalog.Get(k); ok {
				have[t.Key] = true
			}
		}
		var missing []string
		for _, k := range in.Detected {
			if !have[k] {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			add(Info, "missing-template", 0,
				fmt.Sprintf("project looks like it also uses: %s", strings.Join(missing, ", ")),
				"gignore add "+strings.Join(missing, " "))
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			// Findings without a line go last.
			if out[i].Line == 0 || out[j].Line == 0 {
				return out[j].Line == 0 && out[i].Line != 0
			}
			return out[i].Line < out[j].Line
		}
		return out[i].Severity > out[j].Severity
	})
	return out
}

// Worst returns the highest severity in fs, or -1 when fs is empty.
func Worst(fs []Finding) Severity {
	w := Severity(-1)
	for _, f := range fs {
		w = max(w, f.Severity)
	}
	return w
}
