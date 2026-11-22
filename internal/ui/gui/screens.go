package gui

import (
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
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
	// The moth sits 1u before the wordmark and the pair is centered; its wings flap, 250 ms a frame, while the app starts.
	mw, _ := p.mothSize()
	markW := p.width(met.Title, brand)
	markX := s.X + (s.W-(mw+u+markW))/2
	p.drawMoth(markX, titleTop, LineHeight(met.Title), wingFrameAt(now))
	p.text(markX+mw+u, titleTop, met.Title, colAccent, brand)

	line := m.tr("ui.gui.splash.starting", nil)
	top := titleTop + LineHeight(met.Title) + u
	w := p.width(met.Body, line)
	x := s.X + (s.W-w)/2
	p.text(x, top, met.Body, colDim, line)
	if spinnerShown(m.start, now) { // on the right of the text, as on the exit screen
		r := 3 * met.Pixel // half the spinner square
		p.spinner(x+w+u+r, top+LineHeight(met.Body)/2, now)
	}

	infoTop := s.Bottom() - LineHeight(met.Secondary) - 2*u
	p.drawSky([]rect{
		{X: markX, Y: titleTop, W: mw + u + markW, H: LineHeight(met.Title)},
		{X: s.X, Y: top, W: s.W, H: LineHeight(met.Body)},
		{X: s.X, Y: infoTop, W: s.W, H: LineHeight(met.Secondary)},
	})
	info := m.o.Version + " · " + app.SanitizeText(m.profile.Name)
	p.rightAligned(s.Right()-2*u, s.Bottom()-LineHeight(met.Secondary)-2*u, LineHeight(met.Secondary), met.Secondary, colDim, info)
}

// exitCounts are the counts the exit screen shows: what Wait reported when it answered, and otherwise what
// the GUI saw of the downloads of this run.
func (m Model) exitCounts() (done, skipped, failed, canceled, waiting int) {
	if e := m.exit; e != nil && e.finished && e.res.WaitErr == nil && !e.res.TimedOut {
		b := e.res.Batch
		for _, j := range b.Jobs {
			if consent.Needed(j) {
				waiting++
			}
		}
		return b.Done, b.Skipped, max(b.Failed-waiting, 0), b.Cancel, waiting
	}
	return m.jobs.summary()
}

func (m Model) exitSummary() string {
	done, skipped, failed, canceled, waiting := m.exitCounts()
	var parts []string
	for _, c := range []struct {
		n   int
		key string
	}{{done, "downloaded"}, {skipped, "skipped"}, {failed, "failed"}, {waiting, "waiting"}, {canceled, "canceled"}} {
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
	done, _, _, _, _ := m.exitCounts()
	e := m.exit
	switch {
	case e != nil && !e.finished && done > 0:
		line := m.tr("ui.gui.exit.refreshing", nil)
		w := p.width(met.Secondary, line)
		x := s.X + (s.W-w)/2
		p.text(x, y, met.Secondary, colDim, line)
		if spinnerShown(e.started, now) {
			r := 3 * met.Pixel // half the spinner square
			p.spinner(x+w+u+r, y+LineHeight(met.Secondary)/2, now)
		}
		y += LineHeight(met.Secondary) + u
	case e != nil && e.finished && e.res.Batch.RefreshWarning != "":
		for _, line := range p.wrap(met.Secondary, app.SanitizeText(e.res.Batch.RefreshWarning), s.W-8*u, 3) {
			row(met.Secondary, colWarn, line)
		}
	}

	if e != nil && e.finished && e.res.Batch.RefreshWarning != "" {
		m.drawContinueHint(p) // during the notice, one hint tells that A skips the wait
	}

	last := m.tr("ui.gui.exit.closing", nil)
	if m.profile.ID != "generic" {
		last = m.tr("ui.gui.exit.returning", map[string]any{"frontend": app.SanitizeText(m.profile.Name)})
	}
	p.centered(rect{X: s.X, Y: s.Bottom() - 6*u - lh, W: s.W, H: lh}, met.Body, colDim, p.fit(met.Body, last, s.W-4*u))
}

// drawContinueHint draws the single hint of the exit notice, (A) Continue, centered at the bottom of the safe
// rectangle: the exit screen has no hint bar otherwise (docs/ux/gui.md section 5.13).
func (m Model) drawContinueHint(p painter) {
	met := m.metrics
	size := met.Secondary
	h := max(met.HintBar-met.Unit, 20)
	label := input.Hint(input.Confirm, m.pad, m.cfg.confirmEast)
	word := m.tr("ui.gui.hint.continue", nil)
	total := p.pillWidth(h, label) + met.Unit/2 + p.width(size, word)
	x := met.Safe.X + (met.Safe.W-total)/2
	y := met.Safe.Bottom() - met.Unit - h
	x += p.pillOn(x, y, h, label, colSurface) + met.Unit/2 // a disc on the bg screen is drawn in surface
	p.text(x, y+(h-LineHeight(size))/2, size, colDim, word)
}
