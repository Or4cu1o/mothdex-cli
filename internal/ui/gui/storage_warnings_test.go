package gui

import (
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

const (
	gamesInRAM = "Games folder is on a RAM disk: downloaded games are lost at reboot."
	dataInRAM  = "Data folder is on a RAM disk: history, cache and settings are lost at reboot."
)

// openSystemCard opens Settings > System: the row before About, at the end of the list.
func openSystemCard(t *testing.T, h *harness) {
	t.Helper()
	for i := 0; i < 11; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm)
}

// A RAM disk is not a verification problem (PRT-G11): the System card shows each storage warning as its own warn line, with no
// "Not verified" prefix, and the Verified line does not depend on it. Bússola set the rule; Forja's profile detection fills
// ProfileInfo.StorageWarnings with the two texts.
func TestTheSystemCardShowsStorageWarningsOnTheirOwnAndKeepsVerified(t *testing.T) {
	svc := settingsFixture()
	svc.Profile = app.ProfileInfo{ID: "batocera", Name: "Batocera", RomRoot: "/userdata/roms", Verified: true,
		StorageWarnings: []string{gamesInRAM, dataInRAM}}
	h := openSettingsFromHome(t, svc, 1280, 720)
	openSystemCard(t, h)

	h.mustHave("Verified")
	for _, want := range []string{gamesInRAM, dataInRAM} {
		if !shows(h, want) {
			t.Errorf("the card does not show %q: %q", want, h.texts())
		}
	}
	for _, txt := range h.texts() {
		if strings.Contains(txt, "Not verified") {
			t.Errorf("a storage warning is not a verification problem: %q", txt)
		}
	}
	// every line of a warning, wrapped or not, is in the warn color; "Verified" is not
	for _, op := range h.render().Ops {
		if op.Text == "" || op.Text == "Verified" || len(op.Text) < 12 {
			continue
		}
		if (strings.Contains(gamesInRAM, op.Text) || strings.Contains(dataInRAM, op.Text)) && op.Color != colWarn {
			t.Errorf("%q is a storage warning and is drawn in %+v, not the warn color", op.Text, op.Color)
		}
	}
}

// A card with no storage warning is as before, and an unverified profile still says "Not verified: ..." for its own warnings.
func TestTheSystemCardKeepsItsVerificationLinesApart(t *testing.T) {
	svc := settingsFixture()
	svc.Profile = app.ProfileInfo{ID: "generic", Name: "Generic", RomRoot: "/roms", Warnings: []string{"no frontend found"},
		StorageWarnings: []string{dataInRAM}}
	h := openSettingsFromHome(t, svc, 1280, 720)
	openSystemCard(t, h)
	h.mustHave("Not verified: no frontend found")
	if !shows(h, dataInRAM) {
		t.Errorf("the storage warning is missing: %q", h.texts())
	}
	h.mustNotHave("Verified")
	if shows(h, "Not verified: "+dataInRAM) {
		t.Error("the storage warning got the Not verified prefix")
	}

	clean := settingsFixture()
	clean.Profile = app.ProfileInfo{ID: "batocera", Name: "Batocera", RomRoot: "/userdata/roms", Verified: true}
	h = openSettingsFromHome(t, clean, 1280, 720)
	openSystemCard(t, h)
	h.mustHave("Verified")
	if shows(h, gamesInRAM) || shows(h, dataInRAM) {
		t.Error("a profile with no storage warning shows none")
	}
}

// shows reports whether the frame holds the text, which may have wrapped onto several lines of the card.
func shows(h *harness, text string) bool {
	return strings.Contains(strings.Join(h.texts(), " "), text)
}

func TestSystemCardLocalizesStorageWarnings(t *testing.T) {
	svc := settingsFixture()
	svc.Profile = app.ProfileInfo{Verified: true, StorageWarnings: []string{gamesInRAM, dataInRAM}}
	h := openSettingsFromHome(t, svc, 1280, 720)
	h.m.o.T = shared.bundle.New("pt_br")
	openSystemCard(t, h)
	if !shows(h, "Os jogos baixados serão perdidos ao reiniciar.") || !shows(h, "O histórico, o cache e as configurações serão perdidos ao reiniciar.") {
		t.Fatalf("storage warnings are not localized: %v", h.texts())
	}
}
