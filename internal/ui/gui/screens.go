package gui

import (
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// spinnerShown is true once a wait has lasted long enough to show a spinner: cached answers never flash it.
func spinnerShown(since, now time.Time) bool { return now.Sub(since) >= spinnerAfter }

// drawSplash draws the Splash of docs/ux/gui.md section 5.1: the name, "Starting…" with a spinner, and the
// version and profile at the bottom right. No bars: it is the first frame.
func (m Model) drawSplash(p painter, now time.Time) {
	met := m.metrics
	s := met.Safe
	u := met.Unit
	titleTop := s.Y + s.H*36/100
	p.centered(rect{X: s.X, Y: titleTop, W: s.W, H: LineHeight(met.Title)}, met.Title, colText, brand)

	line := m.tr("ui.gui.splash.starting", nil)
	top := titleTop + LineHeight(met.Title) + u
	w := p.width(met.Body, line)
	x := s.X + (s.W-w)/2
	p.text(x, top, met.Body, colDim, line)
	if spinnerShown(m.start, now) {
		r := LineHeight(met.Body) / 3
		p.spinner(x-u-r, top+LineHeight(met.Body)/2, r, now)
	}

	info := m.o.Version + " · " + app.SanitizeText(m.profile.Name)
	p.rightAligned(s.Right()-2*u, s.Bottom()-LineHeight(met.Secondary)-2*u, LineHeight(met.Secondary), met.Secondary, colDim, info)
}

// exitCounts are the counts the exit screen shows: what Wait reported when it answered, and otherwise what
// the GUI saw of the downloads of this run.
func (m Model) exitCounts() (done, skipped, failed, canceled int) {
	if e := m.exit; e != nil && e.finished && e.res.WaitErr == nil && !e.res.TimedOut {
		b := e.res.Batch
		return b.Done, b.Skipped, b.Failed, b.Cancel
	}
	return m.jobs.summary()
}

func (m Model) exitSummary() string {
	done, skipped, failed, canceled := m.exitCounts()
	var parts []string
	for _, c := range []struct {
		n   int
		key string
	}{{done, "downloaded"}, {skipped, "skipped"}, {failed, "failed"}, {canceled, "canceled"}} {
		if c.n > 0 {
			parts = append(parts, m.tr("ui.gui.exit."+c.key, map[string]any{"count": c.n}))
		}
	}
	return strings.Join(parts, " · ")
}

// drawExit draws the exit screen of section 5.13: what this run downloaded, the game-list refresh while it
// runs (or the notice when the person must do it by hand), and where the person is going.
func (m Model) drawExit(p painter, now time.Time) {
	met := m.metrics
	s := met.Safe
	u := met.Unit
	lh := LineHeight(met.Body)
	y := s.Y + s.H*30/100
	row := func(size int, c draw.Color, text string) {
		p.centered(rect{X: s.X, Y: y, W: s.W, H: LineHeight(size)}, size, c, p.fit(size, text, s.W-4*u))
		y += LineHeight(size) + u
	}

	if summary := m.exitSummary(); summary != "" {
		row(met.Body, colText, summary)
	}
	done, _, _, _ := m.exitCounts()
	e := m.exit
	switch {
	case e != nil && !e.finished && done > 0:
		line := m.tr("ui.gui.exit.refreshing", nil)
		w := p.width(met.Secondary, line)
		x := s.X + (s.W-w)/2
		p.text(x, y, met.Secondary, colDim, line)
		if spinnerShown(e.started, now) {
			r := LineHeight(met.Secondary) / 3
			p.spinner(x+w+u+r, y+LineHeight(met.Secondary)/2, r, now)
		}
		y += LineHeight(met.Secondary) + u
	case e != nil && e.finished && e.res.Batch.RefreshWarning != "":
		for _, line := range p.wrap(met.Secondary, app.SanitizeText(e.res.Batch.RefreshWarning), s.W-8*u, 3) {
			row(met.Secondary, colWarn, line)
		}
	}

	last := m.tr("ui.gui.exit.closing", nil)
	if m.profile.ID != "generic" {
		last = m.tr("ui.gui.exit.returning", map[string]any{"frontend": app.SanitizeText(m.profile.Name)})
	}
	p.centered(rect{X: s.X, Y: s.Bottom() - 6*u - lh, W: s.W, H: lh}, met.Body, colDim, p.fit(met.Body, last, s.W-4*u))
}
