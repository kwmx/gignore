package cli

import (
	"github.com/spf13/cobra"

	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/tui"
)

func (a *app) runTUI(cmd *cobra.Command, args []string, global bool, output string) error {
	if !a.isTTY() {
		return usageError("the interactive picker needs a terminal; use gignore generate for scripts (see gignore --help)")
	}
	cfg, err := a.config()
	if err != nil {
		return err
	}
	// Load only local sources up front so the picker opens instantly; the
	// online catalog loads in the background.
	e, err := engine.New(cfg)
	if err != nil {
		return err
	}
	path, err := a.target(global, output)
	if err != nil {
		return err
	}
	res, err := tui.Run(cmd.Context(), tui.Options{
		Engine:      e,
		Target:      path,
		DisplayPath: a.rel(path),
		Global:      global,
		ProjectRoot: a.projectRoot(),
		Initial:     args,
		Offline:     a.offline,
		Refresh:     a.refresh,
	})
	if err != nil {
		return err
	}
	if res.Saved != "" {
		a.success("%s", res.Saved)
	}
	return nil
}
