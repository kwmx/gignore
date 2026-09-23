// Package cli implements the gignore command line.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/config"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/gitx"
	"github.com/kwmx/gignore/internal/ui"
)

// Exit codes. Documented in docs/cli.md.
const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitFindings = 3 // check found problems, or update --check found a stale file
)

// ExitError carries a specific exit code. When Silent is set the message has
// already been printed.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// app holds global flags and lazily loaded state shared by commands.
type app struct {
	configFile string
	dir        string
	offline    bool
	refresh    bool
	noColor    bool
	verbose    bool

	cfg    *config.Config
	eng    *engine.Engine
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
}

func (a *app) config() (config.Config, error) {
	if a.cfg != nil {
		return *a.cfg, nil
	}
	c, err := config.Load(a.configFile, a.workDir())
	if err != nil {
		return c, err
	}
	a.cfg = &c
	return c, nil
}

// engine loads the catalog, including the online catalog unless offline.
func (a *app) engine(ctx context.Context) (*engine.Engine, error) {
	if a.eng != nil {
		return a.eng, nil
	}
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	e, err := engine.Load(ctx, cfg, engine.LoadOptions{Offline: a.offline, Refresh: a.refresh})
	if err != nil {
		return nil, err
	}
	for _, w := range e.Warnings {
		a.warn("%s", w)
	}
	a.eng = e
	return e, nil
}

func (a *app) workDir() string {
	if a.dir != "" {
		return a.dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// projectRoot is the repository root when inside one, otherwise the work dir.
// Writing to the root by default avoids scattering .gitignore files into
// whatever subdirectory the command happened to run in.
func (a *app) projectRoot() string {
	if root, err := gitx.Root(a.workDir()); err == nil {
		return root
	}
	return a.workDir()
}

// target resolves which ignore file a command operates on.
func (a *app) target(global bool, output string) (string, error) {
	if global {
		p, _, err := gitx.GlobalExcludesFile()
		return p, err
	}
	if output != "" {
		if filepath.IsAbs(output) {
			return output, nil
		}
		return filepath.Join(a.workDir(), output), nil
	}
	cfg, err := a.config()
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(cfg.Generate.Output) {
		return cfg.Generate.Output, nil
	}
	return filepath.Join(a.projectRoot(), cfg.Generate.Output), nil
}

// rel shortens path for display.
func (a *app) rel(path string) string {
	// Relative paths read better, but not once they climb more than two levels.
	if r, err := filepath.Rel(a.workDir(), path); err == nil && !strings.HasPrefix(r, filepath.Join("..", "..", "..")) {
		return r
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// Output helpers. Styled output is downsampled or stripped automatically when
// the stream is not a color terminal or NO_COLOR is set.

func (a *app) out() io.Writer {
	if a.noColor {
		return &colorprofile.Writer{Forward: a.stdout, Profile: colorprofile.NoTTY}
	}
	return colorprofile.NewWriter(a.stdout, os.Environ())
}

func (a *app) errOut() io.Writer {
	if a.noColor {
		return &colorprofile.Writer{Forward: a.stderr, Profile: colorprofile.NoTTY}
	}
	return colorprofile.NewWriter(a.stderr, os.Environ())
}

func (a *app) palette() ui.Palette {
	if a.cfg != nil && a.cfg.TUI.Theme == "mono" {
		return ui.Mono
	}
	return ui.ANSI
}

func (a *app) success(format string, args ...any) {
	s := lipgloss.NewStyle().Foreground(a.palette().Add).Render("✓")
	fmt.Fprintf(a.errOut(), "%s %s\n", s, fmt.Sprintf(format, args...))
}

func (a *app) warn(format string, args ...any) {
	s := lipgloss.NewStyle().Foreground(a.palette().Warn).Render("warning:")
	fmt.Fprintf(a.errOut(), "%s %s\n", s, fmt.Sprintf(format, args...))
}

func (a *app) note(format string, args ...any) {
	s := lipgloss.NewStyle().Foreground(a.palette().Muted)
	fmt.Fprintln(a.errOut(), s.Render(fmt.Sprintf(format, args...)))
}

// stdoutIsTTY reports whether standard output is a terminal.
func (a *app) stdoutIsTTY() bool {
	out, ok := a.stdout.(*os.File)
	return ok && term.IsTerminal(out.Fd())
}

// isTTY reports whether both stdin and stdout are terminals.
func (a *app) isTTY() bool {
	in, ok1 := a.stdin.(*os.File)
	out, ok2 := a.stdout.(*os.File)
	return ok1 && ok2 && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

// errNeedsYes is returned when a confirmation is required but no terminal is attached.
var errNeedsYes = errors.New("confirmation required; rerun with --yes to proceed without a prompt")

// confirm asks a yes/no question on the terminal. Without a terminal it fails
// rather than guessing, unless yes is set.
func (a *app) confirm(question string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !a.isTTY() {
		return false, errNeedsYes
	}
	fmt.Fprintf(a.errOut(), "%s [y/N] ", question)
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	root := newRoot(a)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		if !ee.Silent && ee.Err != nil {
			fmt.Fprintf(a.errOut(), "%s %v\n", lipgloss.NewStyle().Foreground(a.palette().Error).Render("error:"), ee.Err)
		}
		return ee.Code
	}
	fmt.Fprintf(a.errOut(), "%s %v\n", lipgloss.NewStyle().Foreground(a.palette().Error).Render("error:"), err)
	if isUsageError(err) {
		fmt.Fprintf(a.errOut(), "Run '%s --help' for usage.\n", root.CommandPath())
		return ExitUsage
	}
	return ExitFailure
}

func isUsageError(err error) bool {
	msg := err.Error()
	for _, s := range []string{"unknown command", "unknown flag", "unknown shorthand", "flag needs an argument", "accepts ", "requires at least", "invalid argument", "if any flags in the group"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// usageError wraps err so it exits with ExitUsage.
func usageError(format string, args ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

func (a *app) completeTemplates(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := a.config()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// Completion must be fast: use cached online templates, never the network.
	e, err := engine.Load(cmd.Context(), cfg, engine.LoadOptions{Offline: true})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	prefix := strings.ToLower(toComplete)
	for _, t := range e.Catalog.All() {
		if strings.HasPrefix(t.Key, prefix) {
			out = append(out, t.Key+"\t"+t.Name+" ("+string(t.Source)+")")
		}
	}
	if strings.HasPrefix(toComplete, "@") || toComplete == "" {
		for name := range e.Catalog.Presets() {
			out = append(out, "@"+name+"\tpreset")
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
