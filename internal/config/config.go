// Package config loads settings from defaults, the user config file, the
// project's .gignore.toml, and environment variables, in that order.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/paths"
)

// ProjectFile is the per-repository config file name.
const ProjectFile = ".gignore.toml"

// Duration is a time.Duration written as a string like "24h" in TOML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q (examples: 30s, 12h)", string(b))
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Config is the effective configuration.
type Config struct {
	Online   Online              `toml:"online"`
	Generate Generate            `toml:"generate"`
	TUI      TUI                 `toml:"tui"`
	Presets  map[string][]string `toml:"presets"`
	Aliases  map[string]string   `toml:"aliases"`

	// Files that contributed to this config, in load order. Not written to TOML.
	Sources []string `toml:"-"`
}

// Online configures the gitignore.io catalog.
type Online struct {
	Enabled  bool     `toml:"enabled"`
	URL      string   `toml:"url"`
	CacheTTL Duration `toml:"cache_ttl"`
	Timeout  Duration `toml:"timeout"`
	Retries  int      `toml:"retries"`
	// Prefer decides which source wins when a template exists both built in and
	// online: "builtin" keeps output identical across machines, "online" gets
	// gitignore.io's versions.
	Prefer string `toml:"prefer"`
}

// Generate configures composed output.
type Generate struct {
	Output      string   `toml:"output"`
	Dedupe      bool     `toml:"dedupe"`
	Always      []string `toml:"always"`    // added to every generation, e.g. your OS and editor
	Templates   []string `toml:"templates"` // project default selection
	DetectDepth int      `toml:"detect_depth"`
}

// TUI configures the interactive interface.
type TUI struct {
	Theme      string `toml:"theme"` // auto, dark, light, or mono
	Mouse      bool   `toml:"mouse"`
	Detect     bool   `toml:"detect"`      // preselect detected templates in a new project
	SearchMode string `toml:"search_mode"` // fuzzy, exact, or regex
	ShowOS     bool   `toml:"suggest_os"`  // include the OS template in suggestions
}

// Default returns built-in settings.
func Default() Config {
	return Config{
		Online: Online{
			Enabled:  true,
			URL:      catalog.DefaultAPI,
			CacheTTL: Duration{7 * 24 * time.Hour},
			Timeout:  Duration{15 * time.Second},
			Retries:  2,
			Prefer:   "builtin",
		},
		Generate: Generate{Output: ".gitignore", Dedupe: true, DetectDepth: 2},
		TUI:      TUI{Theme: "auto", Mouse: true, Detect: true, SearchMode: "fuzzy", ShowOS: true},
		Presets:  map[string][]string{},
		Aliases:  map[string]string{},
	}
}

// Load builds the effective config. userFile may be empty to use the default
// location; dir is where to start looking for a project file.
func Load(userFile, dir string) (Config, error) {
	c := Default()
	if userFile == "" {
		userFile = paths.ConfigFile()
	}
	if err := decodeFile(&c, userFile); err != nil {
		return c, err
	}
	if p := FindProject(dir); p != "" {
		if err := decodeFile(&c, p); err != nil {
			return c, err
		}
	}
	if err := applyEnv(&c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func decodeFile(c *Config, path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	md, err := toml.Decode(string(b), c)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return fmt.Errorf("%s: unknown setting %s", path, strings.Join(keys, ", "))
	}
	c.Sources = append(c.Sources, path)
	return nil
}

func applyEnv(c *Config) error {
	if v, ok := os.LookupEnv("GIGNORE_OFFLINE"); ok {
		b, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("GIGNORE_OFFLINE: %w", err)
		}
		if b {
			c.Online.Enabled = false
		}
	}
	if v := os.Getenv("GIGNORE_API_URL"); v != "" {
		c.Online.URL = v
	}
	if v := os.Getenv("GIGNORE_THEME"); v != "" {
		c.TUI.Theme = v
	}
	if v := os.Getenv("GIGNORE_OUTPUT"); v != "" {
		c.Generate.Output = v
	}
	return nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	}
	return strconv.ParseBool(v)
}

// Validate rejects values that would fail later in confusing ways.
func (c Config) Validate() error {
	switch c.Online.Prefer {
	case "builtin", "online":
	default:
		return fmt.Errorf("online.prefer must be \"builtin\" or \"online\", got %q", c.Online.Prefer)
	}
	switch c.TUI.Theme {
	case "auto", "dark", "light", "mono":
	default:
		return fmt.Errorf("tui.theme must be auto, dark, light, or mono, got %q", c.TUI.Theme)
	}
	if c.Generate.DetectDepth < 0 || c.Generate.DetectDepth > 6 {
		return fmt.Errorf("generate.detect_depth must be between 0 and 6, got %d", c.Generate.DetectDepth)
	}
	return nil
}

// FindProject walks up from dir looking for .gignore.toml, stopping at the
// repository root so a file outside the repo never applies to it.
func FindProject(dir string) string {
	if dir == "" {
		return ""
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, ProjectFile)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		if isRepoRoot(dir) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isRepoRoot(dir string) bool {
	for _, m := range []string{".git", ".hg", ".jj"} {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// Encode renders c as TOML.
func (c Config) Encode() (string, error) {
	var b strings.Builder
	err := toml.NewEncoder(&b).Encode(c)
	return b.String(), err
}

// Starter is written by `gignore config init`.
const Starter = `# gignore configuration. Every setting is optional; the values shown are defaults.
# A .gignore.toml in a repository uses the same format and overrides this file.

[online]
# Also load the gitignore.io catalog (about 570 templates). Built-in templates
# always work offline.
enabled = true
url = "https://www.toptal.com/developers/gitignore/api"
cache_ttl = "168h"
timeout = "15s"
retries = 2
# Which copy wins when a template exists in both sources: "builtin" or "online".
prefer = "builtin"

[generate]
output = ".gitignore"
# Drop rules already added by an earlier template.
dedupe = true
# Templates added to every generation, for example your OS and editor.
always = []
# Default selection for a project. Usually set in a repository's .gignore.toml.
templates = []
# How many directory levels below the project root detection scans.
detect_depth = 2

[tui]
# auto, dark, light, or mono
theme = "auto"
mouse = true
# Preselect detected templates when a project has no gignore block yet.
detect = true
# fuzzy, exact, or regex
search_mode = "fuzzy"
suggest_os = true

# Named template sets. Use them as @name, for example: gignore generate @web
[presets]
# web = ["node", "macos", "visualstudiocode"]

# Extra shorthand names. Built-in aliases include js, py, golang, vscode, and mac.
[aliases]
# tf = "terraform"
`
