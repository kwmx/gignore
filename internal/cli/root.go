package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/version"
)

func newRoot(a *app) *cobra.Command {
	var global bool
	var output string
	root := &cobra.Command{
		Use:   version.Name + " [templates...]",
		Short: "Build and maintain .gitignore files from a library of templates",
		Long: `gignore builds .gitignore files from more than 300 built-in templates, plus
about 570 from gitignore.io when online.

Run it in a terminal to open the interactive picker, with any templates you name
already selected. When output is piped or redirected, gignore prints the rules
for the named templates instead. Rules you add outside the generated block are
kept every time gignore updates the file.`,
		Example: `  gignore                           Open the interactive picker
  gignore node,macos > .gitignore   Print rules into a file
  gignore generate go macos -w      Write .gitignore with Go and macOS rules
  gignore generate --detect -w      Detect the project's stack and write .gitignore
  gignore add node                  Add Node rules to an existing .gitignore
  gignore check                     Find mistakes and tracked files that should be ignored
  gignore explain dist/app.js       Show which rule ignores a path`,
		Version:           version.Version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completeTemplates,
		RunE: func(cmd *cobra.Command, args []string) error {
			// "gignore node,macos > .gitignore" prints the rules, so the
			// command works in pipes and redirects as well as interactively.
			if len(args) > 0 && !a.stdoutIsTTY() {
				return a.printRules(cmd, args)
			}
			return a.runTUI(cmd, args, global, output)
		},
	}
	root.SetVersionTemplate(version.String() + "\n")
	pf := root.PersistentFlags()
	pf.StringVar(&a.configFile, "config", "", "use this config file instead of the default")
	pf.StringVarP(&a.dir, "dir", "C", "", "run as if started in this directory")
	pf.BoolVar(&a.offline, "offline", false, "don't use the network; cached online templates still load")
	pf.BoolVar(&a.refresh, "refresh", false, "refetch the online template catalog")
	pf.BoolVar(&a.noColor, "no-color", false, "disable colored output (also honors NO_COLOR)")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "print extra detail")
	root.Flags().BoolVarP(&global, "global", "g", false, "edit your global git ignore file")
	root.Flags().StringVarP(&output, "output", "o", "", "file to edit (default: .gitignore at the repository root)")

	root.AddGroup(
		&cobra.Group{ID: "write", Title: "Create and update:"},
		&cobra.Group{ID: "inspect", Title: "Inspect:"},
		&cobra.Group{ID: "manage", Title: "Manage gignore:"},
	)
	for _, c := range []*cobra.Command{newGenerate(a), newAdd(a), newRemove(a), newUpdate(a), newUntrack(a), newTUI(a)} {
		c.GroupID = "write"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{newList(a), newShow(a), newDetect(a), newCheck(a), newExplain(a)} {
		c.GroupID = "inspect"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{newCache(a), newConfig(a), newDoctor(a), newVersion(a)} {
		c.GroupID = "manage"
		root.AddCommand(c)
	}
	root.AddCommand(newManCmd(root))
	root.SetHelpCommandGroupID("manage")
	root.SetCompletionCommandGroupID("manage")
	return root
}

func newVersion(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(a.stdout, version.String())
		},
	}
}

func newTUI(a *app) *cobra.Command {
	var global bool
	var output string
	c := &cobra.Command{
		Use:               "tui [templates...]",
		Short:             "Open the interactive picker (the default command)",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completeTemplates,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTUI(cmd, args, global, output)
		},
	}
	c.Flags().BoolVarP(&global, "global", "g", false, "edit your global git ignore file")
	c.Flags().StringVarP(&output, "output", "o", "", "file to edit (default: .gitignore at the repository root)")
	return c
}
