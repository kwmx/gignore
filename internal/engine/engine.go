// Package engine ties configuration, the template catalog, and composition
// together. The CLI and the TUI both go through it so they produce identical
// output.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/compose"
	"github.com/kwmx/gignore/internal/config"
	"github.com/kwmx/gignore/internal/paths"
)

// Engine holds a loaded catalog.
type Engine struct {
	Cfg      config.Config
	Catalog  *catalog.Catalog
	Online   catalog.OnlineResult
	OnlineOK bool
	// Warnings are non-fatal problems worth showing, such as an unreachable API.
	Warnings []string

	builtin []catalog.Template
	local   []catalog.Template
}

// LoadOptions controls catalog loading.
type LoadOptions struct {
	Offline  bool // skip the network; still use a cached online catalog if present
	Refresh  bool // refetch the online catalog even if the cache is fresh
	NoOnline bool // skip the online catalog entirely, cached or not
}

// New loads built-in and local templates only. Call LoadOnline to add the rest.
func New(cfg config.Config) (*Engine, error) {
	e := &Engine{Cfg: cfg, builtin: catalog.Builtin()}
	local, err := catalog.LoadLocal(paths.TemplatesDir())
	if err != nil {
		e.Warnings = append(e.Warnings, fmt.Sprintf("could not read local templates: %v", err))
	}
	e.local = local
	e.rebuild()
	return e, nil
}

// Load is New followed by LoadOnline.
func Load(ctx context.Context, cfg config.Config, o LoadOptions) (*Engine, error) {
	e, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if !o.NoOnline {
		e.LoadOnline(ctx, o)
	}
	return e, nil
}

// OnlineOptions returns the fetch settings derived from config.
func (e *Engine) OnlineOptions(o LoadOptions) catalog.OnlineOptions {
	return catalog.OnlineOptions{
		URL:      e.Cfg.Online.URL,
		CacheDir: paths.CacheDir(),
		TTL:      e.Cfg.Online.CacheTTL.Duration,
		Timeout:  e.Cfg.Online.Timeout.Duration,
		Retries:  e.Cfg.Online.Retries,
		Refresh:  o.Refresh,
		Offline:  o.Offline || !e.Cfg.Online.Enabled,
	}
}

// FetchOnline loads the online catalog without modifying e, so the TUI can run
// it in the background and apply it with SetOnline.
func (e *Engine) FetchOnline(ctx context.Context, o LoadOptions) (catalog.OnlineResult, error) {
	return catalog.LoadOnline(ctx, e.OnlineOptions(o))
}

// LoadOnline fetches (or reads from cache) the online catalog and merges it.
// Failure is recorded as a warning: built-in templates still work.
func (e *Engine) LoadOnline(ctx context.Context, o LoadOptions) {
	res, err := e.FetchOnline(ctx, o)
	if err != nil && (o.Offline || !e.Cfg.Online.Enabled) {
		err = nil // the user asked for offline; missing online templates is expected
	}
	e.SetOnline(res, err)
}

// SetOnline merges a fetched online catalog.
func (e *Engine) SetOnline(res catalog.OnlineResult, err error) {
	e.Online = res
	e.OnlineOK = err == nil && len(res.Templates) > 0
	switch {
	case err != nil:
		e.Warnings = append(e.Warnings, fmt.Sprintf("online templates unavailable, using built-in only: %v", err))
	case res.Stale && res.FetchErr != nil:
		e.Warnings = append(e.Warnings, fmt.Sprintf("using cached online templates from %s: %v", res.FetchedAt.Local().Format("2006-01-02"), res.FetchErr))
	}
	e.rebuild()
}

func (e *Engine) rebuild() {
	opts := catalog.Options{Aliases: e.Cfg.Aliases, Presets: e.Cfg.Presets}
	online := e.Online.Templates
	if e.Cfg.Online.Prefer == "online" {
		e.Catalog = catalog.New(opts, e.builtin, online, e.local)
	} else {
		e.Catalog = catalog.New(opts, online, e.builtin, e.local)
	}
}

// Build is a composed selection.
type Build struct {
	Templates []*catalog.Template
	Keys      []string
	Body      string // rules without block markers
	Block     string // Body wrapped in markers
	Dropped   int
}

// BuildOptions controls composition.
type BuildOptions struct {
	Dedupe     bool
	SkipAlways bool // leave out generate.always
}

// Build resolves names (plus generate.always) and composes them.
func (e *Engine) Build(names []string, o BuildOptions) (Build, error) {
	all := slices.Clone(names)
	if !o.SkipAlways {
		all = append(all, e.Cfg.Generate.Always...)
	}
	ts, err := e.Catalog.Resolve(all)
	if err != nil {
		var ue *catalog.UnknownError
		if errors.As(err, &ue) && !e.OnlineOK {
			return Build{}, fmt.Errorf("%w; online templates aren't loaded, so it may exist there (retry without --offline, or check gignore doctor)", err)
		}
		return Build{}, err
	}
	if len(ts) == 0 {
		return Build{}, errors.New("no templates selected")
	}
	res := compose.Compose(ts, compose.Options{Dedupe: o.Dedupe})
	keys := catalog.Keys(ts)
	return Build{Templates: ts, Keys: keys, Body: res.Text, Block: block.Render(keys, res.Text), Dropped: res.Dropped}, nil
}

// Plan is the result of applying a build to a file, before writing it.
type Plan struct {
	Path    string
	Old     string
	New     string
	Exists  bool
	Changed bool
}

// PlanWrite computes the new content of path without touching the disk.
func PlanWrite(path, blk string, mode block.Mode) (Plan, error) {
	p := Plan{Path: path}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		p.Exists = true
		p.Old = string(b)
	case !errors.Is(err, os.ErrNotExist):
		return p, err
	}
	out, err := block.Apply(p.Old, blk, mode)
	if err != nil {
		return p, fmt.Errorf("%s: %w (fix the markers by hand or rerun with --replace)", path, err)
	}
	p.New = out
	p.Changed = out != p.Old
	return p, nil
}

// Write saves the plan atomically, keeping the original file mode.
func (p Plan) Write() error {
	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p.Path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".gitignore-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(p.New); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, p.Path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// ReadSelection returns the template keys recorded in path's gignore block.
func ReadSelection(path string) ([]string, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	d, err := block.Parse(string(b))
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return d.Templates, d.Found, nil
}
