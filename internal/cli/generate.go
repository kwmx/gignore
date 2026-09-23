package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/detect"
	"github.com/kwmx/gignore/internal/diffview"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/ui"
)

// writeFlags are shared by every command that changes an ignore file.
type writeFlags struct {
	output   string
	global   bool
	dryRun   bool
	diff     bool
	yes      bool
	noDedup  bool
	noAlways bool
}

func (f *writeFlags) register(c *cobra.Command) {
	c.Flags().StringVarP(&f.output, "output", "o", "", "file to write (default: .gitignore at the repository root)")
	c.Flags().BoolVarP(&f.global, "global", "g", false, "write your global git ignore file instead")
	c.Flags().BoolVarP(&f.dryRun, "dry-run", "n", false, "show the changes without writing")
	c.Flags().BoolVarP(&f.diff, "diff", "d", false, "show the changes, then write")
	c.Flags().BoolVarP(&f.yes, "yes", "y", false, "don't ask for confirmation")
	c.Flags().BoolVar(&f.noDedup, "no-dedupe", false, "keep rules repeated across templates")
	c.Flags().BoolVar(&f.noAlways, "no-always", false, "leave out the templates listed in generate.always")
	c.MarkFlagsMutuallyExclusive("output", "global")
	c.MarkFlagsMutuallyExclusive("dry-run", "yes")
}

func newGenerate(a *app) *cobra.Command {
	var wf writeFlags
	var write, detectFlag, replace, raw bool
	c := &cobra.Command{
		Use:     "generate [templates...]",
		Aliases: []string{"gen", "g"},
		Short:   "Build ignore rules from templates",
		Long: `Build ignore rules from one or more templates.

Without --write or --output, the rules print to standard output. With either
flag, gignore writes them into a managed block in the file and keeps everything
outside that block.

Templates can be names, aliases such as js or vscode, comma-separated lists, or
presets written as @name. With no templates, gignore uses generate.templates from
.gignore.toml, or detection when --detect is set.`,
		Example: `  gignore generate go macos            Print rules for Go and macOS
  gignore generate go,macos -w         Write them to .gitignore
  gignore generate --detect -w         Detect the stack and write .gitignore
  gignore generate @web -o web/.gitignore
  gignore generate python --diff -w    Show the diff, then write`,
		ValidArgsFunction: a.completeTemplates,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := a.engine(ctx)
			if err != nil {
				return err
			}
			names := slices.Clone(args)
			if detectFlag {
				found, err := a.detected(e)
				if err != nil {
					return err
				}
				if len(found) == 0 {
					a.note("Detection found no known project files.")
				} else {
					a.note("Detected: %s", strings.Join(found, ", "))
				}
				names = append(names, found...)
			}
			if len(names) == 0 {
				names = e.Cfg.Generate.Templates
			}
			if len(names) == 0 {
				return usageError("name at least one template, or use --detect (see gignore list)")
			}
			b, err := e.Build(names, engine.BuildOptions{Dedupe: e.Cfg.Generate.Dedupe && !wf.noDedup, SkipAlways: wf.noAlways})
			if err != nil {
				return err
			}
			if a.verbose && b.Dropped > 0 {
				a.note("Removed %d duplicate rules.", b.Dropped)
			}
			toFile := write || wf.output != "" || wf.global || wf.dryRun
			if !toFile {
				text := b.Body
				if !raw {
					text = b.Block
				}
				_, err := fmt.Fprint(a.stdout, text)
				return err
			}
			mode := block.Merge
			if replace {
				mode = block.Replace
			}
			return a.applyBlock(wf, b.Block, mode, fmt.Sprintf("with %s", strings.Join(b.Keys, ", ")))
		},
	}
	wf.register(c)
	c.Flags().BoolVarP(&write, "write", "w", false, "write to .gitignore at the repository root")
	c.Flags().BoolVar(&detectFlag, "detect", false, "add templates detected from the project's files")
	c.Flags().BoolVar(&replace, "replace", false, "overwrite the whole file instead of keeping your own rules")
	c.Flags().BoolVar(&raw, "raw", false, "when printing, leave out the gignore block markers")
	return c
}

// printRules writes the block for names to standard output.
func (a *app) printRules(cmd *cobra.Command, names []string) error {
	e, err := a.engine(cmd.Context())
	if err != nil {
		return err
	}
	b, err := e.Build(names, engine.BuildOptions{Dedupe: e.Cfg.Generate.Dedupe})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(a.stdout, b.Block)
	return err
}

// detected returns detected template keys that exist in the catalog.
func (a *app) detected(e *engine.Engine) ([]string, error) {
	ms, err := detect.Detect(a.projectRoot(), detect.Options{MaxDepth: e.Cfg.Generate.DetectDepth, IncludeOS: e.Cfg.TUI.ShowOS})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range ms {
		if _, ok := e.Catalog.Get(m.Template); ok {
			out = append(out, m.Template)
		}
	}
	return out, nil
}

// applyBlock writes blk into the target file, honoring dry-run, diff, and the
// confirmation rules shared by every write command.
func (a *app) applyBlock(wf writeFlags, blk string, mode block.Mode, what string) error {
	path, err := a.target(wf.global, wf.output)
	if err != nil {
		return err
	}
	plan, err := engine.PlanWrite(path, blk, mode)
	if err != nil {
		return err
	}
	return a.commit(plan, wf, mode == block.Replace, what)
}

func (a *app) commit(plan engine.Plan, wf writeFlags, destructive bool, what string) error {
	name := a.rel(plan.Path)
	if !plan.Changed {
		a.success("%s is already up to date", name)
		return nil
	}
	d := diffview.Unified(name, plan.Old, plan.New)
	if wf.dryRun || wf.diff {
		fmt.Fprintln(a.out(), ui.ColorizeDiff(a.palette(), d))
	}
	if wf.dryRun {
		added, removed := diffview.Stats(d)
		a.note("Dry run: %s would change (+%d -%d). Nothing was written.", name, added, removed)
		return nil
	}
	if destructive && plan.Exists {
		ok, err := a.confirm(fmt.Sprintf("Replace all of %s, including your own rules?", name), wf.yes)
		if err != nil {
			return err
		}
		if !ok {
			return &ExitError{Code: ExitFailure, Err: fmt.Errorf("cancelled; %s was not changed", name)}
		}
	}
	if err := plan.Write(); err != nil {
		return err
	}
	verb := "Updated"
	if !plan.Exists {
		verb = "Created"
	}
	added, removed := diffview.Stats(d)
	a.success("%s %s %s (+%d -%d)", verb, name, what, added, removed)
	return nil
}

// fileExists is a small helper for commands that read the target file.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
