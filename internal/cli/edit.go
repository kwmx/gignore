package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/engine"
)

func newAdd(a *app) *cobra.Command {
	var wf writeFlags
	c := &cobra.Command{
		Use:   "add <templates...>",
		Short: "Add templates to a .gitignore",
		Long: `Add templates to the gignore block in a .gitignore, creating the block (or the
file) if needed. Rules outside the block are kept.`,
		Example:           "  gignore add node visualstudiocode\n  gignore add -g macos      Add to your global ignore file",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeTemplates,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.editSelection(cmd, wf, func(cur []*catalog.Template, e *engine.Engine) ([]string, string, error) {
				add, err := e.Catalog.Resolve(args)
				if err != nil {
					return nil, "", err
				}
				keys := catalog.Keys(cur)
				var added []string
				for _, t := range add {
					if !slices.Contains(keys, t.Key) {
						keys = append(keys, t.Key)
						added = append(added, t.Key)
					}
				}
				if len(added) == 0 {
					return keys, "", nil
				}
				return keys, "adding " + strings.Join(added, ", "), nil
			})
		},
	}
	wf.register(c)
	return c
}

func newRemove(a *app) *cobra.Command {
	var wf writeFlags
	var all bool
	c := &cobra.Command{
		Use:     "remove <templates...>",
		Aliases: []string{"rm"},
		Short:   "Remove templates from a .gitignore",
		Long: `Remove templates from the gignore block. Removing the last template, or using
--all, deletes the block and keeps your own rules.`,
		Example:           "  gignore remove jetbrains\n  gignore remove --all",
		ValidArgsFunction: a.completeSelected(&wf),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return usageError("name the templates to remove, or use --all")
			}
			if all {
				return a.removeBlock(wf)
			}
			return a.editSelection(cmd, wf, func(cur []*catalog.Template, e *engine.Engine) ([]string, string, error) {
				drop := map[string]bool{}
				for _, n := range args {
					if t, ok := e.Catalog.Get(n); ok {
						drop[t.Key] = true
					} else {
						drop[catalog.Normalize(n)] = true
					}
				}
				var keys, removed []string
				for _, t := range cur {
					if drop[t.Key] {
						removed = append(removed, t.Key)
					} else {
						keys = append(keys, t.Key)
					}
				}
				if len(removed) == 0 {
					return nil, "", fmt.Errorf("none of %s are in the block (it has: %s)", strings.Join(args, ", "), strings.Join(catalog.Keys(cur), ", "))
				}
				return keys, "removing " + strings.Join(removed, ", "), nil
			})
		},
	}
	wf.register(c)
	c.Flags().BoolVar(&all, "all", false, "remove the whole gignore block")
	return c
}

func newUpdate(a *app) *cobra.Command {
	var wf writeFlags
	var check bool
	c := &cobra.Command{
		Use:     "update",
		Aliases: []string{"up", "sync"},
		Short:   "Regenerate the gignore block from its recorded templates",
		Long: `Regenerate the gignore block with the latest version of the templates it lists.

With --check, nothing is written: the command exits with status 3 when the file
is out of date, which suits CI.`,
		Example: "  gignore update\n  gignore update --check     Fail in CI when .gitignore is out of date",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if check {
				wf.dryRun = true
			}
			var stale bool
			err := a.editSelection(cmd, wf, func(cur []*catalog.Template, e *engine.Engine) ([]string, string, error) {
				return catalog.Keys(cur), "", nil
			}, func(p engine.Plan) { stale = p.Changed })
			if err != nil {
				return err
			}
			if check && stale {
				return &ExitError{Code: ExitFindings, Err: fmt.Errorf("out of date; run gignore update")}
			}
			return nil
		},
	}
	wf.register(c)
	c.Flags().BoolVar(&check, "check", false, "exit with status 3 if the file is out of date, without writing")
	return c
}

// editSelection reads the templates recorded in the target's block, lets
// change compute a new list, and writes the regenerated block.
func (a *app) editSelection(cmd *cobra.Command, wf writeFlags,
	change func(cur []*catalog.Template, e *engine.Engine) (keys []string, what string, err error),
	observe ...func(engine.Plan),
) error {
	e, err := a.engine(cmd.Context())
	if err != nil {
		return err
	}
	path, err := a.target(wf.global, wf.output)
	if err != nil {
		return err
	}
	recorded, found, err := engine.ReadSelection(path)
	if err != nil {
		return err
	}
	if !found && cmd.Name() == "update" {
		return fmt.Errorf("%s has no gignore block; create one with gignore generate -w or gignore add", a.rel(path))
	}
	var cur []*catalog.Template
	if len(recorded) > 0 {
		cur, err = e.Catalog.Resolve(recorded)
		if err != nil {
			return fmt.Errorf("%s: %w", a.rel(path), err)
		}
	}
	keys, what, err := change(cur, e)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return a.removeBlock(wf)
	}
	// always-templates are already recorded in the block, so don't add them twice.
	b, err := e.Build(keys, engine.BuildOptions{Dedupe: e.Cfg.Generate.Dedupe && !wf.noDedup, SkipAlways: found || wf.noAlways})
	if err != nil {
		return err
	}
	plan, err := engine.PlanWrite(path, b.Block, block.Merge)
	if err != nil {
		return err
	}
	for _, o := range observe {
		o(plan)
	}
	if what == "" {
		what = "with " + strings.Join(b.Keys, ", ")
	}
	return a.commit(plan, wf, false, what)
}

func (a *app) removeBlock(wf writeFlags) error {
	path, err := a.target(wf.global, wf.output)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rest, found, err := block.Remove(string(old))
	if err != nil {
		return err
	}
	if !found {
		a.note("%s has no gignore block; nothing to remove.", a.rel(path))
		return nil
	}
	plan := engine.Plan{Path: path, Old: string(old), New: rest, Exists: true, Changed: true}
	if strings.TrimSpace(rest) == "" && !wf.dryRun {
		a.note("Only the gignore block was in %s; the file is left empty.", a.rel(path))
	}
	return a.commit(plan, wf, false, "removing the gignore block")
}

func (a *app) completeSelected(wf *writeFlags) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		path, err := a.target(wf.global, wf.output)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		keys, _, _ := engine.ReadSelection(path)
		var out []string
		for _, k := range keys {
			if strings.HasPrefix(k, toComplete) && !slices.Contains(args, k) {
				out = append(out, k)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
