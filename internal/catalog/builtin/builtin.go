// Package builtin embeds a snapshot of https://github.com/github/gitignore
// (CC0-1.0) so gignore works offline. Refresh it with tools/synctemplates.
package builtin

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:data
var data embed.FS

// File is one embedded template.
type File struct {
	Path    string // relative to the upstream repository root, e.g. "Global/macOS.gitignore"
	Stem    string // file name without extension, e.g. "macOS"
	Group   string // "core", "global", or "community"
	Content string
}

// Files returns every embedded template in upstream directory order.
func Files() []File {
	var out []File
	_ = fs.WalkDir(data, "data", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".gitignore") {
			return err
		}
		b, err := data.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "data/")
		group := "core"
		switch {
		case strings.HasPrefix(rel, "Global/"):
			group = "global"
		case strings.HasPrefix(rel, "community/"):
			group = "community"
		}
		name := rel[strings.LastIndex(rel, "/")+1:]
		out = append(out, File{
			Path:    rel,
			Stem:    strings.TrimSuffix(name, ".gitignore"),
			Group:   group,
			Content: string(b),
		})
		return nil
	})
	return out
}

// Source describes the upstream commit the snapshot came from.
func Source() map[string]string {
	out := map[string]string{}
	b, err := data.ReadFile("data/SOURCE")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}
