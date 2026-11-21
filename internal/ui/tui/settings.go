package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

const (
	settingLabelW  = 28
	settingSourceW = 8
	cacheKey       = "cache" // the row that is an action, not a setting
)

// settingKind decides how a row is edited.
type settingKind int

const (
	kindBool settingKind = iota
	kindInt
	kindEnum
	kindText
	kindList
	kindCache
)

// settingSpec is one row of the curated Settings table. Until the app layer exposes the config schema
// (ui.md section 5, gap F1) the kinds, ranges and values are written here, for the keys the UX shows.
type settingSpec struct {
	key  string
	kind settingKind
	enum []string
	rng  string // the range shown while editing a number
	live bool   // the TUI applies it at once; the others apply on the next start
}

// settingSections are the groups of tui.md section 7, in order. Advanced keys stay in `mothdex config`.
var settingSections = []struct {
	name string
	rows []settingSpec
}{
	{"download", []settingSpec{
		{key: "download.dir", kind: kindText},
		{key: "download.concurrency", kind: kindInt, rng: "1-6"},
		// overwrite is not offered: the service always refuses it as a setting (use -f per run)
		{key: "download.on_exists", kind: kindEnum, enum: []string{"skip", "rename"}},
		{key: "download.boxart", kind: kindBool, live: true},
		{key: "download.prefer.languages", kind: kindList, live: true},
		{key: "download.prefer.formats", kind: kindList, live: true},
		{key: "download.prefer.sources", kind: kindList, live: true},
		{key: "download.prefer.size", kind: kindEnum, enum: []string{"none", "smallest", "largest"}, live: true},
	}},
	{"interface", []settingSpec{
		{key: "interface.language", kind: kindText},
		{key: "interface.color", kind: kindEnum, enum: []string{"auto", "always", "never"}},
		{key: "interface.page_size", kind: kindInt, rng: "1-100", live: true},
		{key: "interface.confirm_downloads", kind: kindBool, live: true},
	}},
	{"cache", []settingSpec{{key: cacheKey, kind: kindCache}}},
}

// settingRows is the table as one list, in screen order.
func settingRows() []settingSpec {
	var out []settingSpec
	for _, sec := range settingSections {
		out = append(out, sec.rows...)
	}
	return out
}

type (
	settingsMsg struct {
		tok   int64
		list  []app.Setting
		stats app.CacheStats
		err   error
	}
	setDoneMsg struct {
		key string
		err error
	}
	cacheClearedMsg struct{ err error }
)

// settingEdit is a row being edited: a text field, or the list of an enum.
type settingEdit struct {
	spec   settingSpec
	in     textinput.Model
	enumAt int
	err    string
}

// settingsScreen is the Settings screen (tui.md section 7).
type settingsScreen struct {
	st       loadState
	fail     failure
	tok      int64
	vals     map[string]app.Setting
	stats    app.CacheStats
	cur      int
	changed  map[string]bool // set during this run: the ones that are not live say "applies on next start"
	edit     *settingEdit
	askCache bool
}

func newSettings() settingsScreen {
	return settingsScreen{st: loadLoading, vals: map[string]app.Setting{}, changed: map[string]bool{}}
}

func (settingsScreen) id() screenID            { return scSettings }
func (s settingsScreen) loading() bool         { return s.st == loadLoading }
func (settingsScreen) title(rt *shared) string { return rt.T("ui.tui.screen.settings", nil) }

func (s settingsScreen) focus() focusKind {
	switch {
	case s.askCache:
		return focusPrompt
	case s.edit != nil && s.edit.spec.kind != kindEnum:
		return focusText
	}
	return focusNone
}

func (s settingsScreen) init(rt *shared) (screen, tea.Cmd) { return s.load(rt) }

func (s settingsScreen) load(rt *shared) (settingsScreen, tea.Cmd) {
	s.tok = rt.token()
	tok, e := s.tok, rt.env
	return s, func() tea.Msg {
		list, err := e.svc.ConfigList()
		stats, _ := e.svc.CacheStats() // the cache row is optional: its failure only hides the numbers
		return settingsMsg{tok: tok, list: list, stats: stats, err: err}
	}
}

func (s settingsScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsMsg:
		if msg.tok != s.tok {
			return s, nil
		}
		if msg.err != nil {
			s.st, s.fail = loadError, rt.failureOf(msg.err)
			return s, nil
		}
		s.vals = make(map[string]app.Setting, len(msg.list))
		for _, st := range msg.list {
			s.vals[st.Key] = st
		}
		s.stats, s.st = msg.stats, loadReady
		list := msg.list
		return s, func() tea.Msg { return appliedMsg{settings: list} } // the root applies the live ones
	case setDoneMsg:
		return s.onSet(msg, rt)
	case cacheClearedMsg:
		if msg.err != nil {
			f := rt.failureOf(msg.err)
			return s, toastCmd(f.msg+" ["+f.code+"]", toastError)
		}
		next, cmd := s.load(rt)
		return next, tea.Batch(cmd, toastCmd(rt.T("ui.cli.cache.cleared", nil), toastInfo))
	case keyEv:
		return s.onKey(msg, rt)
	}
	return s, nil
}

// onSet closes the editor on success and reloads; on failure the editor stays open with the message.
func (s settingsScreen) onSet(msg setDoneMsg, rt *shared) (screen, tea.Cmd) {
	if msg.err != nil {
		f := rt.failureOf(msg.err)
		if s.edit != nil {
			e := *s.edit
			e.err = f.msg
			s.edit = &e
			return s, nil
		}
		return s, toastCmd(f.msg+" ["+f.code+"]", toastError)
	}
	s.edit = nil
	s.changed = withKey(s.changed, msg.key)
	return s.load(rt)
}

func withKey(m map[string]bool, key string) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[key] = true
	return out
}

func (s settingsScreen) spec() settingSpec { return settingRows()[s.cur] }

// readOnlyNote says why a row cannot be changed here: an environment variable or a flag sets it.
func readOnlyNote(rt *shared, key string, src string) string {
	switch src {
	case "env":
		return rt.T("ui.tui.settings.set_by_env", i18n.Args{"var": "MOTHDEX_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))})
	case "flag":
		return rt.T("ui.tui.settings.set_by_flag", nil)
	}
	return ""
}

func (s settingsScreen) onKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	switch {
	case s.askCache:
		s.askCache = false
		if ev.a == actYes {
			return s, clearCacheCmd(rt.env)
		}
		return s, nil
	case s.edit != nil:
		return s.onEditKey(ev, rt)
	case s.st == loadError:
		switch ev.a {
		case actRetry:
			return s.load(rt)
		case actBack:
			return s, popCmd()
		}
		return s, nil
	}
	n := len(settingRows())
	switch ev.a {
	case actBack:
		return s, popCmd()
	case actUp, actDown, actFirst, actLast:
		s.cur = moveIndex(ev.a, s.cur, n)
	case actEnter, actToggle, actReset:
		return s.act(ev.a, rt)
	}
	return s, nil
}

// act handles Enter, Space and Backspace on the row under the cursor.
func (s settingsScreen) act(a action, rt *shared) (screen, tea.Cmd) {
	sp := s.spec()
	if sp.kind == kindCache {
		if a == actEnter && s.stats.MaxBytes > 0 {
			s.askCache = true
		}
		return s, nil
	}
	cur := s.vals[sp.key]
	if note := readOnlyNote(rt, sp.key, string(cur.Source)); note != "" {
		return s, toastCmd(note, toastWarn)
	}
	switch {
	case a == actReset:
		return s, setCmd(rt.env, sp.key, "", true)
	case sp.kind == kindBool:
		b, _ := cur.Value.(bool)
		return s, setCmd(rt.env, sp.key, fmt.Sprint(!b), false)
	case a == actToggle:
		return s, nil
	case sp.kind == kindEnum:
		at := 0
		for i, v := range sp.enum {
			if v == settingRaw(cur.Value) {
				at = i
			}
		}
		s.edit = &settingEdit{spec: sp, enumAt: at}
	default:
		in := textinput.New()
		in.Prompt = ""
		in.SetWidth(map[bool]int{true: 8, false: 22}[sp.kind == kindInt]) // leave room for the range
		in.SetValue(settingRaw(cur.Value))
		in.Focus()
		s.edit = &settingEdit{spec: sp, in: in}
	}
	return s, nil
}

func (s settingsScreen) onEditKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	e := *s.edit
	enum := e.spec.kind == kindEnum
	switch ev.a {
	case actBlur, actBack:
		s.edit = nil
		return s, nil
	case actUp, actDown:
		if enum {
			e.enumAt = moveIndex(ev.a, e.enumAt, len(e.spec.enum))
		}
	case actEnter:
		value := strings.TrimSpace(e.in.Value())
		if enum {
			value = e.spec.enum[e.enumAt]
		}
		e.err = ""
		s.edit = &e
		return s, setCmd(rt.env, e.spec.key, value, false)
	case actText:
		e.in, _ = e.in.Update(ev.k)
		e.err = ""
	}
	s.edit = &e
	return s, nil
}

// setCmd writes one setting (or resets it); the service validates and writes the file atomically.
func setCmd(e *env, key, value string, reset bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if reset {
			err = e.svc.ConfigReset(key)
		} else {
			err = e.svc.ConfigSet(key, value)
		}
		return setDoneMsg{key: key, err: err}
	}
}

func clearCacheCmd(e *env) tea.Cmd {
	return func() tea.Msg { return cacheClearedMsg{err: e.svc.CacheClear()} }
}

// settingRaw is a value as it is typed back into the editor.
func settingRaw(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(x, ",")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// settingText is a value as it is shown in the table.
func settingText(rt *shared, v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return rt.T("ui.tui.settings.on", nil)
		}
		return rt.T("ui.tui.settings.off", nil)
	case []string:
		if len(x) == 0 {
			return "-"
		}
		return strings.Join(x, ", ")
	case string:
		return orDash(x)
	case nil:
		return "-"
	}
	return fmt.Sprint(v)
}

func settingLabel(rt *shared, key string) string {
	return rt.T("ui.tui.settings.label."+strings.ReplaceAll(key, ".", "_"), nil)
}

func (s settingsScreen) view(rt *shared, w, h int) screenView {
	switch s.st {
	case loadLoading:
		return screenView{body: []string{rt.st.spin(rt.spin) + " " + rt.T("ui.tui.loading", nil)}}
	case loadError:
		return screenView{body: errorBody(rt, s.fail), status: rt.T("ui.tui.results.retry_help", nil)}
	}
	if s.edit != nil && s.edit.spec.kind == kindEnum {
		return s.enumView(rt)
	}
	var lines []string
	curLine := 0
	i := 0
	for _, sec := range settingSections {
		lines = append(lines, render(rt.st.heading, rt.T("ui.tui.settings.section."+sec.name, nil)))
		for _, sp := range sec.rows {
			if i == s.cur {
				curLine = len(lines)
			}
			lines = append(lines, s.rowLine(rt, sp, i == s.cur, w))
			i++
		}
	}
	lines = append(lines, "", render(rt.st.dim, rt.T("ui.tui.settings.advanced", nil)))
	top := 0
	if curLine >= h {
		top = curLine - h + 1
	}
	sv := screenView{body: lines[min(top, len(lines)):]}
	switch {
	case s.askCache:
		sv.status = render(rt.st.warn, rt.T("ui.tui.settings.clear_cache_ask", i18n.Args{"size": humanSize(s.stats.TotalBytes)}))
		sv.prompt = true
	case s.edit != nil && s.edit.err != "":
		sv.status, sv.prompt = render(rt.st.bad, s.edit.err), true
	default:
		sv.status = s.hint(rt)
	}
	return sv
}

// hint is the status line of the row under the cursor: why it is read-only, or that it applies later.
func (s settingsScreen) hint(rt *shared) string {
	sp := s.spec()
	if sp.kind == kindCache {
		return ""
	}
	if note := readOnlyNote(rt, sp.key, string(s.vals[sp.key].Source)); note != "" {
		return render(rt.st.warn, note)
	}
	if s.changed[sp.key] && !sp.live {
		return render(rt.st.dim, rt.T("ui.tui.settings.next_start", nil))
	}
	return ""
}

// rowLine draws one row: the label, the value (or the field being edited), and where the value comes from.
func (s settingsScreen) rowLine(rt *shared, sp settingSpec, cursor bool, w int) string {
	label := fit(settingLabel(rt, sp.key), settingLabelW)
	if sp.kind == kindCache {
		return s.cacheLine(rt, label, cursor, w)
	}
	cur := s.vals[sp.key]
	src := clean(string(cur.Source))
	value := settingText(rt, cur.Value)
	if s.edit != nil && s.edit.spec.key == sp.key {
		value = "[ " + s.edit.in.View() + " ]"
		if sp.rng != "" {
			value += " " + sp.rng
		}
	}
	room := max(w-1-settingLabelW-2-settingSourceW-2, 4)
	line := label + "  " + fit(clean(value), room) + "  " + padLeft(render(rt.st.dim, src), settingSourceW)
	return mark(rt, line, cursor, w)
}

func (s settingsScreen) cacheLine(rt *shared, label string, cursor bool, w int) string {
	usage := rt.T("ui.tui.settings.cache_unknown", nil)
	if s.stats.MaxBytes > 0 {
		usage = rt.T("ui.cli.cache.total", i18n.Args{"used": humanSize(s.stats.TotalBytes), "max": humanSize(s.stats.MaxBytes)})
	}
	return mark(rt, label+"  "+usage+"   [ "+rt.T("ui.tui.settings.clear_cache", nil)+" ]", cursor, w)
}

// mark draws the cursor row in reverse video with a > in front.
func mark(rt *shared, line string, cursor bool, w int) string {
	if cursor {
		return render(rt.st.on, ">"+fit(line, max(w-1, 1)))
	}
	return " " + line
}

// enumView is the small list of an enum opened with Enter.
func (s settingsScreen) enumView(rt *shared) screenView {
	e := s.edit
	lines := []string{render(rt.st.heading, settingLabel(rt, e.spec.key)), ""}
	for i, v := range e.spec.enum {
		row := "  " + v
		if i == e.enumAt {
			row = render(rt.st.on, "> "+v)
		}
		lines = append(lines, row)
	}
	sv := screenView{body: lines, status: rt.T("ui.tui.settings.enum_help", nil)}
	if e.err != "" {
		sv.status, sv.prompt = render(rt.st.bad, e.err), true
	}
	return sv
}
