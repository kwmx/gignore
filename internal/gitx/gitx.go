// Package gitx runs the git commands gignore needs. Every function degrades
// gracefully when git is not installed or dir is not a repository.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrNoGit means the git executable is not on PATH.
var ErrNoGit = errors.New("git is not installed or not on PATH")

// Available reports whether git can be run.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func run(dir string, args ...string) (string, error) {
	if !Available() {
		return "", ErrNoGit
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), &Error{Args: args, Msg: msg, Err: err}
	}
	return out.String(), nil
}

// Error is a failed git invocation.
type Error struct {
	Args []string
	Msg  string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Msg) }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode returns git's exit status, or -1 when git did not run.
func (e *Error) ExitCode() int {
	var ee *exec.ExitError
	if errors.As(e.Err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Root returns the top-level directory of the repository containing dir.
func Root(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(strings.TrimSpace(out)), nil
}

// GlobalExcludesFile returns the user's global ignore file and whether it is
// set explicitly in git config. When unset, git reads $XDG_CONFIG_HOME/git/ignore
// (or ~/.config/git/ignore), so that is returned instead.
func GlobalExcludesFile() (path string, configured bool, err error) {
	if Available() {
		out, err := run("", "config", "--global", "--get", "core.excludesFile")
		if err == nil && strings.TrimSpace(out) != "" {
			return expandHome(strings.TrimSpace(out)), true, nil
		}
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "git", "ignore"), false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(home, ".config", "git", "ignore"), false, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// IgnoreMatch explains why a path is or isn't ignored.
type IgnoreMatch struct {
	Path    string
	Ignored bool   // false when no rule matched, or the last matching rule is a negation
	Source  string // file containing the deciding rule
	Line    int
	Pattern string
}

// CheckIgnore asks git which rule decides each path. It works for paths that
// don't exist and for tracked files.
func CheckIgnore(dir string, paths []string) ([]IgnoreMatch, error) {
	args := append([]string{"check-ignore", "--verbose", "--non-matching", "--no-index", "--"}, paths...)
	out, err := run(dir, args...)
	var gerr *Error
	// Exit status 1 only means "nothing ignored"; the output is still valid.
	if err != nil && (!errors.As(err, &gerr) || gerr.ExitCode() != 1) {
		return nil, err
	}
	var res []IgnoreMatch
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		info, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		m := IgnoreMatch{Path: path}
		// info is "source:line:pattern"; source may itself contain ':' on Windows.
		if info != "::" {
			last := strings.LastIndex(info, ":")
			mid := strings.LastIndex(info[:last], ":")
			if mid > 0 && last > mid {
				m.Source = info[:mid]
				m.Line, _ = strconv.Atoi(info[mid+1 : last])
				m.Pattern = info[last+1:]
				m.Ignored = !strings.HasPrefix(m.Pattern, "!")
			}
		}
		res = append(res, m)
	}
	return res, nil
}

// TrackedIgnored lists tracked files that the current ignore rules match.
// Such files stay in the repository until removed from the index.
func TrackedIgnored(dir string) ([]string, error) {
	out, err := run(dir, "ls-files", "--cached", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// RemoveCached untracks files without deleting them from disk.
func RemoveCached(dir string, files []string) error {
	const batch = 200 // stay well under command-line length limits
	for i := 0; i < len(files); i += batch {
		chunk := files[i:min(i+batch, len(files))]
		args := append([]string{"rm", "--cached", "--quiet", "--"}, chunk...)
		if _, err := run(dir, args...); err != nil {
			return err
		}
	}
	return nil
}
