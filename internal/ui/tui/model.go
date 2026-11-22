package tui

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

const (
	toastFor  = 3 * time.Second        // how long a toast replaces the status bar
	spinEvery = 100 * time.Millisecond // the loading spinner
	warmEvery = time.Second            // the Warming counter (cli-conventions section 5)
	defaultW  = 80                     // the size before the first WindowSizeMsg arrives
	defaultH  = 24
)

// toastKind picks the color role of a toast.
type toastKind int

const (
	toastInfo toastKind = iota
	toastWarn
	toastError
)

type toast struct {
	id   int
	text string
	kind toastKind
}

// tickKind names the two timers the UI runs.
type tickKind int

const (
	tickSpin tickKind = iota
	tickWarm
)

// Messages between the root model and the screens. A screen never changes the stack itself: it
// returns a command that carries one of these.
type (
	pushMsg      struct{ s screen }
	popMsg       struct{}
	popToRootMsg struct{ fill *app.SearchQuery }
	fillMsg      struct{ q app.SearchQuery }
	toastMsg     struct {
		text string
		kind toastKind
	}
	toastExpireMsg struct{ id int }
	eventMsg       struct {
		ev app.Event
		ok bool // false: the channel closed
	}
	dbInfoMsg struct {
		total int64
		ok    bool
	}
	historyMsg   struct{ recs []app.HistoryRecord }
	jobsMsg      struct{ jobs []app.JobSnapshot }
	platformsMsg struct{ names map[string]string }
	prefsMsg     struct{ pref app.Preference }
	enqueuedMsg  struct {
		reqs []app.DownloadRequest
		ids  []app.JobID
		jobs []app.JobSnapshot
		err  error
	}
	tickMsg struct{ kind tickKind }
	// appliedMsg carries the effective settings after a change in the Settings screen.
	appliedMsg struct{ settings []app.Setting }
	// retriedMsg is the answer to a retry from the Queue: the request and the jobs it re-queued.
	retriedMsg struct {
		enq enqueuedMsg
		ids []app.JobID
	}
	// keyEv is a key already mapped to an action by the reducer; the raw key stays for text fields.
	keyEv struct {
		a action
		k tea.KeyPressMsg
	}
)

// shared is what a screen sees of the root model, rebuilt for every call.
type shared struct {
	*env
	w, h    int
	tier    sizeTier
	prefs   app.Preference
	done    map[string]bool
	book    jobBook
	dbTotal int64
	dbKnown bool
	spin    int
	// settings that change while the TUI runs; they shadow the start-up values of env
	pageSize int
	confirm  bool
	boxart   bool
	names    map[string]string
}

// screenView is what a screen draws: the body lines, the text in the separator, and an optional
// status line that replaces the key help (an open prompt, for example).
type screenView struct {
	body   []string
	footer string
	status string
	prompt bool // status is an open [y/N] question: it wins over a toast
}

// screen is one level of the stack. Screens are values: update returns the next one.
type screen interface {
	id() screenID
	focus() focusKind
	loading() bool
	title(rt *shared) string
	init(rt *shared) (screen, tea.Cmd) // starts the screen: it may issue its first request
	update(msg tea.Msg, rt *shared) (screen, tea.Cmd)
	view(rt *shared, w, h int) screenView
}

// model is the root: the screen stack, the frame, the toast, the queue summary and the quit prompt.
type model struct {
	env      *env
	w, h     int
	stack    []screen
	toast    toast
	toastSeq int
	book     jobBook
	done     map[string]bool
	prefs    app.Preference
	dbTotal  int64
	dbKnown  bool
	help     bool
	quitAsk  bool
	pageSize int // the settings the TUI applies at once (interface.page_size ...)
	confirm  bool
	boxart   bool
	names    map[string]string // platform id to name, for the Entry screen
	cfgPrefs map[string]any    // download.prefer.* as last read, to see which one changed
	spin     int
	spinning bool
	warming  bool
	events   <-chan app.Event
	start    tea.Cmd // what the first screen asked for when it started
}

// newModel builds the root with its first screen already started, so Init has nothing to write back.
func newModel(e *env, first screen) model {
	m := model{
		env: e, w: defaultW, h: defaultH, book: newJobBook(),
		done: map[string]bool{}, prefs: e.prefer, events: e.svc.Events(e.ctx),
		pageSize: e.pageSize, confirm: e.confirm, boxart: e.boxart,
	}
	started, cmd := first.init(m.rt())
	m.stack, m.start = []screen{started}, cmd
	return m
}

func (m model) rt() *shared {
	return &shared{
		env: m.env, w: m.w, h: m.h, tier: tier(m.w, m.h), prefs: m.prefs, done: m.done, book: m.book,
		dbTotal: m.dbTotal, dbKnown: m.dbKnown, spin: m.spin, pageSize: m.pageSize, confirm: m.confirm, boxart: m.boxart, names: m.names,
	}
}

func (m model) top() screen { return m.stack[len(m.stack)-1] }

// Init starts the first screen, the event subscription and the two small loads the UI needs.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.start, m.waitEvent(), m.loadDBInfo(), m.loadHistory(), m.loadJobs(), m.loadPlatformNames())
}

func (m model) waitEvent() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		return eventMsg{ev: ev, ok: ok}
	}
}

func (m model) loadDBInfo() tea.Cmd {
	e := m.env
	return func() tea.Msg {
		res, err := e.svc.DBInfo(e.ctx)
		return dbInfoMsg{total: res.Value.TotalEntries, ok: err == nil}
	}
}

// loadHistory reads what was downloaded before, so the Entry screen can say so.
func (m model) loadHistory() tea.Cmd {
	e := m.env
	return func() tea.Msg {
		recs, err := e.svc.History(e.ctx, app.HistoryFilter{})
		if err != nil {
			return historyMsg{}
		}
		return historyMsg{recs: recs}
	}
}

// loadPlatformNames reads the platform list (the service caches it) so the Entry screen can show
// "gbc (Game Boy Color)". A failure leaves the ids alone.
func (m model) loadPlatformNames() tea.Cmd {
	e := m.env
	return func() tea.Msg {
		res, err := e.svc.Platforms(e.ctx, app.PlatformFilter{})
		if err != nil {
			return platformsMsg{}
		}
		names := make(map[string]string, len(res.Value))
		for _, p := range res.Value {
			names[clean(p.ID)] = clean(p.Name)
		}
		return platformsMsg{names: names}
	}
}

// loadJobs seeds the book with the jobs the service already has (none at start, but cheap to ask).
func (m model) loadJobs() tea.Cmd {
	e := m.env
	return func() tea.Msg { return jobsMsg{jobs: e.svc.Jobs()} }
}

// Update is the single entry of messages. It wraps update so that the timers the new state needs are
// always running.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(model)
	nm, ticks := nm.ensureTicks()
	return nm, tea.Batch(cmd, ticks)
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case pushMsg:
		return m.push(msg.s)
	case popMsg:
		return m.pop(), nil
	case popToRootMsg:
		return m.toRoot(msg.fill)
	case toastMsg:
		return m.showToast(msg.text, msg.kind)
	case toastExpireMsg:
		if msg.id == m.toast.id {
			m.toast = toast{}
		}
		return m, nil
	case eventMsg:
		return m.onEvent(msg)
	case enqueuedMsg:
		return m.onEnqueued(msg)
	case retriedMsg:
		next, cmd := m.onEnqueued(msg.enq)
		out, fan := next.(model).fanout(msg)
		return out, tea.Batch(cmd, fan)
	case appliedMsg:
		return m.apply(msg.settings), nil
	case dbInfoMsg:
		m.dbTotal, m.dbKnown = msg.total, msg.ok
		return m, nil
	case historyMsg:
		m.done = withDone(m.done, msg.recs)
		return m, nil
	case platformsMsg:
		if msg.names != nil {
			m.names = msg.names
		}
		return m, nil
	case jobsMsg:
		for _, j := range msg.jobs {
			m.book = m.book.apply(j, m.env.now())
		}
		return m, nil
	case prefsMsg:
		m.prefs = msg.pref
		return m.fanout(msg)
	case tickMsg:
		return m.onTick(msg)
	}
	return m.fanout(msg)
}

// withDone adds the finished downloads of a history to the set of downloaded files.
func withDone(done map[string]bool, recs []app.HistoryRecord) map[string]bool {
	out := maps.Clone(done)
	for _, r := range recs {
		if r.State == string(app.StateDone) {
			out[doneKey(r.Slug, r.LinkIndex)] = true
		}
	}
	return out
}

// fanout hands a message to every screen of the stack: an answer may arrive after another screen was
// opened on top of the one that asked, and each screen ignores what is not its own.
func (m model) fanout(msg tea.Msg) (tea.Model, tea.Cmd) {
	rt := m.rt()
	stack := slices.Clone(m.stack)
	cmds := make([]tea.Cmd, 0, len(stack))
	for i, s := range stack {
		ns, c := s.update(msg, rt)
		stack[i] = ns
		cmds = append(cmds, c)
	}
	m.stack = stack
	return m, tea.Batch(cmds...)
}

func (m model) push(s screen) (tea.Model, tea.Cmd) {
	s, cmd := s.init(m.rt())
	m.stack = append(slices.Clone(m.stack), s)
	return m, cmd
}

func (m model) pop() model {
	if len(m.stack) > 1 {
		m.stack = slices.Clone(m.stack[:len(m.stack)-1])
	}
	return m
}

// toRoot returns to the first screen, optionally filling its search fields.
func (m model) toRoot(fill *app.SearchQuery) (tea.Model, tea.Cmd) {
	m.stack = slices.Clone(m.stack[:1])
	if fill == nil {
		return m, nil
	}
	return m.fanout(fillMsg{q: *fill})
}

func (m model) showToast(text string, kind toastKind) (tea.Model, tea.Cmd) {
	m.toastSeq++
	m.toast = toast{id: m.toastSeq, text: text, kind: kind}
	return m, m.env.after(toastFor, toastExpireMsg{id: m.toast.id})
}

func (m model) toastNow(text string, kind toastKind) (model, tea.Cmd) {
	next, cmd := m.showToast(text, kind)
	return next.(model), cmd
}

func (m model) onEvent(msg eventMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	wasDone := m.book.byID[msg.ev.Job.ID].State == app.StateDone // before this event is applied
	m.book = m.book.apply(msg.ev.Job, m.env.now())
	if msg.ev.Job.State == app.StateDone {
		m.done = maps.Clone(m.done)
		m.done[doneKey(msg.ev.Job.Slug, msg.ev.Job.LinkIndex)] = true
	}
	cmds := []tea.Cmd{m.waitEvent()}
	if msg.ev.Job.State == app.StateDone && m.boxart && !wasDone {
		cmds = append(cmds, m.saveCover(msg.ev.Job))
	}
	if msg.ev.Job.Terminal() { // History reloads when a job ends
		next, c := m.fanout(jobEndedMsg{})
		m = next.(model)
		cmds = append(cmds, c)
	}
	if done, skipped, failed, ok := m.book.settled(); ok {
		m.book = m.book.resetBatch()
		var c tea.Cmd
		m, c = m.toastNow(m.summaryText(done, skipped, failed), summaryKind(failed))
		cmds = append(cmds, c)
	}
	return m, tea.Batch(cmds...)
}

func summaryKind(failed int) toastKind {
	if failed > 0 {
		return toastWarn
	}
	return toastInfo
}

// summaryText is "3 downloaded, 1 skipped, 0 failed".
// saveCover writes the cover next to a finished file when download.boxart is on. The UI decides when
// (a Done job, never after an interrupt); the service decides how (app-service.md, Boxart). A failure is
// a warning, and an entry with no cover is normal.
func (m model) saveCover(j app.JobSnapshot) tea.Cmd {
	e := m.env
	return func() tea.Msg {
		if _, err := e.svc.SaveJobBoxart(e.ctx, j, ""); err != nil && !isNotFound(err) {
			f := e.failureOf(err)
			return toastMsg{text: e.T("ui.cli.boxart.failed", i18n.Args{"title": clean(j.Title), "code": f.code}), kind: toastWarn}
		}
		return nil
	}
}

func (m model) summaryText(done, skipped, failed int) string {
	return m.env.T("ui.tui.summary", i18n.Args{"done": done, "skipped": skipped, "failed": failed})
}

// onEnqueued remembers the jobs of a download request, and tells the user.
func (m model) onEnqueued(msg enqueuedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		f := m.env.failureOf(msg.err)
		return m.showToast(f.msg+" ["+f.code+"]", toastError)
	}
	m.book = m.book.withRequests(msg.ids, msg.reqs)
	for _, j := range msg.jobs {
		m.book = m.book.apply(j, m.env.now())
	}
	label := m.env.N("ui.tui.queued_files", len(msg.ids), nil)
	if len(msg.ids) == 1 && len(msg.jobs) > 0 {
		j := msg.jobs[0]
		label = m.env.T("ui.tui.queued_one", i18n.Args{"title": clean(j.Title), "link": j.LinkIndex})
	}
	return m.showToast(label, toastInfo)
}

func (m model) onTick(msg tickMsg) (tea.Model, tea.Cmd) {
	switch msg.kind {
	case tickSpin:
		m.spin++
		if m.anyLoading() {
			return m, m.env.after(spinEvery, tickMsg{tickSpin})
		}
		m.spinning = false
	case tickWarm:
		if m.book.anyWarming() {
			return m, m.env.after(warmEvery, tickMsg{tickWarm})
		}
		m.warming = false
	}
	return m, nil
}

func (m model) anyLoading() bool {
	for _, s := range m.stack {
		if s.loading() {
			return true
		}
	}
	return false
}

// ensureTicks starts a timer the state needs and that is not running.
func (m model) ensureTicks() (model, tea.Cmd) {
	var cmds []tea.Cmd
	if !m.spinning && m.anyLoading() {
		m.spinning = true
		cmds = append(cmds, m.env.after(spinEvery, tickMsg{tickSpin}))
	}
	if !m.warming && m.book.anyWarming() {
		m.warming = true
		cmds = append(cmds, m.env.after(warmEvery, tickMsg{tickWarm}))
	}
	return m, tea.Batch(cmds...)
}

// apply takes the settings that the TUI applies while it runs from the effective values the service
// reports: the page size, the confirmation, the cover, and the link preference.
func (m model) apply(list []app.Setting) model {
	by := make(map[string]any, len(list))
	for _, s := range list {
		by[s.Key] = s.Value
	}
	if v, ok := by["interface.page_size"].(int); ok && v > 0 {
		m.pageSize = v
	}
	if v, ok := by["interface.confirm_downloads"].(bool); ok {
		m.confirm = v
	}
	if v, ok := by["download.boxart"].(bool); ok {
		m.boxart = v
	}
	m.prefs, m.cfgPrefs = preferenceFrom(by, m.cfgPrefs, m.prefs)
	return m
}

// preferKeys are the settings that make up the link preference.
var preferKeys = []string{"download.prefer.languages", "download.prefer.formats", "download.prefer.sources", "download.prefer.size"}

// preferenceFrom takes download.prefer.* from the effective settings, but only the keys whose value
// changed since the last read: the P overlay of the Entry screen ("for this run only") must survive
// opening Settings or changing something else. It returns the new preference and the values it saw.
func preferenceFrom(by, prev map[string]any, base app.Preference) (app.Preference, map[string]any) {
	seen := make(map[string]any, len(preferKeys))
	for _, k := range preferKeys {
		seen[k] = by[k]
	}
	if prev == nil { // the first read only records what the configuration says
		return base, seen
	}
	changed := func(k string) bool { return !reflect.DeepEqual(prev[k], seen[k]) }
	if v, ok := by[preferKeys[0]].([]string); ok && changed(preferKeys[0]) {
		base.Languages = v
	}
	if v, ok := by[preferKeys[1]].([]string); ok && changed(preferKeys[1]) {
		base.Formats = v
	}
	if v, ok := by[preferKeys[2]].([]string); ok && changed(preferKeys[2]) {
		base.Sources = v
	}
	if v, ok := by[preferKeys[3]].(string); ok && changed(preferKeys[3]) {
		base.Size = app.SizePref(v)
	}
	return base, seen
}

// quitNeedsAsk is the quit rule of tui.md section 1: ask only when downloads are in progress.
func quitNeedsAsk(busy int) bool { return busy > 0 }

func (m model) requestQuit() (tea.Model, tea.Cmd) {
	if quitNeedsAsk(m.book.busy()) {
		m.quitAsk = true
		return m, nil
	}
	return m, tea.Quit
}

// quitNow cancels every job that is not finished and leaves.
func (m model) quitNow() (tea.Model, tea.Cmd) {
	svc, jobs := m.env.svc, m.book.list()
	return m, func() tea.Msg {
		for _, j := range jobs {
			if !j.Terminal() {
				_ = svc.Cancel(j.ID)
			}
		}
		return tea.QuitMsg{}
	}
}

func (m model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := normalize(msg.String())
	if m.quitAsk || m.top().focus() == focusPrompt {
		k = m.env.yesAlias(k) // the localized initial of "yes" answers a [y/N] question
	}
	switch {
	case k == "ctrl+c":
		if m.quitAsk {
			return m.quitNow()
		}
		return m.requestQuit()
	case m.quitAsk:
		if reduce(scSearch, focusPrompt, k, false) == actYes {
			return m.quitNow()
		}
		m.quitAsk = false
		return m, nil
	case m.help:
		m.help = false
		return m, nil
	case tier(m.w, m.h) == tierTooSmall:
		if k == "q" {
			return m.requestQuit()
		}
		return m, nil
	}
	top := m.top()
	a := reduce(top.id(), top.focus(), k, len(m.stack) == 1)
	switch a {
	case actNone:
		return m, nil
	case actQuit:
		return m.requestQuit()
	case actRedraw:
		return m, nil
	case actHelp:
		m.help = true
		return m, nil
	case actQueue:
		if top.id() == scQueue {
			return m, nil
		}
		return m.push(newQueue())
	case actHistory:
		if top.id() == scHistory {
			return m, nil
		}
		return m.push(newHistory())
	case actSettings:
		if top.id() == scSettings {
			return m, nil
		}
		return m.push(newSettings())
	case actSearchScreen:
		if top.id() != scSearch {
			var fill *app.SearchQuery
			if q, ok := top.(interface{ query() app.SearchQuery }); ok {
				qq := q.query()
				fill = &qq
			}
			return m.toRoot(fill)
		}
	}
	rt := m.rt()
	next, cmd := top.update(keyEv{a: a, k: msg}, rt)
	stack := slices.Clone(m.stack)
	stack[len(stack)-1] = next
	m.stack = stack
	return m, cmd
}

// View draws the current screen in the frame, or the too-small notice.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "mothdex"
	return v
}

func (m model) render() string {
	rt := m.rt()
	if rt.tier == tierTooSmall {
		return m.tooSmall()
	}
	top := m.top()
	sv := top.view(rt, rt.w-4, rt.h-4)
	spec := frameSpec{title: top.title(rt), right: m.summary(), body: sv.body, footer: sv.footer, status: m.status(top, sv)}
	if m.help {
		spec.title = m.env.T("ui.tui.help.title", nil)
		spec.body, spec.footer = m.helpBody(top.id()), ""
	}
	return rt.st.frame(spec, rt.w, rt.h)
}

// tooSmall is the whole screen when the terminal is below the minimum: the notice, wrapped to the
// width there is. The state is kept; the screen returns when the terminal grows.
func (m model) tooSmall() string {
	text := m.env.T("ui.tui.too_small", i18n.Args{"need_w": minCols, "need_h": minRows, "w": m.w, "h": m.h})
	return strings.Join(wrapWords(text, max(m.w, 1)), "\n")
}

// wrapWords breaks text into lines of at most w cells, at spaces; a word longer than a line is cut.
func wrapWords(text string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case cells(cur)+1+cells(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cutTo(cur, w))
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cutTo(cur, w))
	}
	return lines
}

// summary is the live queue summary of the title bar: "↓ 2 active · 1 queued", empty when idle.
func (m model) summary() string {
	active, queued := m.book.counts()
	var parts []string
	if active > 0 {
		parts = append(parts, m.env.st.g.down+" "+m.env.T("ui.tui.title_active", i18n.Args{"count": active}))
	}
	if queued > 0 {
		q := m.env.T("ui.tui.title_queued", i18n.Args{"count": queued})
		if active == 0 {
			q = m.env.st.g.down + " " + q
		}
		parts = append(parts, q)
	}
	return render(m.env.st.accent, strings.Join(parts, " · "))
}

// status is the bottom line: the quit question, a toast, the screen's own line, or the key help.
func (m model) status(top screen, sv screenView) string {
	st := m.env.st
	switch {
	case m.quitAsk:
		return render(st.warn, m.env.T("ui.tui.quit_ask", nil))
	case sv.prompt:
		return sv.status
	case m.toast.text != "":
		style := st.dim
		switch m.toast.kind {
		case toastWarn:
			style = st.warn
		case toastError:
			style = st.bad
		case toastInfo:
			style = st.accent
		}
		return render(style, clean(m.toast.text))
	case sv.status != "":
		return sv.status
	}
	keys := helpFor(top.id())
	limit := statusKeyLimit(m.w)
	more := ""
	if len(keys) > limit {
		keys = keys[:limit]
		more = "  " + m.env.T("ui.tui.more", nil)
	}
	parts := make([]string, len(keys))
	onConsent := false
	if q, ok := top.(queueScreen); ok {
		onConsent = q.consentFocused(m.rt())
	}
	for i, k := range keys {
		parts[i] = k.keys + " " + m.env.T("ui.tui.key."+k.label, nil)
		if onConsent && k.label == "retry" { // r means "download anyway" on a row that waits for the OK
			parts[i] = m.env.T("ui.consent.footer", nil)
		}
	}
	return strings.Join(parts, "  ") + more
}

func (m model) helpBody(id screenID) []string {
	var out []string
	for _, k := range helpFor(id) {
		out = append(out, padLeft(k.keys, 10)+"  "+m.env.T("ui.tui.key."+k.label, nil))
	}
	return append(out, "", render(m.env.st.dim, m.env.T("ui.tui.help.close", nil)))
}
