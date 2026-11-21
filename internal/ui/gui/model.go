package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Timings of docs/ux/gui.md sections 4 and 5.
const (
	splashMax     = 3 * time.Second        // Home opens after this long even if DBInfo has not answered
	spinnerAfter  = 200 * time.Millisecond // a spinner appears only after this long, so cached answers never flash
	spinnerFrame  = 60 * time.Millisecond  // how often a visible spinner is redrawn
	spinnerPeriod = time.Second            // one turn per second
	toastFor      = 3 * time.Second
	dialogIgnore  = 150 * time.Millisecond // input right after a dialog opens is ignored (a held button)
	exitMinShow   = time.Second            // the exit screen stays at least this long
	exitNotice    = 4 * time.Second        // and this long after Close when the refresh needs the person's hand
	dbInfoTimeout = 15 * time.Second
)

type screenKind int

const (
	screenSplash screenKind = iota
	screenHome
	screenExit
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayMenu
	overlayDialog
)

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastWarn
	toastError
)

type toast struct {
	text  string
	kind  toastKind
	until time.Time
}

type dbState int

const (
	dbPending dbState = iota
	dbLoaded
	dbFailed
)

// settings are the gui.* keys the model consumes.
type settings struct {
	confirmEast bool
	largeText   bool
	safe        SafeArea
}

type exitState struct {
	signal    bool // SIGTERM or SIGINT: no minimum time on screen
	cancel    bool
	started   time.Time
	finished  bool
	res       exitResult
	holdUntil time.Time
}

// Model is the whole state of the GUI. Update returns a new Model; nothing in a Model is changed after it
// was returned, so the run loop can keep the old one.
type Model struct {
	o       Options
	cfg     settings
	metrics Metrics
	pad     input.ControllerType
	profile app.ProfileInfo

	screen screenKind
	start  time.Time
	focus  string // the focused element of the screen, by id

	overlay     overlayKind
	menuFocus   string
	dlg         *dialogState
	dlgFocus    string
	ignoreUntil time.Time
	epoch       int // changes whenever an overlay opens or closes: a held button is released then

	toast       toast
	db          dbState
	entries     int64
	entriesText string // the catalog size with its thousands separators, formatted once
	offline     bool
	recent      []string
	jobs        jobTally
	quitPending bool // "Quit when finished" was chosen
	noticeShown bool // the locale notice was shown
	exit        *exitState
	done        bool
}

// Messages that carry the result of a Cmd.
type (
	dbInfoMsg struct {
		entries int64
		err     error
	}
	configMsg struct {
		list []app.Setting
		err  error
	}
	exitDoneMsg struct{ res exitResult }
)

// New returns the model at the Splash and the calls it needs: the catalog size and the settings. It reads the
// profile and the jobs the service already has, which is memory only.
func New(o Options, now time.Time) (Model, []Cmd) {
	if o.Clock == nil {
		o.Clock = SystemClock()
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	m := Model{
		o:       o,
		cfg:     settings{confirmEast: o.ConfirmEast, largeText: o.LargeText, safe: o.Safe},
		profile: o.Service.ActiveProfile(),
		screen:  screenSplash,
		start:   now,
		recent:  readRecent(o.DataDir),
	}
	m.metrics = NewMetrics(o.Width, o.Height, m.cfg.safe, m.cfg.largeText)
	for _, j := range o.Service.Jobs() {
		m.jobs, _, _ = m.jobs.apply(j)
	}
	svc := o.Service
	return m, []Cmd{
		func() Msg {
			ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
			defer cancel()
			res, err := svc.DBInfo(ctx)
			return dbInfoMsg{entries: res.Value.TotalEntries, err: err}
		},
		func() Msg {
			list, err := svc.ConfigList()
			return configMsg{list: list, err: err}
		},
	}
}

// Metrics is the layout in use, for the run loop (render scale, logical size).
func (m Model) Metrics() Metrics { return m.metrics }

// TextMode says whether the screen takes typed text: the loop turns SDL text input on and switches the input
// layer to its text rules.
func (m Model) TextMode() bool { return false }

// InputConfig is what the input layer must follow: the confirm button.
func (m Model) InputConfig() input.Config { return input.Config{ConfirmEast: m.cfg.confirmEast} }

// Done reports that the exit screen has finished and the loop must stop; the exit code is 0.
func (m Model) Done() bool { return m.done }

// OverlayEpoch changes whenever an overlay opens or closes; the loop releases any held button when it does.
func (m Model) OverlayEpoch() int { return m.epoch }

// Update applies one message at time now and returns the new model and the service calls to start.
func (m Model) Update(msg Msg, now time.Time) (Model, []Cmd) {
	switch msg := msg.(type) {
	case InputMsg:
		return m.onAction(msg.Action, now)
	case TickMsg:
		return m.onTick(now)
	case ResizeMsg:
		m.metrics = NewMetrics(msg.W, msg.H, m.cfg.safe, m.cfg.largeText)
		return m, nil
	case ControllerMsg:
		m.pad = msg.Type
		return m, nil
	case JobMsg:
		return m.onJob(msg.Job, now)
	case SignalMsg:
		if m.screen == screenExit {
			return m, nil
		}
		return m.startExit(true, true, now)
	case dbInfoMsg:
		return m.onDBInfo(msg, now)
	case configMsg:
		return m.onConfig(msg), nil
	case exitDoneMsg:
		return m.onExitDone(msg, now)
	}
	return m, nil
}

func dirOf(a input.Action) (Dir, bool) {
	switch a {
	case input.Up:
		return DirUp, true
	case input.Down:
		return DirDown, true
	case input.Left:
		return DirLeft, true
	case input.Right:
		return DirRight, true
	}
	return 0, false
}

func (m Model) onAction(a input.Action, now time.Time) (Model, []Cmd) {
	switch {
	case m.screen == screenExit:
		if a == input.Confirm && m.exit != nil && m.exit.finished && m.exit.res.Batch.RefreshWarning != "" {
			m.done = true // A skips the wait for the notice
		}
		return m, nil
	case m.screen == screenSplash:
		return m, nil
	case m.overlay == overlayDialog:
		if now.Before(m.ignoreUntil) {
			return m, nil
		}
		return m.onDialogAction(a, now)
	case m.overlay == overlayMenu:
		return m.onMenuAction(a, now)
	}
	return m.onHomeAction(a, now)
}

func (m Model) onTick(now time.Time) (Model, []Cmd) {
	switch m.screen {
	case screenSplash:
		if !now.Before(m.start.Add(splashMax)) {
			return m.goHome(now), nil
		}
	case screenExit:
		if m.exit != nil && m.exit.finished && !now.Before(m.exit.holdUntil) {
			m.done = true
		}
	}
	return m, nil
}

func (m Model) goHome(now time.Time) Model {
	m.screen = screenHome
	m.focus = tileSearch
	if m.o.LocaleFallback && !m.noticeShown {
		m.noticeShown = true
		m = m.withToast(m.tr("ui.gui.locale_fallback", nil), toastWarn, now)
	}
	return m
}

func (m Model) onDBInfo(msg dbInfoMsg, now time.Time) (Model, []Cmd) {
	if msg.err != nil {
		m.db = dbFailed
		var ae *app.Error
		if errors.As(msg.err, &ae) && ae.Code == app.CodeNetwork {
			m.offline = true
		}
		m.o.Log.Warn("gui: DBInfo", "err", msg.err)
	} else {
		m.db, m.entries, m.offline = dbLoaded, msg.entries, false
		m.entriesText = message.NewPrinter(language.Make(strings.ReplaceAll(m.o.T.Locale(), "_", "-"))).Sprintf("%d", msg.entries)
	}
	if m.screen == screenSplash {
		m = m.goHome(now)
	}
	return m, nil
}

// onConfig reads the gui.* keys. A key that is missing (an older schema) or invalid keeps its default.
func (m Model) onConfig(msg configMsg) Model {
	if msg.err != nil {
		m.o.Log.Warn("gui: ConfigList", "err", msg.err)
		return m
	}
	for _, s := range msg.list {
		value := strings.TrimSpace(fmt.Sprint(s.Value))
		switch s.Key {
		case "gui.confirm_button":
			m.cfg.confirmEast = value == "east"
		case "gui.text_size":
			m.cfg.largeText = value == "large"
		case "gui.safe_area":
			if safe, ok := ParseSafeArea(value); ok {
				m.cfg.safe = safe
			}
		}
	}
	m.metrics = NewMetrics(m.metrics.DisplayW, m.metrics.DisplayH, m.cfg.safe, m.cfg.largeText)
	return m
}

func (m Model) onJob(j app.JobSnapshot, now time.Time) (Model, []Cmd) {
	var prev app.JobSnapshot
	var known bool
	m.jobs, prev, known = m.jobs.apply(j)
	if j.State == app.StateFailed && (!known || prev.State != app.StateFailed) {
		m = m.withToast(m.tr("ui.gui.toast.download_failed", map[string]any{"title": app.SanitizeText(j.Title)}), toastError, now)
	}
	if m.quitPending && m.screen != screenExit && m.jobs.unfinished() == 0 {
		return m.startExit(false, false, now)
	}
	return m, nil
}

// startExit begins the exit sequence off the render thread and shows the exit screen.
func (m Model) startExit(signal, cancel bool, now time.Time) (Model, []Cmd) {
	m.screen, m.overlay, m.dlg = screenExit, overlayNone, nil
	m.epoch++
	m.exit = &exitState{signal: signal, cancel: cancel, started: now}
	svc, clk, log := m.o.Service, m.o.Clock, m.o.Log
	return m, []Cmd{func() Msg { return exitDoneMsg{res: runExit(svc, clk, cancel, exitBudget, log)} }}
}

func (m Model) onExitDone(msg exitDoneMsg, now time.Time) (Model, []Cmd) {
	if m.exit == nil {
		return m, nil
	}
	exit := *m.exit
	exit.finished, exit.res = true, msg.res
	switch {
	case exit.signal:
		m.done = true // no dialog and no notice on SIGTERM: leave inside the budget
	case msg.res.Batch.RefreshWarning != "":
		exit.holdUntil = now.Add(exitNotice)
	default:
		exit.holdUntil = latest(now, exit.started.Add(exitMinShow))
	}
	m.exit = &exit
	if !m.done && !now.Before(exit.holdUntil) {
		m.done = true
	}
	return m, nil
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (m Model) withToast(text string, kind toastKind, now time.Time) Model {
	m.toast = toast{text: text, kind: kind, until: now.Add(toastFor)}
	return m
}

// NextWake is when the model next needs a frame or a Tick with no input: a toast ending, the Splash timeout,
// a spinner appearing or turning, the end of the exit screen's hold. ok is false when nothing is due.
func (m Model) NextWake(now time.Time) (time.Time, bool) {
	var wake time.Time
	earliest := func(t time.Time) {
		if wake.IsZero() || t.Before(wake) {
			wake = t
		}
	}
	if now.Before(m.toast.until) {
		earliest(m.toast.until)
	}
	switch m.screen {
	case screenSplash:
		earliest(m.start.Add(splashMax))
		if now.Before(m.start.Add(spinnerAfter)) {
			earliest(m.start.Add(spinnerAfter))
		} else {
			earliest(now.Add(spinnerFrame))
		}
	case screenExit:
		if m.exit != nil && !m.exit.finished {
			earliest(now.Add(spinnerFrame))
		} else if m.exit != nil && !m.done {
			earliest(m.exit.holdUntil)
		}
	}
	return wake, !wake.IsZero()
}

// tr translates a key; a missing key shows as itself.
func (m Model) tr(key string, args map[string]any) string { return m.o.T.T(key, args) }
