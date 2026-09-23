// Package detect guesses which templates a project needs from the files in it.
package detect

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Match is one suggested template and the evidence for it.
type Match struct {
	Template string   `json:"template"`
	Reasons  []string `json:"reasons"`
	Kind     string   `json:"kind"` // "project", "editor", or "os"
}

type rule struct {
	template string
	kind     string
	names    []string // exact file or directory names
	globs    []string // filepath.Match patterns on the base name
}

var rules = []rule{
	{template: "node", kind: "project", names: []string{"package.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "bun.lock", ".nvmrc"}},
	{template: "go", kind: "project", names: []string{"go.mod", "go.work"}},
	{template: "rust", kind: "project", names: []string{"Cargo.toml"}},
	{template: "python", kind: "project", names: []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "Pipfile", "uv.lock", "poetry.lock", "tox.ini"}},
	{template: "java", kind: "project", globs: []string{"*.java"}},
	{template: "maven", kind: "project", names: []string{"pom.xml"}},
	{template: "gradle", kind: "project", names: []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew"}},
	{template: "kotlin", kind: "project", globs: []string{"*.kt", "*.kts"}},
	{template: "scala", kind: "project", names: []string{"build.sbt"}},
	{template: "ruby", kind: "project", names: []string{"Gemfile", "Rakefile", ".ruby-version"}},
	{template: "rails", kind: "project", names: []string{"config.ru"}},
	{template: "composer", kind: "project", names: []string{"composer.json"}},
	{template: "laravel", kind: "project", names: []string{"artisan"}},
	{template: "visualstudio", kind: "project", globs: []string{"*.sln", "*.csproj", "*.fsproj", "*.vbproj", "*.slnx"}},
	{template: "swift", kind: "project", names: []string{"Package.swift"}},
	{template: "xcode", kind: "project", globs: []string{"*.xcodeproj", "*.xcworkspace"}},
	{template: "dart", kind: "project", names: []string{"pubspec.yaml"}},
	{template: "flutter", kind: "project", names: []string{".metadata"}},
	{template: "elixir", kind: "project", names: []string{"mix.exs"}},
	{template: "haskell", kind: "project", names: []string{"stack.yaml", "cabal.project"}, globs: []string{"*.cabal"}},
	{template: "c++", kind: "project", globs: []string{"*.cpp", "*.cc", "*.hpp", "*.cxx"}},
	{template: "c", kind: "project", globs: []string{"*.c"}},
	{template: "cmake", kind: "project", names: []string{"CMakeLists.txt"}},
	{template: "zig", kind: "project", names: []string{"build.zig"}},
	{template: "terraform", kind: "project", globs: []string{"*.tf"}},
	{template: "unity", kind: "project", names: []string{"ProjectSettings", "Assets"}},
	{template: "godot", kind: "project", names: []string{"project.godot"}},
	{template: "unrealengine", kind: "project", globs: []string{"*.uproject"}},
	{template: "android", kind: "project", names: []string{"AndroidManifest.xml"}},
	{template: "r", kind: "project", names: []string{"DESCRIPTION", ".Rprofile"}, globs: []string{"*.Rproj"}},
	{template: "julia", kind: "project", names: []string{"Project.toml"}},
	{template: "lua", kind: "project", globs: []string{"*.rockspec"}},
	{template: "ocaml", kind: "project", names: []string{"dune-project"}},
	{template: "erlang", kind: "project", names: []string{"rebar.config"}},
	{template: "perl", kind: "project", names: []string{"Makefile.PL", "cpanfile"}},
	{template: "tex", kind: "project", globs: []string{"*.tex"}},
	{template: "jekyll", kind: "project", names: []string{"_config.yml"}},
	{template: "hugo", kind: "project", names: []string{"hugo.toml", "hugo.yaml"}},
	{template: "nextjs", kind: "project", names: []string{"next.config.js", "next.config.mjs", "next.config.ts"}},
	{template: "nuxtjs", kind: "project", names: []string{"nuxt.config.ts", "nuxt.config.js"}},
	{template: "sass", kind: "project", globs: []string{"*.scss", "*.sass"}},
	{template: "jupyternotebooks", kind: "project", globs: []string{"*.ipynb"}},
	{template: "packer", kind: "project", globs: []string{"*.pkr.hcl"}},
	{template: "ansible", kind: "project", names: []string{"ansible.cfg"}},
	{template: "visualstudiocode", kind: "editor", names: []string{".vscode"}, globs: []string{"*.code-workspace"}},
	{template: "jetbrains", kind: "editor", names: []string{".idea"}},
	{template: "sublimetext", kind: "editor", globs: []string{"*.sublime-project", "*.sublime-workspace"}},
	{template: "vim", kind: "editor", globs: []string{"*.swp", "Session.vim"}},
	{template: "emacs", kind: "editor", names: []string{".dir-locals.el", ".projectile"}},
	{template: "eclipse", kind: "editor", names: []string{".project", ".classpath"}},
}

// skip lists directories never worth scanning; they are large and generated.
var skip = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, "vendor": true,
	"target": true, "dist": true, "build": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, ".gradle": true, "Pods": true, ".next": true,
}

// Options controls the scan.
type Options struct {
	MaxDepth  int  // directory levels below root to scan; 0 means only root
	IncludeOS bool // suggest the template for the running operating system
}

// Detect scans root and returns suggestions sorted by kind then name.
func Detect(root string, o Options) ([]Match, error) {
	found := map[string]*Match{}
	add := func(r rule, reason string) {
		m, ok := found[r.template]
		if !ok {
			m = &Match{Template: r.template, Kind: r.kind}
			found[r.template] = m
		}
		if len(m.Reasons) < 3 && !contains(m.Reasons, reason) {
			m.Reasons = append(m.Reasons, reason)
		}
	}
	root = filepath.Clean(root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subdirectory: skip it, keep scanning
		}
		rel, _ := filepath.Rel(root, path)
		depth := 0
		if rel != "." {
			depth = strings.Count(filepath.ToSlash(rel), "/") + 1
		}
		name := d.Name()
		if d.IsDir() && path != root && (skip[name] || depth > o.MaxDepth+1) {
			return filepath.SkipDir
		}
		if path == root || depth > o.MaxDepth+1 {
			return nil
		}
		for _, r := range rules {
			if matches(r, name) {
				add(r, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if o.IncludeOS {
		if t := OSTemplate(); t != "" {
			found[t] = &Match{Template: t, Kind: "os", Reasons: []string{"running on " + runtime.GOOS}}
		}
	}
	out := make([]Match, 0, len(found))
	order := map[string]int{"project": 0, "editor": 1, "os": 2}
	for _, m := range found {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if order[out[i].Kind] != order[out[j].Kind] {
			return order[out[i].Kind] < order[out[j].Kind]
		}
		return out[i].Template < out[j].Template
	})
	return out, nil
}

// Templates returns just the template keys of ms.
func Templates(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Template
	}
	return out
}

func matches(r rule, name string) bool {
	for _, n := range r.names {
		if n == name {
			return true
		}
	}
	for _, g := range r.globs {
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
	}
	return false
}

// OSTemplate returns the template for the running operating system, or "".
func OSTemplate() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux", "freebsd", "openbsd", "netbsd":
		return "linux"
	}
	return ""
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Exists reports whether path exists; used by callers that probe for markers.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
