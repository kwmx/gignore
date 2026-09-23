package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/catalog/builtin"
	"github.com/kwmx/gignore/internal/config"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/gitx"
	"github.com/kwmx/gignore/internal/paths"
	"github.com/kwmx/gignore/internal/version"
)

func newCache(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Inspect, refresh, or clear the online template cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.cacheInfo()
		},
	}
	c.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "Show what is cached",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.cacheInfo() },
	}, &cobra.Command{
		Use:   "refresh",
		Short: "Download the online catalog now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			e, err := engine.New(cfg)
			if err != nil {
				return err
			}
			o := e.OnlineOptions(engine.LoadOptions{Refresh: true})
			o.Offline = false // an explicit refresh overrides online.enabled = false
			res, err := catalog.LoadOnline(cmd.Context(), o)
			if err != nil {
				return err
			}
			if res.Stale {
				return fmt.Errorf("refresh failed, cached copy kept: %w", res.FetchErr)
			}
			a.success("Downloaded %d online templates", len(res.Templates))
			return nil
		},
	}, &cobra.Command{
		Use:   "clear",
		Short: "Delete the cached catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := catalog.CachePath(paths.CacheDir())
			err := os.Remove(p)
			if errors.Is(err, os.ErrNotExist) {
				a.note("Cache is already empty.")
				return nil
			}
			if err != nil {
				return err
			}
			a.success("Removed %s", a.rel(p))
			return nil
		},
	})
	return c
}

func (a *app) cacheInfo() error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	info := catalog.InspectCache(paths.CacheDir())
	w := a.out()
	fmt.Fprintf(w, "Cache file:  %s\n", a.rel(info.Path))
	if !info.Exists {
		fmt.Fprintln(w, "Status:      empty (run gignore cache refresh, or any command while online)")
		return nil
	}
	age := time.Since(info.FetchedAt)
	status := "fresh"
	if age >= cfg.Online.CacheTTL.Duration {
		status = "expired; refreshes on next online use"
	}
	fmt.Fprintf(w, "Templates:   %d\n", info.Templates)
	fmt.Fprintf(w, "Size:        %.1f KiB\n", float64(info.Bytes)/1024)
	fmt.Fprintf(w, "Fetched:     %s (%s ago)\n", info.FetchedAt.Local().Format("2006-01-02 15:04"), humanDuration(age))
	fmt.Fprintf(w, "Expires:     after %s (%s)\n", humanDuration(cfg.Online.CacheTTL.Duration), status)
	fmt.Fprintf(w, "Source:      %s\n", info.URL)
	return nil
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

func newConfig(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show, create, or edit configuration",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.configShow() },
	}
	var force, project bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a config file with every setting documented",
		Example: `  gignore config init              User config
  gignore config init --project    .gignore.toml at the repository root`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.configPath()
			if project {
				p = filepath.Join(a.projectRoot(), config.ProjectFile)
			}
			if fileExists(p) && !force {
				return fmt.Errorf("%s already exists; use --force to overwrite", a.rel(p))
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(config.Starter), 0o644); err != nil {
				return err
			}
			a.success("Wrote %s", a.rel(p))
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	initCmd.Flags().BoolVar(&project, "project", false, "create .gignore.toml at the repository root instead")
	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.configShow() },
	}, &cobra.Command{
		Use:   "path",
		Short: "Print the user config file path",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { fmt.Fprintln(a.stdout, a.configPath()) },
	}, &cobra.Command{
		Use:   "edit",
		Short: "Open the user config in $VISUAL or $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.configPath()
			if !fileExists(p) {
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(p, []byte(config.Starter), 0o644); err != nil {
					return err
				}
			}
			return openEditor(p)
		},
	}, initCmd)
	return c
}

func (a *app) configPath() string {
	if a.configFile != "" {
		return a.configFile
	}
	return paths.ConfigFile()
}

func (a *app) configShow() error {
	cfg, err := a.config()
	if err != nil {
		return err
	}
	if len(cfg.Sources) == 0 {
		a.note("# No config files found; showing defaults. Create one with gignore config init.")
	} else {
		a.note("# Loaded from: %s", strings.Join(cfg.Sources, ", "))
	}
	s, err := cfg.Encode()
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.stdout, s)
	return err
}

func openEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func newDoctor(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check git, configuration, templates, and network access",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.palette()
			okS := lipgloss.NewStyle().Foreground(p.Add).Render("✓")
			badS := lipgloss.NewStyle().Foreground(p.Error).Render("✗")
			warnS := lipgloss.NewStyle().Foreground(p.Warn).Render("!")
			w := a.out()
			failed := false
			line := func(mark, label, detail string) { fmt.Fprintf(w, "%s %-18s %s\n", mark, label, detail) }

			line(okS, "gignore", version.String())
			if gitx.Available() {
				out, _ := exec.Command("git", "--version").Output()
				line(okS, "git", strings.TrimSpace(string(out)))
			} else {
				line(warnS, "git", "not found; explain, untrack, and the tracked-file check need it")
			}
			cfg, err := a.config()
			if err != nil {
				line(badS, "config", err.Error())
				return &ExitError{Code: ExitFailure, Silent: true}
			}
			if len(cfg.Sources) == 0 {
				line(okS, "config", "defaults (no config file at "+a.rel(a.configPath())+")")
			} else {
				line(okS, "config", strings.Join(cfg.Sources, ", "))
			}
			src := builtin.Source()
			line(okS, "built-in templates", fmt.Sprintf("%d from github/gitignore %s (%s)", len(catalog.Builtin()), short(src["commit"]), src["synced"]))
			local, err := catalog.LoadLocal(paths.TemplatesDir())
			switch {
			case err != nil:
				line(badS, "local templates", err.Error())
				failed = true
			default:
				line(okS, "local templates", fmt.Sprintf("%d in %s", len(local), a.rel(paths.TemplatesDir())))
			}
			info := catalog.InspectCache(paths.CacheDir())
			if info.Exists {
				line(okS, "online cache", fmt.Sprintf("%d templates, fetched %s", info.Templates, info.FetchedAt.Local().Format("2006-01-02")))
			} else {
				line(warnS, "online cache", "empty")
			}
			if !cfg.Online.Enabled || a.offline {
				line(warnS, "network", "skipped (offline)")
			} else {
				e, _ := engine.New(cfg)
				ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Online.Timeout.Duration)
				defer cancel()
				rtt, err := catalog.Ping(ctx, e.OnlineOptions(engine.LoadOptions{}))
				if err != nil {
					line(badS, "network", err.Error())
					failed = true
				} else {
					line(okS, "network", fmt.Sprintf("%s answered in %d ms", cfg.Online.URL, rtt.Milliseconds()))
				}
			}
			if g, configured, err := gitx.GlobalExcludesFile(); err == nil {
				detail := a.rel(g)
				if !configured {
					detail += " (git's default location)"
				}
				if !fileExists(g) {
					detail += ", not created yet"
				}
				line(okS, "global ignore file", detail)
			}
			if failed {
				return &ExitError{Code: ExitFailure, Silent: true}
			}
			return nil
		},
	}
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func newManCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:    "gen-man <dir>",
		Short:  "Write man pages to a directory",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(args[0], 0o755); err != nil {
				return err
			}
			root.DisableAutoGenTag = true
			return doc.GenManTree(root, &doc.GenManHeader{Title: strings.ToUpper(version.Name), Section: "1", Source: version.Name + " " + version.Version}, args[0])
		},
	}
}
