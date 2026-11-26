package gui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// entryState is the Entry screen with its link chooser (docs/ux/gui.md section 5.7).
type entryState struct {
	slug, title string
	phase       loadPhase
	fail        failure
	since       time.Time
	entry       mothdex.Entry
	links       []app.LinkChoice // available links first, in rank order, then the unavailable ones
	focus       int              // index in links
	why         bool
	seq         int
	cancel      context.CancelFunc // cancels the call that loads the entry
}

type entryMsg struct {
	seq   int
	entry mothdex.Entry
	links []app.LinkChoice
	err   error
}

// openEntry shows the Entry screen for a Results row and loads the entry and its ranked links.
func (m Model) openEntry(row resultRow, now time.Time) (Model, []Cmd) {
	m = m.enter(screenEntry)
	return m.loadEntry(row.slug, row.title, now)
}

func (m Model) loadEntry(slug, title string, now time.Time) (Model, []Cmd) {
	if m.entry.cancel != nil { // a fast run of L and R must not leave each earlier call running to its timeout
		m.entry.cancel()
	}
	seq := m.entry.seq + 1
	m.entry = entryState{slug: slug, title: title, phase: loadBusy, since: now, seq: seq, why: m.entry.why}
	m.coverSlug, m.coverSince = slug, now
	svc, prefer := m.o.Service, m.o.Prefer
	ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
	m.entry.cancel = cancel
	// The platform name comes from the Platforms list: load it once into the picker's cache if it is not there.
	m, names := m.loadCatalog(pickPlatform, now)
	return m, append(names, func() Msg {
		defer cancel()
		ref := app.Ref{Slug: slug}
		res, err := svc.Entry(ctx, ref)
		if err != nil {
			return entryMsg{seq: seq, err: err}
		}
		links, err := svc.Links(ctx, ref, app.LinkSelector{Prefer: prefer})
		return entryMsg{seq: seq, entry: res.Value, links: links, err: err}
	})
}

func (m Model) onEntry(msg entryMsg) Model {
	s := m.entry
	if msg.seq != s.seq || !m.onStack(screenEntry) {
		return m
	}
	if msg.err != nil {
		s.phase, s.fail = loadFailed, m.reportOr(msg.err, "entry")
		m.entry = s
		return m
	}
	links := slices.Clone(msg.links)
	slices.SortStableFunc(links, func(a, b app.LinkChoice) int { // unavailable links go last
		switch {
		case a.Available == b.Available:
			return 0
		case a.Available:
			return -1
		}
		return 1
	})
	s.entry, s.links, s.phase, s.focus = msg.entry, links, loadOK, 0
	if t := app.SanitizeText(msg.entry.Title); t != "" {
		s.title = t
	}
	m.entry = s
	return m
}

func (s entryState) available() int {
	n := 0
	for _, l := range s.links {
		if l.Available {
			n++
		}
	}
	return n
}

func (m Model) onEntryAction(a input.Action, now time.Time) (Model, []Cmd) {
	s := m.entry
	if s.phase != loadOK {
		switch a {
		case input.Confirm:
			if s.phase == loadFailed {
				return m.loadEntry(s.slug, s.title, now)
			}
		case input.Back:
			return m.cancelEntry().leave(), nil
		}
		return m, nil
	}
	switch a {
	case input.Up:
		s.focus = max(s.focus-1, 0)
	case input.Down:
		s.focus = min(s.focus+1, max(s.available()-1, 0)) // the unavailable links are skipped
	case input.Info:
		s.why = !s.why
	case input.Confirm:
		if s.focus < s.available() {
			return m.downloadLink(s.links[s.focus], now)
		}
	case input.Download:
		if s.available() > 0 {
			return m.downloadLink(s.links[0], now) // the default link, without moving focus
		}
	case input.PageUp:
		return m.neighbour(-1, now)
	case input.PageDown:
		return m.neighbour(1, now)
	case input.Back:
		return m.cancelEntry().leave(), nil
	}
	m.entry = s
	return m, nil
}

// cancelEntry stops the call that loads the entry and makes its answer stale.
func (m Model) cancelEntry() Model {
	if m.entry.cancel != nil {
		m.entry.cancel()
	}
	m.entry.seq++
	return m
}

// neighbour is L or R: the previous or next entry of the Results list.
func (m Model) neighbour(dir int, now time.Time) (Model, []Cmd) {
	rows := m.results.win.rows
	i := m.results.list.focus + dir
	if i < 0 || i >= len(rows) {
		return m, nil
	}
	m.results.list = m.results.list.moved(dir, m.resultsVisible(), len(rows))
	m, cmds := m.loadEntry(rows[i].slug, rows[i].title, now)
	m, more := m.fetchMore()
	return m, append(cmds, more...)
}

// downloadLink queues one link, asking first when it was downloaded before or is above 500 MB.
func (m Model) downloadLink(l app.LinkChoice, now time.Time) (Model, []Cmd) {
	d := download{slug: m.entry.slug, title: m.entry.title, platform: m.entry.entry.Platform, link: l.Index, size: l.Link.Size}
	if m.isDownloaded(d.slug, l.Index) {
		return m.openDialog(dialogState{
			kind: dialogDownloadAgain, pending: d,
			title: m.tr("ui.gui.confirm.again", nil),
			buttons: []dialogButton{
				{btnCancel, m.tr("ui.gui.dialog.cancel", nil)},
				{btnAgain, m.tr("ui.gui.dialog.again", nil)},
			},
		}, now), nil
	}
	return m.requestDownload(d, now)
}

func (m Model) platformName(id string) string {
	for _, p := range m.platforms.items {
		if p.ID == id {
			return app.SanitizeText(p.Name)
		}
	}
	return app.SanitizeText(id)
}

func (m Model) entryFrame() frameSpec {
	s := m.entry
	spec := frameSpec{crumbs: []string{m.tr("ui.gui.screen.entry", nil)}}
	back := m.tr("ui.gui.hint.back", nil)
	switch {
	case s.phase == loadFailed:
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.retry", nil)}, {role: input.Back, label: back}}
	case s.phase != loadOK:
		spec.hints = []hint{{role: input.Back, label: back}}
	case s.available() == 0:
		spec.hints = []hint{{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.neighbour", nil), optional: true}, {role: input.Back, label: back}}
	default:
		spec.hints = []hint{
			{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.neighbour", nil), optional: true},
			{role: input.Confirm, label: m.tr("ui.gui.hint.download_this", nil)},
			{role: input.Download, label: m.tr("ui.gui.hint.download_best", nil)},
			{role: input.Info, label: m.tr("ui.gui.hint.why", nil)},
			{role: input.Back, label: back},
		}
	}
	return spec
}

func (m Model) drawEntry(p painter, now time.Time) {
	met := m.metrics
	s := m.entry
	c := met.ContentRect()
	switch s.phase {
	case loadBusy:
		m.drawLoading(p, c, m.tr("ui.gui.loading", nil), s.since, now)
		return
	case loadFailed:
		m.drawFailure(p, c, s.fail, true)
		return
	}
	u := met.Unit
	right := c
	if met.Tier != Compact && !met.Narrow {
		w := c.W * 30 / 100
		m.drawCover(p, draw.Rect{X: c.X, Y: c.Y, W: w, H: min(c.H, w*4/3)}, s.slug, s.entry.Platform)
		right = draw.Rect{X: c.X + w + 2*u, Y: c.Y, W: c.W - w - 2*u, H: c.H}
	}
	y := right.Y
	for _, line := range p.wrap(met.Title, s.title, right.W, 3) {
		p.text(right.X, y, met.Title, colText, line)
		y += LineHeight(met.Title)
	}
	platform := app.SanitizeText(s.entry.Platform)
	name := m.platformName(s.entry.Platform)
	if name == platform { // the platform list is not loaded: the id says it all
		name = ""
	}
	meta := strings.Join(nonEmpty(platform, name, app.SanitizeText(strings.Join(s.entry.Regions, ",")), serialText(s.entry.Serial)), " · ")
	p.text(right.X, y, met.Secondary, colDim, p.fit(met.Secondary, meta, right.W))
	y += LineHeight(met.Secondary) + u

	if len(s.links) == 0 {
		p.text(right.X, y, met.Body, colWarn, m.tr("ui.gui.entry.no_links", nil))
		return
	}
	p.text(right.X, y, met.Secondary, colDim, m.tr("ui.gui.entry.links", nil))
	y += LineHeight(met.Secondary) + u/2
	for i, l := range s.links {
		h := met.Row1
		if s.why && len(l.Reasons) > 0 {
			h += LineHeight(met.Secondary)
		}
		if y+h > right.Bottom() {
			break
		}
		r := draw.Rect{X: right.X, Y: y, W: right.W, H: h}
		box := r.Inset(met.Ring)
		if i == s.focus && l.Available {
			p.box(box, true)
		}
		m.drawLinkRow(p, box, i, l)
		y += h + met.Ring
	}
}

func serialText(s string) string {
	if s == "" {
		return ""
	}
	return "serial " + app.SanitizeText(s)
}

func (m Model) drawLinkRow(p painter, r draw.Rect, i int, l app.LinkChoice) {
	met := m.metrics
	color := colText
	if !l.Available {
		color = colDim
	}
	top := r.Y + (met.Row1-2*met.Ring-LineHeight(met.Body))/2
	x := r.X + met.Unit
	if i == 0 && l.Available { // the default link carries the accent star
		p.text(x, top, met.Body, colAccent, "*")
	}
	x += p.width(met.Body, "*") + met.Unit
	extras := strings.Join(l.Link.Extras, ",")
	cols := nonEmpty(app.SanitizeText(l.Link.Type), app.SanitizeText(l.Link.Format), app.SanitizeText(l.Link.SizeStr), app.SanitizeText(extras), app.SanitizeText(l.Link.SourceName))
	text := strings.Join(cols, "   ")
	if m.isDownloaded(m.entry.slug, l.Index) {
		drawCheck(p.l, p.m.Pixel, r.Right()-met.Unit-LineHeight(met.Body)+4, top, LineHeight(met.Body)-6, colOK)
	}
	if !l.Available {
		text += "   " + m.tr("ui.gui.entry.unavailable", nil)
	}
	p.text(x, top, met.Body, color, p.fit(met.Body, text, r.Right()-x-LineHeight(met.Body)-2*met.Unit))
	if m.entry.why && len(l.Reasons) > 0 {
		why := p.fit(met.Secondary, app.SanitizeText(strings.Join(l.Reasons, "; ")), r.W-2*met.Unit)
		p.text(x, top+LineHeight(met.Body), met.Secondary, colDim, why)
	}
}
