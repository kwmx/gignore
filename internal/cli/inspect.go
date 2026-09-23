package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/check"
	"github.com/kwmx/gignore/internal/compose"
	"github.com/kwmx/gignore/internal/detect"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/gitx"
	"github.com/kwmx/gignore/internal/paths"
	"github.com/kwmx/gignore/internal/search"
	"github.com/kwmx/gignore/internal/ui"
)

func newList(a *app) *cobra.Command {
	var asJSON, keysOnly bool
	var source, mode string
	c := &cobra.Command{
		Use:     "list [query]",
		Aliases: []string{"ls", "search"},
		Short:   "List or search available templates",
		Example: `  gignore list
  gignore list python              Fuzzy search
  gignore list '^vis' --mode regex
  gignore list --source local      Only your own templates
  gignore list --keys | fzf        Plain names for scripting`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.engine(cmd.Context())
			if err != nil {
				return err
			}
			m, err := search.ParseMode(mode)
			if err != nil {
				return usageError("%v", err)
			}
			var pool []*catalog.Template
			for _, t := range e.Catalog.All() {
				if source == "" || string(t.Source) == source || (source == "online" && t.Source == catalog.SourceOnline) {
					pool = append(pool, t)
				}
			}
			names := make([]string, len(pool))
			for i, t := range pool {
				names[i] = t.Key
			}
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			hits, err := search.Filter(names, query, m)
			if err != nil {
				return usageError("%v", err)
			}
			if len(pool) == 0 && source == "local" {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf("no local templates; add *.gitignore files to %s", a.rel(paths.TemplatesDir()))}
			}
			if len(hits) == 0 {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf("no templates match %q", query)}
			}
			type row struct {
				Key    string `json:"key"`
				Name   string `json:"name"`
				Source string `json:"source"`
				Group  string `json:"group"`
				Rules  int    `json:"rules"`
			}
			rows := make([]row, len(hits))
			for i, h := range hits {
				t := pool[h.Index]
				rows[i] = row{t.Key, t.Name, string(t.Source), t.Group, t.Rules()}
			}
			switch {
			case asJSON:
				enc := json.NewEncoder(a.stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			case keysOnly || !a.isTTY():
				for _, r := range rows {
					fmt.Fprintln(a.stdout, r.Key)
				}
				return nil
			}
			p := a.palette()
			muted := lipgloss.NewStyle().Foreground(p.Muted)
			tw := tabwriter.NewWriter(a.out(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, lipgloss.NewStyle().Bold(true).Render("TEMPLATE")+"\t"+lipgloss.NewStyle().Bold(true).Render("SOURCE")+"\t"+lipgloss.NewStyle().Bold(true).Render("RULES"))
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Key, muted.Render(sourceLabel(r.Source, r.Group)), muted.Render(fmt.Sprint(r.Rules)))
			}
			tw.Flush()
			a.note("%d of %d templates. Built-in: %d. Online: %s.", len(rows), e.Catalog.Len(), len(catalog.Builtin()), onlineSummary(e))
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	c.Flags().BoolVar(&keysOnly, "keys", false, "print only template names, one per line")
	c.Flags().StringVar(&source, "source", "", "only templates from this source: builtin, online, or local")
	c.Flags().StringVarP(&mode, "mode", "m", "fuzzy", "search mode: fuzzy, exact, or regex")
	_ = c.RegisterFlagCompletionFunc("source", cobra.FixedCompletions([]string{"builtin", "online", "local"}, cobra.ShellCompDirectiveNoFileComp))
	_ = c.RegisterFlagCompletionFunc("mode", cobra.FixedCompletions([]string{"fuzzy", "exact", "regex"}, cobra.ShellCompDirectiveNoFileComp))
	return c
}

func sourceLabel(source, group string) string {
	if source == string(catalog.SourceBuiltin) && group != "core" {
		return source + "/" + group
	}
	return source
}

func onlineSummary(e *engine.Engine) string {
	if !e.OnlineOK {
		return "unavailable"
	}
	s := fmt.Sprintf("%d, fetched %s", len(e.Online.Templates), e.Online.FetchedAt.Local().Format("2006-01-02"))
	if e.Online.Stale {
		s += " (stale)"
	}
	return s
}

func newShow(a *app) *cobra.Command {
	var raw bool
	c := &cobra.Command{
		Use:               "show <templates...>",
		Aliases:           []string{"cat"},
		Short:             "Print the rules of one or more templates",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeTemplates,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.engine(cmd.Context())
			if err != nil {
				return err
			}
			ts, err := e.Catalog.Resolve(args)
			if err != nil {
				return err
			}
			if raw {
				for _, t := range ts {
					fmt.Fprint(a.stdout, t.Content)
				}
				return nil
			}
			text := compose.Compose(ts, compose.Options{}).Text
			if a.isTTY() {
				for _, t := range ts {
					origin := string(t.Source)
					if t.Path != "" {
						origin += ": " + t.Path
					}
					a.note("%s (%s, %d rules)", t.Name, origin, t.Rules())
				}
				text = ui.HighlightGitignore(a.palette(), text)
			}
			_, err = fmt.Fprint(a.out(), text)
			return err
		},
	}
	c.Flags().BoolVar(&raw, "raw", false, "print template files exactly as stored, without section headers")
	return c
}

func newDetect(a *app) *cobra.Command {
	var asJSON bool
	var depth int
	c := &cobra.Command{
		Use:   "detect [dir]",
		Short: "Suggest templates based on the project's files",
		Example: `  gignore detect
  gignore detect --json
  gignore generate --detect -w     Write the suggestions to .gitignore`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			root := a.projectRoot()
			if len(args) == 1 {
				root = args[0]
			}
			if !cmd.Flags().Changed("depth") {
				depth = cfg.Generate.DetectDepth
			}
			ms, err := detect.Detect(root, detect.Options{MaxDepth: depth, IncludeOS: cfg.TUI.ShowOS})
			if err != nil {
				return err
			}
			if asJSON {
				if ms == nil {
					ms = []detect.Match{}
				}
				enc := json.NewEncoder(a.stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(ms)
			}
			if len(ms) == 0 {
				a.note("No known project files found in %s.", a.rel(root))
				return nil
			}
			p := a.palette()
			muted := lipgloss.NewStyle().Foreground(p.Muted)
			tw := tabwriter.NewWriter(a.out(), 0, 4, 2, ' ', 0)
			for _, m := range ms {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Template, muted.Render(m.Kind), muted.Render(strings.Join(m.Reasons, ", ")))
			}
			tw.Flush()
			fmt.Fprintln(a.stderr)
			a.note("Write these with: gignore generate --detect -w")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	c.Flags().IntVar(&depth, "depth", 2, "directory levels to scan below the root")
	return c
}

func newCheck(a *app) *cobra.Command {
	var wf writeFlags
	var asJSON, strict, noGit bool
	c := &cobra.Command{
		Use:     "check",
		Aliases: []string{"lint"},
		Short:   "Find problems in a .gitignore",
		Long: `Check a .gitignore for common mistakes:

  - generated block out of date, or listing templates that no longer exist
  - tracked files that match ignore rules (they stay in the repository)
  - negations that can never apply because a parent directory is ignored
  - rules with trailing spaces or backslashes
  - duplicate and redundant rules
  - detected project types missing from the block

Exits with status 3 when it finds an error, or any warning with --strict.`,
		Example: "  gignore check\n  gignore check --strict --json     For CI",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.engine(cmd.Context())
			if err != nil {
				return err
			}
			path, err := a.target(wf.global, wf.output)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			in := check.Input{
				Content: string(b),
				Catalog: e.Catalog,
				Regenerated: func(keys []string) (string, error) {
					bd, err := e.Build(keys, engine.BuildOptions{Dedupe: e.Cfg.Generate.Dedupe, SkipAlways: true})
					return bd.Body, err
				},
			}
			if !wf.global && !noGit {
				dir := filepath.Dir(path)
				if files, err := gitx.TrackedIgnored(dir); err == nil {
					in.TrackedIgnored = files
				}
				if found, err := a.detected(e); err == nil {
					in.Detected = found
				}
			}
			fs := check.Run(in)
			if asJSON {
				if fs == nil {
					fs = []check.Finding{}
				}
				enc := json.NewEncoder(a.stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(fs); err != nil {
					return err
				}
			} else {
				a.printFindings(a.rel(path), fs)
			}
			w := check.Worst(fs)
			if w == check.Error || (strict && w == check.Warning) {
				return &ExitError{Code: ExitFindings, Silent: true}
			}
			return nil
		},
	}
	c.Flags().StringVarP(&wf.output, "output", "o", "", "file to check (default: .gitignore at the repository root)")
	c.Flags().BoolVarP(&wf.global, "global", "g", false, "check your global git ignore file")
	c.Flags().BoolVar(&asJSON, "json", false, "print findings as JSON")
	c.Flags().BoolVar(&strict, "strict", false, "exit with status 3 on warnings too")
	c.Flags().BoolVar(&noGit, "no-git", false, "skip checks that run git")
	return c
}

func (a *app) printFindings(name string, fs []check.Finding) {
	p := a.palette()
	if len(fs) == 0 {
		a.success("%s: no problems found", name)
		return
	}
	styles := map[check.Severity]lipgloss.Style{
		check.Error:   lipgloss.NewStyle().Foreground(p.Error).Bold(true),
		check.Warning: lipgloss.NewStyle().Foreground(p.Warn),
		check.Info:    lipgloss.NewStyle().Foreground(p.Info),
	}
	muted := lipgloss.NewStyle().Foreground(p.Muted)
	w := a.out()
	counts := map[check.Severity]int{}
	for _, f := range fs {
		counts[f.Severity]++
		loc := name
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", name, f.Line)
		}
		fmt.Fprintf(w, "%s %s %s %s\n", styles[f.Severity].Render(fmt.Sprintf("%-7s", f.Severity)), loc, f.Message, muted.Render("["+f.Code+"]"))
		if f.Fix != "" {
			fmt.Fprintf(w, "        %s\n", muted.Render("→ "+f.Fix))
		}
	}
	fmt.Fprintf(w, "\n%d error(s), %d warning(s), %d note(s)\n", counts[check.Error], counts[check.Warning], counts[check.Info])
}

func newExplain(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:     "explain <paths...>",
		Aliases: []string{"why"},
		Short:   "Show which rule ignores a path, and which template it came from",
		Example: "  gignore explain dist/app.js .env\n  gignore why node_modules",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !gitx.Available() {
				return gitx.ErrNoGit
			}
			dir := a.workDir()
			ms, err := gitx.CheckIgnore(dir, args)
			if err != nil {
				return err
			}
			p := a.palette()
			ign := lipgloss.NewStyle().Foreground(p.Warn).Bold(true)
			ok := lipgloss.NewStyle().Foreground(p.Add).Bold(true)
			muted := lipgloss.NewStyle().Foreground(p.Muted)
			w := a.out()
			for _, m := range ms {
				switch m.Source {
				case "":
					fmt.Fprintf(w, "%s %s\n", ok.Render("not ignored"), m.Path)
					fmt.Fprintf(w, "  %s\n", muted.Render("no rule matches"))
				default:
					state := ign.Render("ignored    ")
					if !m.Ignored {
						state = ok.Render("not ignored")
					}
					fmt.Fprintf(w, "%s %s\n", state, m.Path)
					src := m.Source
					if !filepath.IsAbs(src) {
						src = filepath.Join(dir, src)
					}
					fmt.Fprintf(w, "  rule %s at %s:%d", m.Pattern, a.rel(src), m.Line)
					if tmpl := templateForLine(src, m.Line); tmpl != "" {
						fmt.Fprintf(w, " %s", muted.Render("(from template "+tmpl+")"))
					}
					fmt.Fprintln(w)
				}
			}
			return nil
		},
	}
	return c
}

// templateForLine returns the template section containing line (1-based) of
// file, when that line is inside a gignore block.
func templateForLine(file string, line int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	d, err := block.Parse(string(b))
	if err != nil || !d.Found {
		return ""
	}
	off := d.BodyLineOffset()
	idx := line - 1 - off
	if idx < 0 || idx > strings.Count(d.Body, "\n") {
		return ""
	}
	return compose.SectionAt(d.Body, idx)
}

func newUntrack(a *app) *cobra.Command {
	var dryRun, yes bool
	c := &cobra.Command{
		Use:   "untrack",
		Short: "Stop tracking files that your ignore rules match",
		Long: `Remove tracked files that match your ignore rules from the git index, so the
next commit deletes them from the repository. The files stay on disk.

Review the list before confirming: other clones will delete these files when
they pull the commit.`,
		Example: "  gignore untrack --dry-run\n  gignore untrack",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := a.projectRoot()
			files, err := gitx.TrackedIgnored(root)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				a.success("No tracked files match your ignore rules")
				return nil
			}
			for _, f := range files {
				fmt.Fprintln(a.stdout, f)
			}
			if dryRun {
				fmt.Fprintln(a.stderr)
				a.note("Dry run: %d file(s) would be untracked.", len(files))
				return nil
			}
			ok, err := a.confirm(fmt.Sprintf("Untrack these %d file(s)? They stay on disk.", len(files)), yes)
			if err != nil {
				return err
			}
			if !ok {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf("cancelled; nothing was untracked")}
			}
			if err := gitx.RemoveCached(root, files); err != nil {
				return err
			}
			a.success("Untracked %d file(s). Review with git status, then commit.", len(files))
			return nil
		},
	}
	c.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "list the files without untracking them")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return c
}
