// Package diffview produces unified diffs of .gitignore changes.
package diffview

import (
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// Unified returns a unified diff, or "" when old and new are equal.
func Unified(name, old, new string) string {
	if old == new {
		return ""
	}
	return udiff.Unified("a/"+name, "b/"+name, old, new)
}

// Stats counts added and removed lines in a unified diff.
func Stats(diff string) (added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return added, removed
}
