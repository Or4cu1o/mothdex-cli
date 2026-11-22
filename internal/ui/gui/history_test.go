package gui

import (
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

func rec(id, slug, title, state string, code string) app.HistoryRecord {
	r := app.HistoryRecord{ID: id, Slug: slug, Title: title, Platform: "nes", State: state, LinkIndex: 1, Bytes: 384 << 10, Filename: slug + ".zip",
		FinishedAt: time.Date(2026, 11, 20, 14, 3, 0, 0, time.Local)}
	if code != "" {
		r.Error = &history.Failure{Code: code, Message: "the file has the wrong size"}
		r.Bytes = 0
	}
	return r
}

// openHistoryFromHome walks Home to its History tile and opens it.
func openHistoryFromHome(t *testing.T, h *harness) {
	t.Helper()
	for i := 0; i < 8 && h.m.focus != tileHistory; i++ {
		if i%2 == 0 {
			h.press(input.Down)
		} else {
			h.press(input.Right)
		}
	}
	if h.m.focus != tileHistory {
		t.Fatalf("could not focus the History tile: %q", h.m.focus)
	}
	h.press(input.Confirm)
	if h.m.screen != screenHistory {
		t.Fatalf("screen %d, want History", h.m.screen)
	}
}

func historyService() *guitest.Fake {
	svc := zeldaService(3)
	stopped := rec("j4", "zelda-003", "Zelda 003", "failed", "confirmation_required")
	stopped.ContentLength, stopped.CatalogSize = 700<<20, 40<<20
	svc.Records = []app.HistoryRecord{
		rec("j1", "zelda-001", "Kirby's Adventure (USA)", "done", ""),
		rec("j2", "zelda-002", "Contra (USA)", "failed", "size_mismatch"),
		stopped,
		rec("j5", "zelda-001", "Skipped game", "skipped", ""),
	}
	return svc
}

// History lists the records, filters to the failed ones with Y, opens Details on A, and Download again queues the
// same entry and link; a second Download again while it is queued queues nothing (docs/ux/gui.md section 5.9).
func TestHistoryListsFiltersAndDownloadsAgain(t *testing.T) {
	svc := historyService()
	h := started(t, svc, 1280, 720)
	openHistoryFromHome(t, h)
	h.mustHave("mothdex › History", "All · 4 records", "Kirby's Adventure (USA)", "Contra (USA)", "Zelda 003", "Skipped game")
	h.mustHave("Details", "Failed only", "Back")

	h.press(input.Info)
	h.mustHave("Failed only · 2 records", "Show all")
	h.mustNotHave("Kirby's Adventure (USA)")
	h.press(input.Info)
	h.mustHave("All · 4 records")

	h.press(input.Confirm) // Details of the row that kept the focus through the filter: Contra, a failed record
	h.mustHave("Contra (USA)", "Download again", "Close")
	h.press(input.Back)
	h.mustNotHave("Download again")

	h.press(input.Confirm)
	h.press(input.Confirm) // Download again
	if got := svc.Requests(); len(got) != 1 || got[0].Ref.Slug != "zelda-002" || got[0].SizeConsent != 0 {
		t.Fatalf("requests %+v", got)
	}
	h.press(input.Confirm)
	h.press(input.Confirm) // the same file is queued now
	if len(svc.Requests()) != 1 {
		t.Fatalf("a file that is already queued is not queued again: %+v", svc.Requests())
	}
	h.mustHave("Already queued again.")
}

// A record that stopped on its real size asks again instead of going out with no consent: its first button is
// Download anyway, and the dialog consents to exactly the size it shows (backlog history-retry-consent).
func TestHistoryRecordStoppedOnItsSizeAsksAgain(t *testing.T) {
	svc := historyService()
	h := started(t, svc, 1280, 720)
	openHistoryFromHome(t, h)
	h.mustHave("2026-11-20 14:03 · Needs your OK: the file is 700.0 MB, listed as 40.0 MB.")
	h.press(input.Down)
	h.press(input.Down)
	h.press(input.Confirm) // the stopped record, then Details
	h.mustHave("Download anyway", "Close")
	h.mustNotHave("Download again")
	h.press(input.Confirm) // opens the dialog; nothing is queued yet
	if len(svc.Requests()) != 0 {
		t.Fatal("the button only opens the dialog")
	}
	h.mustHave("This file is 700.0 MB, not 40.0 MB.", "Don't download", "Download 700.0 MB")
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if got := svc.Requests(); len(got) != 1 || got[0].Ref.Slug != "zelda-003" || got[0].SizeConsent != 700<<20 {
		t.Fatalf("requests %+v: the consent is for the size shown", got)
	}
}

func TestHistoryEmptyStatesAndReload(t *testing.T) {
	svc := zeldaService(1)
	h := started(t, svc, 1280, 720)
	openHistoryFromHome(t, h)
	h.mustHave("No downloads yet. Search for a game to start.", "All · 0 records")
	h.press(input.Info)
	h.mustHave("No failed downloads.", "Show all")
	h.press(input.Info)

	before := svc.Count("history")
	svc.Records = []app.HistoryRecord{rec("j1", "zelda-001", "Fresh record", "done", "")}
	h.send(JobMsg{Job: app.JobSnapshot{ID: "j1", Slug: "zelda-001", State: app.StateDone}}) // a terminal event
	if svc.Count("history") != before+1 {
		t.Fatalf("history is read again after a terminal event: %d calls", svc.Count("history")-before)
	}
	h.mustHave("Fresh record", "All · 1 record")
}
