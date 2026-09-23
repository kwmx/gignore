// Package paths resolves the directories gignore reads from and writes to.
//
// On Linux and macOS it follows the XDG base directory spec, because CLI users
// expect ~/.config and ~/.cache rather than ~/Library. On Windows it uses the
// standard AppData locations.
package paths

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/kwmx/gignore/internal/version"
)

// ConfigDir is where config.toml and user templates live.
func ConfigDir() string {
	if d := os.Getenv("GIGNORE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(base("XDG_CONFIG_HOME", ".config", os.UserConfigDir), version.Name)
}

// CacheDir holds the downloaded online catalog.
func CacheDir() string {
	if d := os.Getenv("GIGNORE_CACHE_DIR"); d != "" {
		return d
	}
	return filepath.Join(base("XDG_CACHE_HOME", ".cache", os.UserCacheDir), version.Name)
}

// StateDir holds usage history.
func StateDir() string {
	if d := os.Getenv("GIGNORE_STATE_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserCacheDir(); err == nil { // %LocalAppData%
			return filepath.Join(d, version.Name, "state")
		}
	}
	return filepath.Join(base("XDG_STATE_HOME", filepath.Join(".local", "state"), nil), version.Name)
}

// ConfigFile is the user config path, overridable with GIGNORE_CONFIG.
func ConfigFile() string {
	if f := os.Getenv("GIGNORE_CONFIG"); f != "" {
		return f
	}
	return filepath.Join(ConfigDir(), "config.toml")
}

// TemplatesDir holds the user's own *.gitignore templates.
func TemplatesDir() string { return filepath.Join(ConfigDir(), "templates") }

func base(env, homeRel string, windows func() (string, error)) string {
	if d := os.Getenv(env); d != "" && filepath.IsAbs(d) {
		return d
	}
	if runtime.GOOS == "windows" && windows != nil {
		if d, err := windows(); err == nil {
			return d
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), version.Name)
	}
	return filepath.Join(home, homeRel)
}
