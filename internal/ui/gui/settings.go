package gui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Settings is docs/ux/gui.md section 5.10: the keys a person on a couch may change, read with ConfigList and written
// with ConfigSet and ConfigReset (the service validates and refuses what it must, such as download.on_exists=overwrite;
// the screen never writes around it). Formats, sources, folders, hosts and timeouts stay in `mothdex config`.

type rowKind int

const (
	kindStepper   rowKind = iota // Left/Right change a number in place
	kindChoice                   // Left/Right cycle a short list in place; A opens it
	kindToggle                   // A flips it
	kindLanguages                // the ordered list editor
	kindMargin                   // the calibration view
	kindCache                    // CacheStats, and A asks before CacheClear
	kindInfo                     // read-only: System and About
)

// choice is one value of a list setting: what the config file holds and the locale key of its label.
type choice struct{ value, label string }

// settingRow is one row of the screen.
type settingRow struct {
	id       string // the config key, or the name of a row that has none
	section  int
	label    string // ui.gui.settings.row.<label>
	kind     rowKind
	min, max int      // kindStepper
	choices  []choice // kindChoice; a file value outside them (overwrite) shows read-only
	nextRun  bool     // applies on the next start
}

var settingSections = []string{"sec_downloads", "sec_screen", "sec_storage"}

// uiLanguages are the locales the GUI draws, each by its own name (they are not translated).
var uiLanguages = []struct{ id, name string }{
	{"en_us", "English"}, {"pt_br", "Português (Brasil)"}, {"es_es", "Español (España)"}, {"ru_ru", "Русский"},
}

func languageChoices() []choice {
	out := []choice{{"auto", "val.auto"}}
	for _, l := range uiLanguages {
		out = append(out, choice{l.id, "=" + l.name}) // a label that starts with = is shown as it is
	}
	return out
}

// settingRows are the rows of the table of section 5.10, in screen order. A test compares the keys with the schema.
var settingRows = []settingRow{
	{id: "download.concurrency", section: 0, label: "concurrency", kind: kindStepper, min: 1, max: 6, nextRun: true},
	{id: "download.on_exists", section: 0, label: "on_exists", kind: kindChoice, choices: []choice{{"skip", "val.skip"}, {"rename", "val.rename"}}},
	{id: "download.boxart", section: 0, label: "boxart", kind: kindToggle},
	{id: "download.prefer.languages", section: 0, label: "languages", kind: kindLanguages},
	{id: "download.prefer.size", section: 0, label: "size", kind: kindChoice, choices: []choice{{"none", "val.any"}, {"smallest", "val.smallest"}, {"largest", "val.largest"}}},
	{id: "gui.confirm_button", section: 1, label: "confirm_button", kind: kindChoice, choices: []choice{{"south", "val.bottom"}, {"east", "val.right"}}},
	{id: "gui.text_size", section: 1, label: "text_size", kind: kindChoice, choices: []choice{{"normal", "val.normal"}, {"large", "val.large"}}},
	{id: "gui.safe_area", section: 1, label: "safe_area", kind: kindMargin},
	{id: "interface.language", section: 1, label: "language", kind: kindChoice, choices: languageChoices(), nextRun: true},
	{id: "interface.confirm_downloads", section: 1, label: "confirm_downloads", kind: kindToggle},
	{id: "cache", section: 2, label: "cache", kind: kindCache},
	{id: "system", section: 2, label: "system", kind: kindInfo},
	{id: "about", section: 2, label: "about", kind: kindInfo},
}

// languageTokens are the tokens of the "+ Add" list of the preferred languages.
var languageTokens = []string{"en", "fr", "de", "es", "it", "pt", "ja", "ru", "zh", "ko", "nl", "sv"}

type editorKind int

const (
	edChoice   editorKind = iota // the list of a choice row
	edLangs                      // the ordered list of preferred languages
	edLangItem                   // Move up, Move down, Remove for one language
	edLangAdd                    // the languages that can still be added
	edMargin                     // the calibration view
	edInfo                       // the read-only panel of System and About
)

type settingsEditor struct {
	kind   editorKind
	row    string
	focus  int
	langs  []string // the working copy of the language list while its editor is open
	idx    int      // the language the item menu is about
	pct    int      // the margin being tried
	opened int      // the margin when the view opened
}

type settingsState struct {
	focus   string // the id of the focused row
	top     int    // the first item shown
	errs    map[string]string
	changed map[string]bool // rows that now apply on the next start
	ed      *settingsEditor
}

// settingItem is a header, a row or the closing line.
type settingItem struct {
	header string
	row    *settingRow
	more   bool
}

func (m Model) settingItems() []settingItem {
	var items []settingItem
	last := -1
	for i := range settingRows {
		r := &settingRows[i]
		if r.section != last {
			items = append(items, settingItem{header: m.tr("ui.gui.settings."+settingSections[r.section], nil)})
			last = r.section
		}
		items = append(items, settingItem{row: r})
	}
	return append(items, settingItem{more: true})
}

func (m Model) settingRowByID(id string) *settingRow {
	for i := range settingRows {
		if settingRows[i].id == id {
			return &settingRows[i]
		}
	}
	return nil
}

// setting is the effective value and the source of a key.
func (m Model) setting(key string) (app.Setting, bool) {
	for _, s := range m.config {
		if s.Key == key {
			return s, true
		}
	}
	return app.Setting{}, false
}

func (m Model) settingString(key string) string {
	s, _ := m.setting(key)
	return strings.TrimSpace(fmt.Sprint(s.Value))
}

// envName is the environment variable of a key, from the schema.
func (m Model) envName(key string) string {
	for _, sp := range m.specs {
		if sp.Key == key {
			return sp.Env
		}
	}
	return ""
}

// readOnlyNote says why a row cannot be changed here, "" when it can: a value set by the environment or a flag wins
// over the file, so writing the file would change nothing; overwrite cannot be stored at all.
func (m Model) readOnlyNote(r *settingRow) string {
	s, ok := m.setting(r.id)
	if !ok {
		return ""
	}
	switch string(s.Source) {
	case "env":
		return m.tr("ui.tui.settings.set_by_env", map[string]any{"var": m.envName(r.id)})
	case "flag":
		return m.tr("ui.tui.settings.set_by_flag", nil)
	}
	if r.id == "download.on_exists" && strings.TrimSpace(fmt.Sprint(s.Value)) == "overwrite" {
		return m.tr("ui.gui.settings.overwrite_ro", nil)
	}
	return ""
}

func (m Model) rowSource(r *settingRow) string {
	s, ok := m.setting(r.id)
	if !ok {
		return ""
	}
	return string(s.Source)
}

// ---- opening and leaving ----

func (m Model) openSettings() (Model, []Cmd) {
	if m.screen == screenSettings {
		return m, nil
	}
	m = m.enter(screenSettings)
	m.settings = settingsState{errs: map[string]string{}, changed: map[string]bool{}, focus: settingRows[0].id}
	svc := m.o.Service
	return m, []Cmd{configCmd(svc), cacheStatsCmd(svc)}
}

// ---- service calls ----

type (
	// configSetMsg is the answer of ConfigSet or ConfigReset for one row.
	configSetMsg struct {
		row string
		err error
	}
	cacheStatsMsg struct {
		total int64
		err   error
	}
	cacheClearMsg struct{ err error }
)

func configCmd(svc Service) Cmd {
	return func() Msg {
		list, err := svc.ConfigList()
		return configMsg{list: list, err: err}
	}
}

func cacheStatsCmd(svc Service) Cmd {
	return func() Msg {
		st, err := svc.CacheStats()
		return cacheStatsMsg{total: st.TotalBytes, err: err}
	}
}

func (m Model) setCmd(r *settingRow, value string) Cmd {
	svc, key, row := m.o.Service, r.id, r.id
	return func() Msg { return configSetMsg{row: row, err: svc.ConfigSet(key, value)} }
}

func (m Model) resetCmd(r *settingRow) Cmd {
	svc, key, row := m.o.Service, r.id, r.id
	return func() Msg { return configSetMsg{row: row, err: svc.ConfigReset(key)} }
}

// onConfigSet shows the error under the row and keeps the old value, or reads the settings again so the new value
// takes effect now (the keys the GUI consumes) or is shown with "Applies on next start".
func (m Model) onConfigSet(msg configSetMsg) (Model, []Cmd) {
	s := m.settings
	errs := map[string]string{}
	for k, v := range s.errs {
		errs[k] = v
	}
	changed := map[string]bool{}
	for k, v := range s.changed {
		changed[k] = v
	}
	if msg.err != nil {
		f := m.report(msg.err, "settings")
		errs[msg.row] = m.tr("ui.gui.settings.not_saved", map[string]any{"message": f.msg}) + // the ref has a line of its own,
			"\n" + m.tr("ui.gui.error.code_ref", map[string]any{"code": f.code, "ref": f.ref}) // so a cut never takes it
		s.errs, s.changed = errs, changed
		if s.ed != nil && s.ed.kind != edMargin { // the working copy may be wrong now: close the editor, the list shows the truth
			s.ed = nil
		}
		m.settings = s
		return m, []Cmd{configCmd(m.o.Service)}
	}
	delete(errs, msg.row)
	if r := m.settingRowByID(msg.row); r != nil && r.nextRun {
		changed[msg.row] = true
	}
	s.errs, s.changed = errs, changed
	m.settings = s
	return m, []Cmd{configCmd(m.o.Service)}
}

func (m Model) onCacheStats(msg cacheStatsMsg) Model {
	if msg.err != nil {
		m.o.Log.Warn("gui: CacheStats", "err", msg.err)
		m.cacheTotal = -1
		return m
	}
	m.cacheTotal = msg.total
	return m
}

func (m Model) onCacheClear(msg cacheClearMsg, now time.Time) (Model, []Cmd) {
	if msg.err != nil {
		return m.withToast(m.report(msg.err, "cache clear").short(), toastError, now), nil
	}
	m = m.withToast(m.tr("ui.gui.settings.cache_done", nil), toastOK, now)
	return m, []Cmd{cacheStatsCmd(m.o.Service)}
}

// ---- actions ----

func (m Model) onSettingsAction(a input.Action, now time.Time) (Model, []Cmd) {
	if m.settings.ed != nil {
		return m.onEditorAction(a, now)
	}
	items := m.settingItems()
	rows := focusableRows(items)
	idx := slices.IndexFunc(rows, func(r *settingRow) bool { return r.id == m.settings.focus })
	if idx < 0 {
		idx = 0
	}
	row := rows[idx]
	ro := m.readOnlyNote(row) != ""
	switch a {
	case input.Up:
		idx = max(idx-1, 0)
	case input.Down:
		idx = min(idx+1, len(rows)-1)
	case input.PageUp:
		idx = max(idx-m.settingsVisibleRows(), 0)
	case input.PageDown:
		idx = min(idx+m.settingsVisibleRows(), len(rows)-1)
	case input.Left, input.Right:
		if ro {
			return m, nil
		}
		return m.changeInPlace(row, a == input.Right)
	case input.Confirm:
		return m.editRow(row, ro, now)
	case input.Download: // X resets a value that comes from the file
		if m.rowSource(row) == "file" && row.kind != kindCache && row.kind != kindInfo {
			return m, []Cmd{m.resetCmd(row)}
		}
		return m, nil
	case input.Back:
		return m.leave(), nil
	}
	m.settings.focus = rows[idx].id
	m.settings.top, _ = m.settingsWindow(items)
	return m, nil
}

func focusableRows(items []settingItem) []*settingRow {
	var rows []*settingRow
	for _, it := range items {
		if it.row != nil {
			rows = append(rows, it.row)
		}
	}
	return rows
}

// changeInPlace is Left or Right on a stepper or a choice row, without opening the list.
func (m Model) changeInPlace(r *settingRow, right bool) (Model, []Cmd) {
	step := -1
	if right {
		step = 1
	}
	switch r.kind {
	case kindStepper:
		n, _ := strconv.Atoi(m.settingString(r.id))
		to := min(max(n+step, r.min), r.max)
		if to == n {
			return m, nil
		}
		return m, []Cmd{m.setCmd(r, strconv.Itoa(to))}
	case kindChoice:
		cur := slices.IndexFunc(r.choices, func(c choice) bool { return c.value == m.settingString(r.id) })
		if cur < 0 {
			return m, nil
		}
		to := min(max(cur+step, 0), len(r.choices)-1)
		if to == cur {
			return m, nil
		}
		return m, []Cmd{m.setCmd(r, r.choices[to].value)}
	}
	return m, nil
}

// editRow is A on a row.
func (m Model) editRow(r *settingRow, ro bool, now time.Time) (Model, []Cmd) {
	switch r.kind {
	case kindToggle:
		if ro {
			return m, nil
		}
		return m, []Cmd{m.setCmd(r, strconv.FormatBool(m.settingString(r.id) != "true"))}
	case kindChoice:
		if ro {
			return m, nil
		}
		focus := slices.IndexFunc(r.choices, func(c choice) bool { return c.value == m.settingString(r.id) })
		m.settings.ed = &settingsEditor{kind: edChoice, row: r.id, focus: max(focus, 0)}
	case kindLanguages:
		if ro {
			return m, nil
		}
		m.settings.ed = &settingsEditor{kind: edLangs, row: r.id, langs: m.languageList()}
	case kindMargin:
		if ro {
			return m, nil
		}
		pct := m.cfg.safe.percentFor(m.metrics.DisplayW, m.metrics.DisplayH)
		m.settings.ed = &settingsEditor{kind: edMargin, row: r.id, pct: pct, opened: pct}
	case kindCache:
		return m.askClearCache(now), nil
	case kindInfo:
		if r.id == "about" {
			m.logFile = m.logPath()
		}
		m.settings.ed = &settingsEditor{kind: edInfo, row: r.id}
	}
	return m, nil
}

// languageList is the preferred languages as the service has them.
func (m Model) languageList() []string {
	s, _ := m.setting("download.prefer.languages")
	switch v := s.Value.(type) {
	case []string:
		return slices.Clone(v)
	case string:
		var out []string
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	return nil
}

// askClearCache opens the question of Settings › Cache: Keep is the default, and B is Keep.
func (m Model) askClearCache(now time.Time) Model {
	size := humanSize(max(m.cacheTotal, 0))
	return m.openDialog(dialogState{
		kind:  dialogClearCache,
		title: m.tr("ui.gui.settings.cache_ask", map[string]any{"size": size}),
		buttons: []dialogButton{
			{btnKeep, m.tr("ui.gui.queue.keep", nil)},
			{btnClear, m.tr("ui.gui.settings.cache_clear", nil)},
		},
	}, now)
}

func clearCacheCmd(svc Service) Cmd {
	return func() Msg { return cacheClearMsg{err: svc.CacheClear()} }
}

// ---- the editors ----

// editorOptions are the labels of the list an editor shows.
func (m Model) editorOptions(e *settingsEditor) []string {
	switch e.kind {
	case edChoice:
		r := m.settingRowByID(e.row)
		out := make([]string, len(r.choices))
		for i, c := range r.choices {
			out[i] = m.choiceLabel(c)
		}
		return out
	case edLangs:
		out := make([]string, 0, len(e.langs)+1)
		for _, l := range e.langs {
			out = append(out, titleToken(l))
		}
		return append(out, m.tr("ui.gui.settings.lang_add", nil))
	case edLangItem:
		var out []string
		if e.idx > 0 {
			out = append(out, m.tr("ui.gui.settings.move_up", nil))
		}
		if e.idx < len(e.langs)-1 {
			out = append(out, m.tr("ui.gui.settings.move_down", nil))
		}
		return append(out, m.tr("ui.gui.settings.remove", nil))
	case edLangAdd:
		var out []string
		for _, t := range m.addableLanguages(e) {
			out = append(out, titleToken(t))
		}
		return out
	}
	return nil
}

func (m Model) addableLanguages(e *settingsEditor) []string {
	var out []string
	for _, t := range languageTokens {
		if !slices.Contains(e.langs, t) {
			out = append(out, t)
		}
	}
	return out
}

func titleToken(t string) string {
	if t == "" {
		return t
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// choiceLabel is the label of a choice: a locale key, the pad's own button label for the confirm button, or a name
// that is shown as it is.
func (m Model) choiceLabel(c choice) string {
	if strings.HasPrefix(c.label, "=") {
		return c.label[1:]
	}
	args := map[string]any{}
	switch c.value {
	case "south":
		args["label"] = input.PositionLabel(input.South, m.pad)
	case "east":
		args["label"] = input.PositionLabel(input.East, m.pad)
	}
	return m.tr("ui.gui.settings."+c.label, args)
}

func (m Model) onEditorAction(a input.Action, now time.Time) (Model, []Cmd) {
	e := *m.settings.ed
	switch e.kind {
	case edMargin:
		switch a {
		case input.Left:
			e.pct = max(e.pct-1, 0)
		case input.Right:
			e.pct = min(e.pct+1, maxSafePercent)
		case input.Back: // leaves without saving: the old margin was never replaced
			m.settings.ed = nil
			return m, nil
		case input.Confirm:
			m.settings.ed = nil
			if e.pct == e.opened {
				return m, nil
			}
			return m, []Cmd{m.setCmd(m.settingRowByID(e.row), strconv.Itoa(e.pct))}
		}
		m.settings.ed = &e
		return m, nil
	case edInfo:
		if a == input.Confirm && m.canExport(e.row) {
			return m.askExport(now), nil
		}
		switch a {
		case input.Back, input.Confirm:
			m.settings.ed = nil
			return m, nil
		case input.Up, input.Down: // scrolls a panel with more lines than fit
			_, _, maxOff := m.infoGeometry(m.painter(nil), e.row, 0)
			if a == input.Up {
				e.focus = max(e.focus-1, 0)
			} else {
				e.focus = min(e.focus+1, maxOff)
			}
			m.settings.ed = &e
		}
		return m, nil
	}
	opts := m.editorOptions(&e)
	switch a {
	case input.Up:
		e.focus = max(e.focus-1, 0)
	case input.Down:
		e.focus = min(e.focus+1, max(len(opts)-1, 0))
	case input.Back:
		return m.closeEditorLevel(e), nil
	case input.Confirm:
		return m.confirmEditor(e)
	}
	m.settings.ed = &e
	return m, nil
}

// closeEditorLevel is B in a list editor: the submenus go back to the list, the list closes.
func (m Model) closeEditorLevel(e settingsEditor) Model {
	switch e.kind {
	case edLangItem, edLangAdd:
		e.kind, e.focus = edLangs, min(e.idx, len(e.langs))
		m.settings.ed = &e
	default:
		m.settings.ed = nil
	}
	return m
}

func (m Model) confirmEditor(e settingsEditor) (Model, []Cmd) {
	r := m.settingRowByID(e.row)
	switch e.kind {
	case edChoice:
		m.settings.ed = nil
		if e.focus < len(r.choices) && r.choices[e.focus].value != m.settingString(r.id) {
			return m, []Cmd{m.setCmd(r, r.choices[e.focus].value)}
		}
		return m, nil
	case edLangs:
		if e.focus >= len(e.langs) { // + Add
			if len(m.addableLanguages(&e)) == 0 {
				return m, nil
			}
			e.kind, e.focus = edLangAdd, 0
		} else {
			e.kind, e.idx, e.focus = edLangItem, e.focus, 0
		}
		m.settings.ed = &e
		return m, nil
	case edLangAdd:
		add := m.addableLanguages(&e)
		if e.focus >= len(add) {
			return m, nil
		}
		e.langs = append(slices.Clone(e.langs), add[e.focus])
		return m.saveLanguages(e, len(e.langs)-1)
	case edLangItem:
		return m.applyLanguageItem(e)
	}
	return m, nil
}

// applyLanguageItem runs Move up, Move down or Remove on the language the item menu is about; the options shown
// depend on the position, so the focus names the action through the same list.
func (m Model) applyLanguageItem(e settingsEditor) (Model, []Cmd) {
	var actions []string
	if e.idx > 0 {
		actions = append(actions, "up")
	}
	if e.idx < len(e.langs)-1 {
		actions = append(actions, "down")
	}
	actions = append(actions, "remove")
	langs := slices.Clone(e.langs)
	at := e.idx
	switch actions[min(e.focus, len(actions)-1)] {
	case "up":
		langs[at], langs[at-1] = langs[at-1], langs[at]
		at--
	case "down":
		langs[at], langs[at+1] = langs[at+1], langs[at]
		at++
	case "remove":
		langs = slices.Delete(langs, at, at+1)
		at = min(at, len(langs))
	}
	e.langs = langs
	return m.saveLanguages(e, at)
}

// saveLanguages writes the working copy and returns to the list with focus on row at. An empty list resets the key,
// which is "Any".
func (m Model) saveLanguages(e settingsEditor, at int) (Model, []Cmd) {
	r := m.settingRowByID(e.row)
	e.kind, e.focus = edLangs, at
	m.settings.ed = &e
	if len(e.langs) == 0 {
		return m, []Cmd{m.resetCmd(r)}
	}
	return m, []Cmd{m.setCmd(r, strings.Join(e.langs, ","))}
}

// ---- applying what the GUI consumes ----

// applySettings takes the values the GUI reads from the settings: the download and confirm settings that the
// session gave the model when it started.
func (m Model) applySettings(list []app.Setting) Model {
	for _, s := range list {
		value := strings.TrimSpace(fmt.Sprint(s.Value))
		switch s.Key {
		case "download.boxart":
			m.o.Boxart = value == "true"
		case "interface.confirm_downloads":
			m.o.Confirm = value == "true"
		case "download.prefer.size":
			m.o.Prefer.Size = app.SizePref(map[string]string{"smallest": "smallest", "largest": "largest"}[value])
		case "download.prefer.languages":
			if l, ok := s.Value.([]string); ok {
				m.o.Prefer.Languages = slices.Clone(l)
			}
		}
	}
	return m
}
