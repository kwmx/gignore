// Package tui is the interactive template picker.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kwmx/gignore/internal/block"
	"github.com/kwmx/gignore/internal/catalog"
	"github.com/kwmx/gignore/internal/detect"
	"github.com/kwmx/gignore/internal/diffview"
	"github.com/kwmx/gignore/internal/engine"
	"github.com/kwmx/gignore/internal/paths"
	"github.com/kwmx/gignore/internal/search"
	"github.com/kwmx/gignore/internal/ui"
	"github.com/kwmx/gignore/internal/usage"
)

// Options configures a TUI session.
type Options struct {
	Engine      *engine.Engine
	Target      string // file to write
	DisplayPath string // Target, shortened for display
	Global      bool   // Target is the global excludes file
	ProjectRoot string
	Initial     []string // templates named on the command line
	Offline     bool
	Refresh     bool
}

// Result reports what happened, for the CLI to print after the screen clears.
type Result struct {
	Saved string
}

// Run starts the picker and blocks until the user quits.
func Run(ctx context.Context, o Options) (Result, error) {
	m := newModel(ctx, o)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := p.Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		return Result{}, err
	}
	if fm, ok := final.(*model); ok {
		return fm.result, nil
	}
	return Result{}, nil
}

type focus int

const (
	focusList focus = iota
	focusSelected
	focusPreview
)

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayReview
	overlayQuit
)

type onlineState int

const (
	onlineLoading onlineState = iota
	onlineReady
	onlineFailed
	onlineOff
)

type item struct {
	t       *catalog.Template
	matched []int
}

type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusWarn
	statusErr
)

// Messages.
type (
	onlineMsg struct {
		res catalog.OnlineResult
		err error
	}
	clearStatusMsg struct{ id int }
)

type model struct {
	ctx  context.Context
	opts Options
	eng  *engine.Engine

	width, height int
	pal           ui.Palette
	themeSet      bool

	focus   focus
	overlay overlay

	query     string
	mode      search.Mode
	searchErr string
	items     []item
	cursor    int
	offset    int

	selected  []string // template keys, in output order
	selCursor int
	touched   bool // the user changed the selection

	detected map[string]detect.Match
	recent   map[string]bool
	usage    *usage.Store

	build     engine.Build
	buildErr  error
	missing   []string // selected keys no loaded source provides
	plan      engine.Plan
	planErr   error
	diff      string
	previewLn []string
	pOffset   int

	savePath    string
	reviewLn    []string
	rOffset     int
	online      onlineState
	onlineNote  string
	status      string
	statusKind  statusKind
	statusID    int
	result      Result
	initialNote string
}

func newModel(ctx context.Context, o Options) *model {
	cfg := o.Engine.Cfg
	m := &model{
		ctx:      ctx,
		opts:     o,
		eng:      o.Engine,
		pal:      ui.PaletteFor(cfg.TUI.Theme, true),
		themeSet: cfg.TUI.Theme != "auto",
		detected: map[string]detect.Match{},
		recent:   map[string]bool{},
		usage:    usage.Open(usage.Path(paths.StateDir())),
		savePath: o.DisplayPath,
	}
	if mode, err := search.ParseMode(cfg.TUI.SearchMode); err == nil {
		m.mode = mode
	}
	for _, k := range m.usage.Top(8) {
		m.recent[k] = true
	}
	if !o.Global && o.ProjectRoot != "" {
		if ms, err := detect.Detect(o.ProjectRoot, detect.Options{MaxDepth: cfg.Generate.DetectDepth, IncludeOS: cfg.TUI.ShowOS}); err == nil {
			for _, d := range ms {
				m.detected[d.Template] = d
			}
		}
	}
	m.online = onlineLoading
	m.initialSelection()
	m.refreshItems()
	m.recompute()
	return m
}

// initialSelection picks the starting templates: command-line arguments, then
// the file's existing block, then the project default, then detection.
func (m *model) initialSelection() {
	cfg := m.eng.Cfg
	add := func(keys ...string) {
		for _, k := range keys {
			if t, ok := m.eng.Catalog.Get(k); ok {
				k = t.Key
			} else {
				k = catalog.Normalize(k)
			}
			if k != "" && !slices.Contains(m.selected, k) {
				m.selected = append(m.selected, k)
			}
		}
	}
	recorded, found, err := engine.ReadSelection(m.opts.Target)
	switch {
	case err != nil:
		m.setStatus(statusErr, err.Error())
	case len(m.opts.Initial) > 0:
		for _, n := range m.opts.Initial {
			add(strings.Split(n, ",")...)
		}
		m.touched = true
	case found:
		add(recorded...)
		m.initialNote = fmt.Sprintf("Loaded %d templates from %s", len(recorded), m.opts.DisplayPath)
		return
	case len(cfg.Generate.Templates) > 0:
		add(cfg.Generate.Templates...)
		m.initialNote = "Loaded the project's default templates from .gignore.toml"
	case m.opts.Global:
		if t := detect.OSTemplate(); t != "" {
			add(t)
		}
	case cfg.TUI.Detect && len(m.detected) > 0:
		keys := m.orderedDetected()
		add(keys...)
		m.initialNote = fmt.Sprintf("Detected %s. Press ctrl+x to clear", strings.Join(keys, ", "))
	}
	add(cfg.Generate.Always...)
}

func (m *model) orderedDetected() []string {
	var keys []string
	for k := range m.detected {
		if _, ok := m.eng.Catalog.Get(k); ok {
			keys = append(keys, k)
		}
	}
	order := map[string]int{"project": 0, "editor": 1, "os": 2}
	slices.SortFunc(keys, func(a, b string) int {
		if d := order[m.detected[a].Kind] - order[m.detected[b].Kind]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return keys
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadOnline()}
	if !m.themeSet {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	if m.initialNote != "" && m.status == "" {
		m.setStatus(statusInfo, m.initialNote)
	}
	return tea.Batch(cmds...)
}

func (m *model) loadOnline() tea.Cmd {
	e := m.eng
	lo := engine.LoadOptions{Offline: m.opts.Offline, Refresh: m.opts.Refresh}
	ctx := m.ctx
	return func() tea.Msg {
		res, err := e.FetchOnline(ctx, lo)
		return onlineMsg{res, err}
	}
}

// refreshItems re-filters the template list for the current query and mode.
func (m *model) refreshItems() {
	all := m.eng.Catalog.All()
	var cur string
	if m.cursor < len(m.items) {
		cur = m.items[m.cursor].t.Key
	}
	m.items = m.items[:0]
	m.searchErr = ""
	if strings.TrimSpace(m.query) == "" {
		// Detected, then recently used, then everything else alphabetically.
		rank := func(t *catalog.Template) int {
			switch {
			case m.detected[t.Key].Template != "":
				return 0
			case m.recent[t.Key]:
				return 1
			}
			return 2
		}
		for _, t := range all {
			m.items = append(m.items, item{t: t})
		}
		slices.SortStableFunc(m.items, func(a, b item) int { return rank(a.t) - rank(b.t) })
	} else {
		names := make([]string, len(all))
		for i, t := range all {
			names[i] = t.Key
		}
		hits, err := search.Filter(names, m.query, m.mode)
		if err != nil {
			m.searchErr = err.Error()
		}
		for _, h := range hits {
			m.items = append(m.items, item{t: all[h.Index], matched: h.Matched})
		}
	}
	m.cursor = 0
	if strings.TrimSpace(m.query) == "" && cur != "" {
		for i, it := range m.items {
			if it.t.Key == cur {
				m.cursor = i
				break
			}
		}
	}
	m.offset = 0
	m.clampList()
}

// recompute rebuilds the preview and diff after the selection changes.
func (m *model) recompute() {
	m.missing = nil
	var known []string
	for _, k := range m.selected {
		if _, ok := m.eng.Catalog.Get(k); ok {
			known = append(known, k)
		} else {
			m.missing = append(m.missing, k)
		}
	}
	m.build, m.buildErr = engine.Build{}, nil
	m.plan, m.planErr, m.diff = engine.Plan{}, nil, ""
	if len(known) > 0 {
		m.build, m.buildErr = m.eng.Build(known, engine.BuildOptions{Dedupe: m.eng.Cfg.Generate.Dedupe, SkipAlways: true})
	}
	if m.buildErr == nil && len(known) > 0 {
		m.plan, m.planErr = engine.PlanWrite(m.targetPath(), m.build.Block, block.Merge)
		if m.planErr == nil {
			m.diff = diffview.Unified(m.savePath, m.plan.Old, m.plan.New)
		}
	}
	m.previewLn = nil
	if m.build.Body != "" {
		m.previewLn = strings.Split(strings.TrimRight(ui.HighlightGitignore(m.pal, m.build.Block), "\n"), "\n")
	}
	m.clampPreview()
	m.selCursor = min(m.selCursor, max(len(m.selected)-1, 0))
}

func (m *model) targetPath() string {
	if m.savePath == m.opts.DisplayPath || m.savePath == "" {
		return m.opts.Target
	}
	return expandPath(m.savePath)
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func (m *model) toggle(key string) {
	if i := slices.Index(m.selected, key); i >= 0 {
		m.selected = slices.Delete(m.selected, i, i+1)
		m.setStatus(statusInfo, "Removed "+key)
	} else {
		m.selected = append(m.selected, key)
		m.setStatus(statusInfo, "Added "+key)
	}
	m.touched = true
	m.recompute()
}

func (m *model) setStatus(kind statusKind, msg string) tea.Cmd {
	m.statusID++
	m.status, m.statusKind = msg, kind
	id := m.statusID
	if kind == statusErr {
		return nil // errors stay until replaced
	}
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{id} })
}

// save writes the plan, records usage, and reports the result.
func (m *model) save() tea.Cmd {
	if len(m.missing) > 0 {
		return m.setStatus(statusErr, fmt.Sprintf("Can't save: %s not available. Remove it or go online.", strings.Join(m.missing, ", ")))
	}
	if len(m.selected) == 0 {
		return m.setStatus(statusWarn, "Select at least one template first")
	}
	if m.buildErr != nil || m.planErr != nil {
		return m.setStatus(statusErr, errors.Join(m.buildErr, m.planErr).Error())
	}
	if !m.plan.Changed {
		m.overlay = overlayNone
		return m.setStatus(statusOK, m.savePath+" is already up to date")
	}
	if err := m.plan.Write(); err != nil {
		return m.setStatus(statusErr, "Save failed: "+err.Error())
	}
	_ = m.usage.Record(m.build.Keys...) // usage ranking is a convenience; ignore write errors
	added, removed := diffview.Stats(m.diff)
	verb := "Updated"
	if !m.plan.Exists {
		verb = "Created"
	}
	msg := fmt.Sprintf("%s %s (+%d −%d) with %s", verb, m.savePath, added, removed, strings.Join(m.build.Keys, ", "))
	m.result.Saved = msg
	m.touched = false
	m.overlay = overlayNone
	m.recompute()
	return m.setStatus(statusOK, msg)
}

func (m *model) dirty() bool {
	return m.touched && m.plan.Changed
}
