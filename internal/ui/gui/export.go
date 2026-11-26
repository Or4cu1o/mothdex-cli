package gui

import (
	"context"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// "Export logs" (docs/contracts/cli/logs.md section 7, T-110 TM-27), from Settings › About and from the crash
// screen: a confirmation, the bundle off the render thread, then a dialog that says where it is.

// exportTimeout bounds the bundle: a few megabytes of local files.
const exportTimeout = time.Minute

// exportDoneLines is how many lines the place of the bundle may take: a long path on a small screen.
const exportDoneLines = 6

type exportDoneMsg struct {
	path string
	err  error
}

// canExport says whether the info panel of a settings row offers the action.
func (m Model) canExport(row string) bool { return row == "about" && m.o.ExportLogs != nil }

func (m Model) askExport(now time.Time) Model {
	if m.exporting {
		return m.withToast(m.tr("ui.gui.logs.exporting", nil), toastInfo, now)
	}
	return m.openDialog(dialogState{
		kind: dialogExportLogs, title: m.tr("ui.gui.logs.ask", nil), detail: m.tr("ui.gui.logs.ask_detail", nil), lines: exportDoneLines,
		buttons: []dialogButton{
			{btnCancel, m.tr("ui.gui.hint.cancel", nil)},
			{btnExport, m.tr("ui.gui.logs.export", nil)},
		},
	}, now)
}

func (m Model) startExport(now time.Time) (Model, []Cmd) {
	m.exporting = true
	m = m.withToast(m.tr("ui.gui.logs.exporting", nil), toastInfo, now)
	export := m.o.ExportLogs
	return m, []Cmd{func() Msg {
		ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
		defer cancel()
		path, err := export(ctx)
		return exportDoneMsg{path: path, err: err}
	}}
}

// onExportDone says where the bundle is. When the person has opened something else meanwhile, a toast says it
// instead of covering that.
func (m Model) onExportDone(msg exportDoneMsg, now time.Time) Model {
	m.exporting, m.toast = false, toast{}
	if m.screen == screenExit {
		return m
	}
	title, detail, kind := m.tr("ui.gui.logs.done", nil), "", toastOK
	if msg.err != nil {
		f := m.report(msg.err, "logs export")
		title, detail, kind = m.tr("ui.gui.logs.failed", nil), f.short(), toastError
	} else {
		detail = folderThenFile(app.SanitizeText(msg.path))
	}
	if m.overlay != overlayNone {
		return m.withToastFor(detail, kind, noticeFor, now)
	}
	return m.openDialog(dialogState{
		kind: dialogExported, title: title, detail: detail, lines: exportDoneLines,
		buttons: []dialogButton{{btnOK, m.tr("ui.gui.logs.ok", nil)}},
	}, now)
}

// folderThenFile breaks a path before its file name, so that the name a person looks for is not cut in two.
func folderThenFile(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[:i+1] + "\n" + path[i+1:]
	}
	return path
}
