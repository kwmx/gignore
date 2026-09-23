// Package search filters template names with fuzzy, substring, or regex matching.
package search

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

// Mode selects the matching algorithm.
type Mode int

const (
	Fuzzy Mode = iota
	Exact      // case-insensitive substring
	Regex
)

// Modes lists every mode in cycling order.
var Modes = []Mode{Fuzzy, Exact, Regex}

func (m Mode) String() string {
	switch m {
	case Exact:
		return "exact"
	case Regex:
		return "regex"
	default:
		return "fuzzy"
	}
}

// ParseMode accepts "fuzzy", "exact", or "regex".
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "fuzzy":
		return Fuzzy, nil
	case "exact", "substring":
		return Exact, nil
	case "regex", "regexp":
		return Regex, nil
	}
	return Fuzzy, fmt.Errorf("unknown search mode %q (use fuzzy, exact, or regex)", s)
}

// Next returns the mode after m.
func (m Mode) Next() Mode { return Modes[(int(m)+1)%len(Modes)] }

// Hit is a matching item with the rune positions that matched, for highlighting.
type Hit struct {
	Index   int
	Matched []int
}

// Filter returns the items matching query, best match first. An empty query
// returns every item in its original order.
func Filter(items []string, query string, mode Mode) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		hits := make([]Hit, len(items))
		for i := range items {
			hits[i] = Hit{Index: i}
		}
		return hits, nil
	}
	switch mode {
	case Exact:
		q := strings.ToLower(query)
		var hits []Hit
		for i, it := range items {
			l := strings.ToLower(it)
			if pos := strings.Index(l, q); pos >= 0 {
				hits = append(hits, Hit{Index: i, Matched: runeRange(l, pos, len(q))})
			}
		}
		// Prefix matches first, then shorter names, so "go" ranks "go" above "google".
		sort.SliceStable(hits, func(a, b int) bool {
			pa := strings.HasPrefix(strings.ToLower(items[hits[a].Index]), q)
			pb := strings.HasPrefix(strings.ToLower(items[hits[b].Index]), q)
			if pa != pb {
				return pa
			}
			return len(items[hits[a].Index]) < len(items[hits[b].Index])
		})
		return hits, nil
	case Regex:
		re, err := regexp.Compile("(?i)" + query)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		var hits []Hit
		for i, it := range items {
			if loc := re.FindStringIndex(it); loc != nil {
				hits = append(hits, Hit{Index: i, Matched: runeRange(it, loc[0], loc[1]-loc[0])})
			}
		}
		return hits, nil
	default:
		ms := fuzzy.Find(query, items)
		// fuzzy.Find sorts by score; break ties toward exact and shorter names.
		sort.SliceStable(ms, func(a, b int) bool {
			ea := strings.EqualFold(ms[a].Str, query)
			eb := strings.EqualFold(ms[b].Str, query)
			if ea != eb {
				return ea
			}
			if ms[a].Score != ms[b].Score {
				return ms[a].Score > ms[b].Score
			}
			return len(ms[a].Str) < len(ms[b].Str)
		})
		hits := make([]Hit, len(ms))
		for i, m := range ms {
			hits[i] = Hit{Index: m.Index, Matched: bytesToRunes(m.Str, m.MatchedIndexes)}
		}
		return hits, nil
	}
}

// runeRange converts a byte span of s into rune indexes.
func runeRange(s string, start, n int) []int {
	var out []int
	ri := 0
	for bi := range s {
		if bi >= start && bi < start+n {
			out = append(out, ri)
		}
		ri++
	}
	return out
}

func bytesToRunes(s string, byteIdx []int) []int {
	want := map[int]bool{}
	for _, b := range byteIdx {
		want[b] = true
	}
	var out []int
	ri := 0
	for bi := range s {
		if want[bi] {
			out = append(out, ri)
		}
		ri++
	}
	return out
}
