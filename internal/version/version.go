// Package version holds build metadata injected at link time by GoReleaser.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Name is the program name used in output, user agents, and file names.
const Name = "gignore"

// Set with -ldflags "-X github.com/kwmx/gignore/internal/version.Version=...".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	if Version != "dev" {
		return
	}
	// `go install module@version` builds carry the module version but no ldflags.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	if Commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && len(s.Value) >= 7 {
					Commit = s.Value[:7]
				}
			}
		}
	}
}

// String returns a one-line description of the build.
func String() string {
	s := fmt.Sprintf("%s %s", Name, Version)
	if Commit != "" {
		s += " (" + Commit
		if Date != "" {
			s += ", " + Date
		}
		s += ")"
	}
	return s + fmt.Sprintf(" %s/%s", runtime.GOOS, runtime.GOARCH)
}

// UserAgent is sent with every HTTP request.
func UserAgent() string {
	return fmt.Sprintf("%s/%s (+https://github.com/kwmx/gignore)", Name, Version)
}
