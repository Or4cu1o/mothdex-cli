package gui

import (
	"context"
	"errors"
	"image"
	stddraw "image/draw"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// Covers (docs/ux/gui.md sections 5.6 and 5.7, contract section 5): requested through Catalog.Cover 250 ms after
// focus settles on a row, decoded again with the same caps through app.DecodeCover one at a time, scaled to at
// most 512 px, and kept in a memory cache of the last 50. No cover, or a decode error, shows a placeholder.
const (
	coverSettle = 250 * time.Millisecond
	coverSide   = 512
	maxCovers   = 50
)

// coverEntry is a decoded cover, or the fact that it could not be had (so it is not asked for again).
type coverEntry struct {
	img     *image.RGBA
	id      uint64 // names the picture to the backend's texture cache
	failed  bool
	retryAt time.Time // a failure that may pass (network, timeout) is asked again after this; zero is for good
}

// coverCache holds the last maxCovers covers by slug. It is a value: put returns a new cache.
type coverCache struct {
	items map[string]coverEntry
	order []string // oldest first
	next  uint64
}

func (c coverCache) get(slug string) (coverEntry, bool) {
	e, ok := c.items[slug]
	return e, ok
}

// settled reports whether nothing more is to be asked for a slug at time now: it has a cover, or it failed for
// good, or it failed for a reason that may pass and the wait has not ended.
func (c coverCache) settled(slug string, now time.Time) bool {
	e, ok := c.items[slug]
	return ok && (e.retryAt.IsZero() || now.Before(e.retryAt))
}

// coverRetryAfter is how long a cover that failed for a reason that may pass is left alone.
const coverRetryAfter = 30 * time.Second

// permanent reports whether a cover error will not pass on its own: the cover is not there, or it cannot be
// decoded. A canceled request, a timeout or a network error may pass, so they are asked again later.
func permanent(err error) bool {
	var ae *app.Error
	return errors.As(err, &ae) && ae.Code == app.CodeNotFound
}

// put stores a cover, giving it an id, and drops the oldest beyond the limit.
func (c coverCache) put(slug string, img *image.RGBA, failed bool, retryAt time.Time) coverCache {
	items := maps.Clone(c.items)
	if items == nil {
		items = map[string]coverEntry{}
	}
	order := slices.DeleteFunc(slices.Clone(c.order), func(s string) bool { return s == slug })
	c.next++
	items[slug] = coverEntry{img: img, id: c.next, failed: failed, retryAt: retryAt}
	order = append(order, slug)
	for len(order) > maxCovers {
		delete(items, order[0])
		order = order[1:]
	}
	return coverCache{items: items, order: order, next: c.next}
}

type coverMsg struct {
	slug string
	img  *image.RGBA
	err  error
}

// fetchCoverCmd gets and decodes one cover off the render thread.
func fetchCoverCmd(svc Service, slug string) Cmd {
	return func() Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
		defer cancel()
		r, contentType, err := svc.Cover(ctx, app.Ref{Slug: slug})
		if err != nil {
			return coverMsg{slug: slug, err: err}
		}
		defer func() { _ = r.Close() }()
		img, err := app.DecodeCover(io.LimitReader(r, 8<<20), contentType, coverSide)
		if err != nil {
			return coverMsg{slug: slug, err: err}
		}
		b := img.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		stddraw.Draw(rgba, rgba.Bounds(), img, b.Min, stddraw.Src)
		return coverMsg{slug: slug, img: rgba}
	}
}

// coverWanted is the slug whose cover the current screen shows.
func (m Model) coverWanted() string {
	if m.metrics.Tier == Compact || m.metrics.Narrow { // no cover panel there: no request either
		return ""
	}
	switch m.screen {
	case screenResults:
		return m.results.focusedSlug()
	case screenEntry:
		return m.entry.slug
	}
	return ""
}

// noteCoverFocus restarts the settle timer when the focus moved to another entry.
func (m Model) noteCoverFocus(now time.Time) Model {
	if want := m.coverWanted(); want != m.coverSlug {
		m.coverSlug, m.coverSince = want, now
	}
	return m
}

// coverDue reports the cover to request now, if the focus has settled and nothing is in flight.
func (m Model) coverDue(now time.Time) (string, bool) {
	want := m.coverWanted()
	if want == "" || m.coverBusy != "" || want != m.coverSlug {
		return "", false
	}
	if m.covers.settled(want, now) || now.Before(m.coverSince.Add(coverSettle)) {
		return "", false
	}
	return want, true
}

// startCover begins the decode of a due cover.
func (m Model) startCover(now time.Time) (Model, []Cmd) {
	slug, ok := m.coverDue(now)
	if !ok {
		return m, nil
	}
	m.coverBusy = slug
	return m, []Cmd{fetchCoverCmd(m.o.Service, slug)}
}

func (m Model) onCover(msg coverMsg, now time.Time) (Model, []Cmd) {
	if msg.slug == m.coverBusy {
		m.coverBusy = ""
	}
	if msg.err != nil {
		if !canceled(msg.err) {
			m.o.Log.Debug("gui: cover", "slug", msg.slug, "err", msg.err)
		}
		var retryAt time.Time
		if !permanent(msg.err) {
			retryAt = now.Add(coverRetryAfter)
		}
		m.covers = m.covers.put(msg.slug, nil, true, retryAt)
	} else {
		m.covers = m.covers.put(msg.slug, msg.img, false, time.Time{})
	}
	return m.startCover(now)
}

// drawCover draws a cover fitted inside r with its aspect kept, or the placeholder (the platform id on a
// surface) when there is none.
func (m Model) drawCover(p painter, r draw.Rect, slug, platform string) {
	met := m.metrics
	if e, ok := m.covers.get(slug); ok && !e.failed && e.img != nil {
		b := e.img.Bounds()
		w, h := r.W, r.H
		if b.Dx()*h > b.Dy()*w { // wider than the box: fit the width
			h = b.Dy() * w / b.Dx()
		} else {
			w = b.Dx() * h / b.Dy()
		}
		p.l.Image(draw.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: max(w, 1), H: max(h, 1)}, e.id, e.img)
		return
	}
	p.shape(r, colSurface)
	p.centered(r, met.Secondary, colDim, app.SanitizeText(platform))
}
