package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

type entryDoneMsg struct {
	tok     int64
	entry   mothdex.Entry
	choices []app.LinkChoice
	err     error
}

// pendingDownload is a download waiting for the "Already downloaded. Download again?" answer.
type pendingDownload struct {
	req  app.DownloadRequest
	text string // the question, already translated
}

// entryScreen shows one entry and its ranked links; the link list is the link chooser (tui.md section 4).
type entryScreen struct {
	slugs   []string // the entries of the Results list, for the left and right keys
	pos     int
	st      loadState
	entry   mothdex.Entry
	choices []app.LinkChoice
	cur     int // the LinkChoice.Index under the cursor (the number --link takes)
	fail    failure
	tok     int64
	cancel  context.CancelFunc
	confirm *pendingDownload
	prefs   *prefOverlay
}

func newEntry(slugs []string, pos int) entryScreen {
	return entryScreen{slugs: slugs, pos: pos, st: loadLoading}
}

func (entryScreen) id() screenID              { return scEntry }
func (s entryScreen) loading() bool           { return s.st == loadLoading }
func (s entryScreen) title(rt *shared) string { return rt.T("ui.tui.screen.entry", nil) }

func (s entryScreen) focus() focusKind {
	switch {
	case s.confirm != nil:
		return focusPrompt
	case s.prefs != nil:
		return focusText
	}
	return focusNone
}

func (s entryScreen) slug() string {
	if s.pos >= 0 && s.pos < len(s.slugs) {
		return s.slugs[s.pos]
	}
	return ""
}

func (s entryScreen) init(rt *shared) (screen, tea.Cmd) { return s.load(rt) }

// load fetches the entry and its links ranked by the active preference.
func (s entryScreen) load(rt *shared) (entryScreen, tea.Cmd) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(rt.ctx)
	s.cancel, s.tok, s.st = cancel, rt.token(), loadLoading
	tok, ref, svc, prefs := s.tok, app.Ref{Slug: s.slug()}, rt.svc, rt.prefs
	return s, func() tea.Msg {
		res, err := svc.Entry(ctx, ref)
		if err != nil {
			return entryDoneMsg{tok: tok, err: err}
		}
		choices, err := svc.Links(ctx, ref, app.LinkSelector{Prefer: prefs})
		return entryDoneMsg{tok: tok, entry: res.Value, choices: choices, err: err}
	}
}

func (s entryScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case entryDoneMsg:
		if msg.tok != s.tok {
			return s, nil
		}
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return s, nil
			}
			s.st, s.fail = loadError, rt.failureOf(msg.err)
			return s, nil
		}
		s.st, s.entry, s.choices = loadReady, msg.entry, msg.choices
		s.cur = defaultChoice(msg.choices)
		return s, nil
	case prefsMsg:
		if s.st == loadReady { // a new preference ranks the links again
			return s.load(rt)
		}
		return s, nil
	case keyEv:
		return s.onKey(msg, rt)
	}
	return s, nil
}

// defaultChoice is the link the ranking puts first among those that can be downloaded.
func defaultChoice(choices []app.LinkChoice) int {
	for _, c := range choices {
		if c.Available {
			return c.Index
		}
	}
	if len(choices) > 0 {
		return choices[0].Index
	}
	return 0
}

// cursorPos is the position of the cursor in the list.
func (s entryScreen) cursorPos() int {
	for i, c := range s.choices {
		if c.Index == s.cur {
			return i
		}
	}
	return 0
}

func (s entryScreen) onKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	switch {
	case s.confirm != nil:
		return s.answer(ev.a, rt)
	case s.prefs != nil:
		return s.onPrefsKey(ev)
	case s.st == loadLoading:
		if ev.a == actBack {
			if s.cancel != nil {
				s.cancel()
			}
			return s, popCmd()
		}
		return s, nil
	}
	switch ev.a {
	case actBack:
		return s, popCmd()
	case actRetry:
		if s.st == loadError {
			return s.load(rt)
		}
	case actUp, actDown:
		return s.moveCursor(ev.a == actDown), nil
	case actEnter, actDownload:
		return s.downloadLink(rt, s.cur)
	case actDownloadBest:
		return s.downloadLink(rt, 0)
	case actCover:
		return s, s.coverCmd(rt)
	case actPrefer:
		o := newPrefOverlay(rt.prefs)
		s.prefs = &o
	case actPrevEntry, actNextEntry:
		return s.step(ev.a == actNextEntry, rt)
	}
	return s, nil
}

func (s entryScreen) moveCursor(down bool) entryScreen {
	n := len(s.choices)
	if n == 0 {
		return s
	}
	i := s.cursorPos()
	if down {
		i++
	} else {
		i--
	}
	s.cur = s.choices[max(0, min(i, n-1))].Index
	return s
}

// step moves to the previous or next entry of the Results list without going back.
func (s entryScreen) step(forward bool, rt *shared) (screen, tea.Cmd) {
	next := s.pos - 1
	if forward {
		next = s.pos + 1
	}
	if next < 0 || next >= len(s.slugs) {
		return s, nil
	}
	s.pos, s.entry, s.choices = next, mothdex.Entry{}, nil
	return s.load(rt)
}

// downloadLink queues the link with that number, or the best one when index is 0. A file the history
// says was saved asks first (tui.md section 4).
func (s entryScreen) downloadLink(rt *shared, index int) (screen, tea.Cmd) {
	if len(s.choices) == 0 {
		return s, toastCmd(rt.T("ui.tui.entry.no_links", nil), toastWarn)
	}
	sel := app.LinkSelector{Index: index, Prefer: rt.prefs}
	req := app.DownloadRequest{Ref: app.Ref{Slug: s.slug()}, Link: sel}
	which := index
	if which == 0 {
		which = defaultChoice(s.choices)
	}
	var size int64
	for _, c := range s.choices {
		if c.Index == which {
			size = c.Link.Size
		}
	}
	switch {
	case rt.done[doneKey(s.slug(), which)]:
		s.confirm = &pendingDownload{req: req, text: rt.T("ui.tui.entry.again", nil)}
		return s, nil
	case needsConfirm(rt.confirm, 1, size):
		s.confirm = &pendingDownload{req: req, text: rt.N("ui.tui.confirm_download", 1, i18n.Args{"size": humanSize(size)})}
		return s, nil
	}
	return s, enqueueCmd(rt.env, []app.DownloadRequest{req})
}

// answer reads the y/N of the "download again" question.
func (s entryScreen) answer(a action, rt *shared) (screen, tea.Cmd) {
	req := s.confirm.req
	s.confirm = nil
	if a == actYes {
		return s, enqueueCmd(rt.env, []app.DownloadRequest{req})
	}
	return s, nil
}

// coverCmd saves the cover next to the downloads and says where (ui.md section 5, D1).
func (s entryScreen) coverCmd(rt *shared) tea.Cmd {
	e, slug := rt.env, s.slug()
	return func() tea.Msg {
		path, err := e.svc.SaveBoxart(e.ctx, app.Ref{Slug: slug}, "")
		switch {
		case err == nil:
			return toastMsg{text: e.T("ui.tui.entry.cover_saved", i18n.Args{"path": clean(path)}), kind: toastInfo}
		case isNotFound(err):
			return toastMsg{text: e.T("ui.tui.entry.no_cover", nil), kind: toastWarn}
		}
		f := e.failureOf(err)
		return toastMsg{text: f.msg + " [" + f.code + "]", kind: toastError}
	}
}

func isNotFound(err error) bool {
	var ae *app.Error
	return errors.As(err, &ae) && ae.Code == app.CodeNotFound
}

// prefOverlay is the P form: lang, format, source and size, for this run only (tui.md section 4).
type prefOverlay struct {
	in  [4]textinput.Model
	at  int
	bad bool
}

var prefLabels = [4]string{"lang", "format", "source", "size"}

func newPrefOverlay(p app.Preference) prefOverlay {
	var o prefOverlay
	vals := [4]string{strings.Join(p.Languages, ","), strings.Join(p.Formats, ","), strings.Join(p.Sources, ","), ""}
	if p.Size != app.SizeNone {
		vals[3] = string(p.Size)
	}
	for i := range o.in {
		o.in[i] = textinput.New()
		o.in[i].Prompt = ""
		o.in[i].SetWidth(28)
		o.in[i].SetValue(vals[i])
	}
	o.in[0].Focus()
	return o
}

// preference reads the form: ok is false when size is not none, smallest or largest.
func (o prefOverlay) preference() (app.Preference, bool) {
	size := strings.ToLower(strings.TrimSpace(o.in[3].Value()))
	switch size {
	case "", "none":
		size = string(app.SizeNone)
	case "smallest", "largest":
	default:
		return app.Preference{}, false
	}
	return app.Preference{
		Languages: splitIDs(o.in[0].Value()), Formats: splitIDs(o.in[1].Value()), Sources: splitIDs(o.in[2].Value()),
		Size: app.SizePref(size),
	}, true
}

func (s entryScreen) onPrefsKey(ev keyEv) (screen, tea.Cmd) {
	o := *s.prefs
	switch ev.a {
	case actBlur:
		s.prefs = nil
		return s, nil
	case actEnter:
		pref, ok := o.preference()
		if !ok {
			o.bad = true
			s.prefs = &o
			return s, nil
		}
		s.prefs = nil
		return s, func() tea.Msg { return prefsMsg{pref: pref} }
	case actNextField, actPrevField, actDown, actUp:
		o.in[o.at].Blur()
		step := 3
		if ev.a == actNextField || ev.a == actDown {
			step = 1
		}
		o.at = (o.at + step) % len(o.in)
		o.in[o.at].Focus()
	default:
		o.in[o.at], _ = o.in[o.at].Update(ev.k)
		o.bad = false
	}
	s.prefs = &o
	return s, nil
}

func (s entryScreen) view(rt *shared, w, h int) screenView {
	st := rt.st
	switch s.st {
	case loadLoading:
		return screenView{body: []string{st.spin(rt.spin) + " " + rt.T("ui.tui.loading", nil)}}
	case loadError:
		return screenView{body: errorBody(rt, s.fail), status: rt.T("ui.tui.results.retry_help", nil)}
	}
	if s.prefs != nil {
		return s.prefsView(rt)
	}
	e := s.entry
	lines := []string{
		render(st.heading, clean(e.Title)),
		rt.T("ui.tui.entry.facts", i18n.Args{"platform": orDash(clean(e.Platform)), "regions": orDash(clean(strings.Join(e.Regions, ","))), "serial": orDash(clean(e.Serial))}),
		"",
	}
	head := rt.T("ui.cli.info.links", nil)
	if sum := preferSummary(rt.prefs); sum != "" {
		right := rt.T("ui.tui.entry.ranked_by", i18n.Args{"prefs": sum})
		head += strings.Repeat(" ", max(w-cells(head)-cells(right), 2)) + render(st.dim, right)
	}
	lines = append(lines, head)
	if len(s.choices) == 0 {
		lines = append(lines, rt.T("ui.tui.entry.no_links", nil))
	} else {
		lines = append(lines, s.linkLines(rt, w, max(h-len(lines), 1))...)
	}
	sv := screenView{body: lines}
	if s.confirm != nil {
		sv.status, sv.prompt = render(st.warn, s.confirm.text), true
	}
	return sv
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// linkLines draws the ranked links: the cursor, the number with * for the default, the columns, then the
// reasons (hidden with few rows) and a note for a file already downloaded.
func (s entryScreen) linkLines(rt *shared, w, h int) []string {
	st := rt.st
	def := defaultChoice(s.choices)
	var tw, fw, sw, ew, nw int
	for _, c := range s.choices {
		tw, fw = max(tw, cells(clean(c.Link.Type))), max(fw, cells(clean(c.Link.Format)))
		sw, ew = max(sw, cells(clean(c.Link.SizeStr))), max(ew, cells(clean(strings.Join(c.Link.Extras, ","))))
		nw = max(nw, cells(clean(c.Link.SourceName)))
	}
	numW := len(strconv.Itoa(len(s.choices))) + 1
	top := scrollTop(0, s.cursorPos(), h, len(s.choices))
	var out []string
	for i := top; i < len(s.choices) && i < top+h; i++ {
		c := s.choices[i]
		num := strconv.Itoa(c.Index)
		if c.Index == def && c.Available {
			num += "*"
		}
		parts := []string{
			fit(num, numW), fit(clean(c.Link.Type), tw), fit(clean(c.Link.Format), fw), padLeft(clean(c.Link.SizeStr), sw),
			fit(orDash(clean(strings.Join(c.Link.Extras, ","))), ew), fit(clean(c.Link.SourceName), nw),
		}
		line := strings.Join(parts, "  ")
		note := clean(strings.Join(c.Reasons, ", "))
		if !c.Available {
			note = rt.T("ui.tui.entry.unavailable", nil) + " " + note
		}
		if rt.done[doneKey(s.slug(), c.Index)] {
			note = strings.TrimSpace(st.g.ok + " " + rt.T("ui.tui.entry.downloaded", nil) + " " + note)
		}
		if note != "" && rt.tier != tierShort {
			line += "  " + render(st.dim, note)
		}
		if c.Index == s.cur {
			line = render(st.on, ">"+fit(line, max(w-1, 1)))
		} else {
			line = " " + line
		}
		out = append(out, line)
	}
	return out
}

func (s entryScreen) prefsView(rt *shared) screenView {
	o := *s.prefs
	lines := []string{rt.T("ui.tui.prefer.title", nil), ""}
	for i := range o.in {
		label := fit(rt.T("ui.tui.prefer."+prefLabels[i], nil), 10)
		field := "[ " + o.in[i].View() + " ]"
		if i == o.at {
			field = render(rt.st.on, field)
		}
		lines = append(lines, label+field)
	}
	if o.bad {
		lines = append(lines, "", render(rt.st.warn, rt.T("ui.tui.prefer.bad_size", nil)))
	}
	return screenView{body: lines, status: rt.T("ui.tui.prefer.help", nil)}
}
