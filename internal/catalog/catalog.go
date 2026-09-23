// Package catalog merges templates from every source into one lookup table.
package catalog

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/kwmx/gignore/internal/catalog/builtin"
)

// Source identifies where a template came from.
type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceOnline  Source = "gitignore.io"
	SourceLocal   Source = "local"
)

// Template is a named set of ignore rules.
type Template struct {
	Key     string // normalized lookup key, e.g. "visualstudiocode"
	Name    string // display name, e.g. "VisualStudioCode"
	Group   string // "core", "global", "community", "online", or "local"
	Source  Source
	Path    string // upstream path for builtin templates, file path for local ones
	Content string
}

// Lines returns the number of non-empty, non-comment rules in the template.
func (t *Template) Rules() int {
	n := 0
	for _, l := range strings.Split(t.Content, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			n++
		}
	}
	return n
}

// DefaultAliases maps common shorthand to catalog keys.
var DefaultAliases = map[string]string{
	"c#":         "visualstudio",
	"csharp":     "visualstudio",
	"dotnet":     "visualstudio",
	"golang":     "go",
	"idea":       "jetbrains",
	"intellij":   "jetbrains",
	"javascript": "node",
	"js":         "node",
	"mac":        "macos",
	"nodejs":     "node",
	"osx":        "macos",
	"py":         "python",
	"rs":         "rust",
	"ts":         "node",
	"typescript": "node",
	"vs":         "visualstudio",
	"vscode":     "visualstudiocode",
	"win":        "windows",
}

// Catalog is an immutable, merged view of all templates.
type Catalog struct {
	byKey   map[string]*Template
	list    []*Template
	aliases map[string]string
	presets map[string][]string
}

// Options configures lookup behavior.
type Options struct {
	Aliases map[string]string   // merged over DefaultAliases
	Presets map[string][]string // referenced as "@name"
}

// New builds a catalog. Later sets take priority over earlier ones when keys collide.
func New(opts Options, sets ...[]Template) *Catalog {
	c := &Catalog{byKey: map[string]*Template{}, aliases: map[string]string{}, presets: map[string][]string{}}
	for k, v := range DefaultAliases {
		c.aliases[k] = v
	}
	for k, v := range opts.Aliases {
		c.aliases[Normalize(k)] = Normalize(v)
	}
	for k, v := range opts.Presets {
		c.presets[Normalize(k)] = v
	}
	for _, set := range sets {
		for i := range set {
			t := set[i]
			if t.Key == "" {
				t.Key = Normalize(t.Name)
			}
			c.byKey[t.Key] = &t
		}
	}
	for _, t := range c.byKey {
		c.list = append(c.list, t)
	}
	sort.Slice(c.list, func(i, j int) bool { return c.list[i].Key < c.list[j].Key })
	return c
}

// Normalize converts user input or a file name into a lookup key.
func Normalize(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".gitignore")
	name = strings.ReplaceAll(name, " ", "")
	return strings.ToLower(name)
}

// All returns every template sorted by key.
func (c *Catalog) All() []*Template { return c.list }

// Len returns the number of templates.
func (c *Catalog) Len() int { return len(c.list) }

// Presets returns the configured presets.
func (c *Catalog) Presets() map[string][]string { return c.presets }

// Get looks up a template by key, display name, or alias.
func (c *Catalog) Get(name string) (*Template, bool) {
	k := Normalize(name)
	if t, ok := c.byKey[k]; ok {
		return t, true
	}
	if a, ok := c.aliases[k]; ok {
		t, ok := c.byKey[a]
		return t, ok
	}
	return nil, false
}

// UnknownError reports names that matched no template or preset.
type UnknownError struct {
	Names       []string
	Suggestions map[string][]string
}

func (e *UnknownError) Error() string {
	var b strings.Builder
	for i, n := range e.Names {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "unknown template %q", n)
		if s := e.Suggestions[n]; len(s) > 0 {
			fmt.Fprintf(&b, " (did you mean %s?)", strings.Join(s, ", "))
		}
	}
	return b.String()
}

// Resolve expands presets and aliases, drops duplicates, and keeps input order.
// Items may be comma-separated, so "go,macos" works like "go macos".
func (c *Catalog) Resolve(names []string) ([]*Template, error) {
	var out []*Template
	seen := map[string]bool{}
	unknown := &UnknownError{Suggestions: map[string][]string{}}
	var visit func(name string, depth int)
	visit = func(name string, depth int) {
		for _, part := range strings.Split(name, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, "@") {
				p, ok := c.presets[Normalize(part[1:])]
				if !ok || depth > 8 {
					unknown.Names = append(unknown.Names, part)
					continue
				}
				for _, n := range p {
					visit(n, depth+1)
				}
				continue
			}
			t, ok := c.Get(part)
			if !ok {
				unknown.Names = append(unknown.Names, part)
				unknown.Suggestions[part] = c.Suggest(part, 3)
				continue
			}
			if !seen[t.Key] {
				seen[t.Key] = true
				out = append(out, t)
			}
		}
	}
	for _, n := range names {
		visit(n, 0)
	}
	if len(unknown.Names) > 0 {
		return out, unknown
	}
	return out, nil
}

// Suggest returns up to n keys close to name, best first.
func (c *Catalog) Suggest(name string, n int) []string {
	k := Normalize(name)
	type scored struct {
		key   string
		score int
	}
	var cands []scored
	for _, t := range c.list {
		d := levenshtein(k, t.Key)
		switch {
		case strings.HasPrefix(t.Key, k):
			d = min(d, 1)
		case strings.Contains(t.Key, k) && len(k) >= 3:
			d = min(d, 2)
		}
		if d <= max(2, len(k)/3) {
			cands = append(cands, scored{t.Key, d})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score < cands[j].score
		}
		return len(cands[i].key) < len(cands[j].key)
	})
	var out []string
	for _, s := range cands {
		if len(out) == n {
			break
		}
		out = append(out, s.key)
	}
	return out
}

// Keys returns the keys of ts in order.
func Keys(ts []*Template) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Key
	}
	return out
}

// Names returns the display names of ts in order.
func Names(ts []*Template) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

// Builtin converts the embedded snapshot into templates. When the same key
// appears more than once, top-level templates win over Global, and Global over
// community.
func Builtin() []Template {
	rank := map[string]int{"core": 0, "global": 1, "community": 2}
	best := map[string]Template{}
	for _, f := range builtin.Files() {
		t := Template{
			Key:     Normalize(f.Stem),
			Name:    f.Stem,
			Group:   f.Group,
			Source:  SourceBuiltin,
			Path:    f.Path,
			Content: f.Content,
		}
		if cur, ok := best[t.Key]; ok && rank[cur.Group] <= rank[t.Group] {
			continue
		}
		best[t.Key] = t
	}
	out := make([]Template, 0, len(best))
	for _, t := range best {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Template) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
