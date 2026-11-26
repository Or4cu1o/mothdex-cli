package gui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The version notice and "Update" (docs/contracts/cli/update.md section 5, T-110 TM-39 and TM-40): a check once
// Home is up, a notice, the item in Settings › About, and an update that only starts when the person asks and
// only installs after a second yes, given on the pad once the download is verified.

// Updates is what the composition root gives the model for this.
type Updates interface {
	// Check answers from the cache or the network within its own time limit. A zero Version means nothing newer.
	Check(ctx context.Context) (Release, error)
	// MarkNotified records that the notice of a version was shown, so that it is told once a day.
	MarkNotified(version string) error
	// Apply downloads, verifies and installs the new version. progress and confirm are called from the goroutine
	// that runs Apply, one at a time. confirm blocks until the person answers; false changes nothing. Canceling
	// ctx before the confirmation changes nothing either.
	Apply(ctx context.Context, progress func(UpdateProgress), confirm func(Staged) bool) error
}

// Release is a newer version. Notify says the person has not been told about it today.
type Release struct {
	Version, PageURL string
	Notify           bool
}

// UpdateProgress is the step Apply is on (docs/contracts/cli/update.md: discover, download, verify, stage, swap,
// check, done) and how far it is; Total is zero while it is unknown.
type UpdateProgress struct {
	Step        string
	Done, Total int64
}

// Staged is a downloaded and verified update that waits for the person. Notes come from the verified archive.
type Staged struct {
	From, To, Signature, Notes string
}

const (
	notesLines  = 6 // of the CHANGELOG summary in the confirmation: what fits a small screen
	updateEvent = 8 // events waiting for the model; progress beyond that is dropped
	signedNone  = "none"
)

// updateRun is one Apply in progress. The goroutine that runs it talks to the model through events.
type updateRun struct {
	events chan Msg
	answer chan bool
	cancel context.CancelFunc
}

type updateState struct {
	checked   bool
	release   Release
	run       *updateRun
	progress  UpdateProgress
	staged    Staged
	confirmed bool // the person said yes: files may be changing, so nothing cancels it
	restart   bool // the person asked to restart
}

type (
	releaseMsg struct {
		release Release
		err     error
	}
	updateProgressMsg struct {
		run *updateRun
		p   UpdateProgress
	}
	updateStagedMsg struct {
		run    *updateRun
		staged Staged
	}
	updateDoneMsg struct {
		run *updateRun
		err error
	}
)

// RestartRequested reports that the run ended because the person chose to restart into the new version.
func (m Model) RestartRequested() bool { return m.done && m.upd.restart }

// checkUpdate starts the one check of the run, once Home is up so that it never delays the start.
func (m Model) checkUpdate() (Model, []Cmd) {
	if m.o.Updates == nil || m.upd.checked || m.screen != screenHome {
		return m, nil
	}
	m.upd.checked = true
	up := m.o.Updates
	return m, []Cmd{func() Msg {
		r, err := up.Check(context.Background())
		return releaseMsg{release: r, err: err}
	}}
}

func (m Model) onRelease(msg releaseMsg, now time.Time) (Model, []Cmd) {
	if msg.err != nil { // a notice nobody asked for must not become an error on screen
		m.o.Log.Info("gui: update check", "err", msg.err)
		return m, nil
	}
	m.upd.release = Release{Version: app.SanitizeText(msg.release.Version), PageURL: app.SanitizeText(msg.release.PageURL), Notify: msg.release.Notify}
	if m.upd.release.Version == "" || !msg.release.Notify || m.screen == screenExit {
		return m, nil
	}
	m = m.withToastFor(m.tr("ui.gui.update.available", map[string]any{"version": m.upd.release.Version}), toastInfo, noticeFor, now)
	up, log, version := m.o.Updates, m.o.Log, msg.release.Version
	return m, []Cmd{func() Msg {
		if err := up.MarkNotified(version); err != nil {
			log.Info("gui: update notice not recorded", "err", err)
		}
		return nil
	}}
}

// startUpdate runs Apply off the render thread and shows its progress.
func (m Model) startUpdate() (Model, []Cmd) {
	if m.o.Updates == nil || m.upd.run != nil {
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &updateRun{events: make(chan Msg, updateEvent), answer: make(chan bool, 1), cancel: cancel}
	m.upd.run, m.upd.progress, m.upd.confirmed, m.upd.staged = run, UpdateProgress{}, false, Staged{}
	m.overlay, m.dlg, m.toast = overlayUpdate, nil, toast{}
	m.epoch++
	up := m.o.Updates
	return m, []Cmd{func() Msg {
		go func() {
			defer cancel()
			err := up.Apply(ctx, func(p UpdateProgress) {
				select {
				case run.events <- updateProgressMsg{run: run, p: p}:
				default: // the screen is behind: the next one says more
				}
			}, func(s Staged) bool {
				run.events <- updateStagedMsg{run: run, staged: s}
				select {
				case yes := <-run.answer:
					return yes
				case <-ctx.Done():
					return false
				}
			})
			run.events <- updateDoneMsg{run: run, err: err}
		}()
		return <-run.events
	}}
}

func nextUpdate(run *updateRun) Cmd { return func() Msg { return <-run.events } }

func (m Model) onUpdateProgress(msg updateProgressMsg) (Model, []Cmd) {
	if msg.run != m.upd.run {
		return m, nil
	}
	m.upd.progress = msg.p
	return m, []Cmd{nextUpdate(msg.run)}
}

// onUpdateStaged asks the question that installs: the versions, the notes of the verified archive and how far its
// origin was proven, with Cancel focused.
func (m Model) onUpdateStaged(msg updateStagedMsg, now time.Time) (Model, []Cmd) {
	if msg.run != m.upd.run || m.screen == screenExit {
		return m, []Cmd{answerUpdate(msg.run, false)}
	}
	s := msg.staged
	m.upd.staged = s
	var lines []string
	for _, line := range strings.Split(s.Notes, "\n") {
		if line = strings.TrimSpace(app.SanitizeText(line)); line != "" && len(lines) < notesLines {
			lines = append(lines, line)
		}
	}
	signature := m.tr("ui.gui.update.signature_none", nil)
	if s.Signature != signedNone && s.Signature != "" {
		signature = m.tr("ui.gui.update.signature", map[string]any{"method": app.SanitizeText(s.Signature)})
	}
	return m.openDialog(dialogState{
		kind:  dialogUpdate,
		title: m.tr("ui.gui.update.confirm", map[string]any{"from": app.SanitizeText(s.From), "to": app.SanitizeText(s.To)}),
		// the signature comes first: a long CHANGELOG must never push it off the screen
		detail: strings.Join(append([]string{signature}, lines...), "\n"), lines: notesLines + 2,
		buttons: []dialogButton{{btnCancel, m.tr("ui.gui.hint.cancel", nil)}, {btnUpdate, m.tr("ui.gui.update.action", nil)}},
	}, now), nil
}

// answerUpdate gives the person's answer to the waiting Apply and takes its next event.
func answerUpdate(run *updateRun, yes bool) Cmd {
	return func() Msg {
		select {
		case run.answer <- yes:
		default: // already answered
		}
		return <-run.events
	}
}

func (m Model) decideUpdate(yes bool) (Model, []Cmd) {
	run := m.upd.run
	if run == nil {
		return m, nil
	}
	m.upd.confirmed = yes
	m.overlay = overlayUpdate
	m.epoch++
	return m, []Cmd{answerUpdate(run, yes)}
}

// cancelUpdate stops a download that has not been confirmed. The overlay stays until Apply has returned.
func (m Model) cancelUpdate() {
	if m.upd.run != nil && !m.upd.confirmed {
		m.upd.run.cancel()
	}
}

func (m Model) onUpdateDone(msg updateDoneMsg, now time.Time) (Model, []Cmd) {
	if msg.run != m.upd.run {
		return m, nil
	}
	installed := msg.err == nil && m.upd.confirmed
	m.upd.run = nil
	if m.screen == screenExit {
		return m, nil
	}
	m = m.closeOverlay()
	switch {
	case installed:
		m.upd.release = Release{}
		return m.openDialog(dialogState{
			kind: dialogRestart, title: m.tr("ui.gui.update.done", map[string]any{"version": app.SanitizeText(m.upd.staged.To)}),
			detail:  m.tr("ui.gui.update.restart_ask", nil),
			buttons: []dialogButton{{btnLater, m.tr("ui.gui.update.later", nil)}, {btnRestart, m.tr("ui.gui.update.restart", nil)}},
		}, now), nil
	case msg.err == nil || declined(msg.err):
		return m.withToast(m.tr("ui.gui.update.canceled", nil), toastInfo, now), nil
	}
	f := m.report(msg.err, "update")
	return m.openDialog(dialogState{
		kind: dialogExported, title: m.tr("ui.gui.update.failed", nil),
		detail: f.msg + "\n" + m.tr("ui.gui.error.code_ref", map[string]any{"code": f.code, "ref": f.ref}), lines: exportDoneLines,
		buttons: []dialogButton{{btnOK, m.tr("ui.gui.logs.ok", nil)}},
	}, now), nil
}

// declined reports an Apply that ended because the person said no.
func declined(err error) bool {
	var ae *app.Error
	return canceled(err) || (errors.As(err, &ae) && ae.Code == app.CodeCanceled)
}

// onUpdateAction is the pad while the update runs: B stops a download, nothing stops an installation.
func (m Model) onUpdateAction(a input.Action) (Model, []Cmd) {
	if a == input.Back {
		m.cancelUpdate()
	}
	return m, nil
}

func (m Model) drawUpdate(p painter) {
	met := m.metrics
	u := met.Unit
	title := m.tr("ui.gui.update.downloading", nil)
	if step := m.upd.progress.Step; step == "verify" || step == "stage" {
		title = m.tr("ui.gui.update.verifying", nil)
	}
	var lines []string
	if m.upd.confirmed {
		title = m.tr("ui.gui.update.installing", nil)
		lines = []string{m.tr("ui.gui.update.keep_on", nil)}
	} else {
		if pr := m.upd.progress; pr.Total > 0 {
			lines = append(lines, m.tr("ui.gui.update.progress", map[string]any{"done": humanSize(pr.Done), "total": humanSize(pr.Total)}))
		} else if pr.Done > 0 {
			lines = append(lines, humanSize(pr.Done))
		}
		lines = append(lines, input.Hint(input.Back, m.pad, m.cfg.confirmEast)+"  "+m.tr("ui.gui.hint.cancel", nil))
	}
	w := min(met.Safe.W-4*u, 24*met.Body)
	var wrapped []string
	for _, l := range lines {
		wrapped = append(wrapped, p.wrap(met.Secondary, l, w-4*u, 3)...)
	}
	h := LineHeight(met.Body) + len(wrapped)*LineHeight(met.Secondary) + 5*u
	panel := draw.Rect{X: met.Safe.X + (met.Safe.W-w)/2, Y: met.Safe.Y + (met.Safe.H-h)/2, W: w, H: h}
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, colScrim)
	p.window(panel)
	y := panel.Y + 2*u
	p.text(panel.X+2*u, y, met.Body, colText, p.fit(met.Body, title, w-4*u))
	y += LineHeight(met.Body) + u
	for _, l := range wrapped {
		p.text(panel.X+2*u, y, met.Secondary, colDim, l)
		y += LineHeight(met.Secondary)
	}
}
