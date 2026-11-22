package gui

import (
	"slices"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The controller notices of docs/ux/gui.md section 2.3. The backend reports every joystick SDL sees (PadsMsg); this
// turns the list into what the person needs to know:
//
//   - no controller that SDL can use: a warn toast once when that state appears (after the first report at start, and when
//     the last pad goes away), and the "No controller" badge in the top bar for as long as it lasts, since one toast can be
//     missed;
//   - a joystick SDL has no mapping for: a warn toast once per device per session that names it and points to Settings ›
//     About, where the full path of the file to add the mapping to is shown.
//
// Notices are toasts, so they show one at a time: one that comes while another is on screen waits for it to end.

// hasMappedPad reports whether some joystick has a mapping, so it can drive the GUI.
func (m Model) hasMappedPad() bool {
	for _, p := range m.pads {
		if p.Mapped {
			return true
		}
	}
	return false
}

// noPad is true once the backend has reported and no joystick that SDL maps is present.
func (m Model) noPad() bool { return m.padsKnown && !m.hasMappedPad() }

// padKey says which device a notice was already given for: its name and GUID, so two joysticks of the same model are one
// device (one notice), and where the GUID is not available (32-bit platforms) only the name counts.
func padKey(p input.PadInfo) string { return p.Name + "|" + p.GUID }

// onPads takes the list of joysticks and queues the notices its change calls for.
func (m Model) onPads(msg PadsMsg, now time.Time) Model {
	first := !m.padsKnown
	hadMapped := m.hasMappedPad()
	m.pads, m.padsKnown = slices.Clone(msg.Pads), true
	var queue []string
	if !m.hasMappedPad() && (first || hadMapped) {
		queue = append(queue, m.tr("ui.gui.pad.none", nil))
	}
	seen := make(map[string]bool, len(m.unmappedSeen)+len(msg.Pads))
	for k := range m.unmappedSeen {
		seen[k] = true
	}
	for _, p := range msg.Pads {
		if p.Mapped || seen[padKey(p)] {
			continue
		}
		seen[padKey(p)] = true
		queue = append(queue, m.unmappedNotice(p))
	}
	m.unmappedSeen = seen
	m.notices = append(slices.Clone(m.notices), queue...)
	return m.showNotices(now)
}

// unmappedNotice is the toast for a joystick that has no mapping. The name may wrap and, past two lines, is the only
// thing cut, with an ellipsis (it is a device name, like a title); the rest of the sentence is never cut.
func (m Model) unmappedNotice(p input.PadInfo) string {
	name := app.SanitizeText(p.Name)
	if name == "" {
		name = "?"
	}
	sentence := func(n string) string { return m.tr("ui.gui.pad.unmapped", map[string]any{"name": n}) }
	pp := m.painter(nil)
	runes := []rune(name)
	for k := len(runes); k > 0; k-- {
		cand := string(runes[:k])
		if k < len(runes) {
			cand += "…"
		}
		mm := m
		mm.toast.text = sentence(cand)
		lines := mm.toastLines(pp)
		if strings.Join(lines, " ") == mm.toast.text {
			if k < len(runes) { // a cut name ends in an ellipsis: it takes the place of the period after it
				return strings.Replace(mm.toast.text, cand+".", cand, 1)
			}
			return mm.toast.text
		}
	}
	return sentence("…")
}

// showNotices puts the next waiting notice on screen when no toast is showing and the screen is one that shows toasts
// (not the Splash, which hands over to Home, nor the exit).
func (m Model) showNotices(now time.Time) Model {
	if len(m.notices) == 0 || m.screen == screenSplash || m.screen == screenExit || now.Before(m.toast.until) {
		return m
	}
	text := m.notices[0]
	m.notices = slices.Clone(m.notices[1:])
	return m.withToastFor(text, toastWarn, noticeFor, now)
}
