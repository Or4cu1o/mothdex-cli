package gui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The rows of the screen are keys of the real schema, with the same range and values: the GUI's table cannot drift
// from the config contract (docs/ux/gui.md section 8, F1).
func TestSettingRowsAreKeysOfTheSchema(t *testing.T) {
	specs := map[string]config.SettingSpec{}
	for _, sp := range config.Specs() {
		specs[sp.Key] = sp
	}
	for _, r := range settingRows {
		if r.kind == kindCache || r.kind == kindInfo {
			continue
		}
		sp, ok := specs[r.id]
		if !ok {
			t.Errorf("%s is not a key of the schema", r.id)
			continue
		}
		wantKind := map[rowKind]string{kindStepper: config.KindInt, kindToggle: config.KindBool, kindLanguages: config.KindList}[r.kind]
		if wantKind != "" && sp.Kind != wantKind {
			t.Errorf("%s: the schema says %s, the row is a %s editor", r.id, sp.Kind, wantKind)
		}
		switch r.kind {
		case kindStepper:
			if sp.Min != strconv.Itoa(r.min) || sp.Max != strconv.Itoa(r.max) {
				t.Errorf("%s: range %s-%s in the schema, %d-%d here", r.id, sp.Min, sp.Max, r.min, r.max)
			}
		case kindChoice:
			if r.id == "interface.language" {
				continue // a locale such as pt_br: checked by the schema's own validator
			}
			for _, c := range r.choices {
				found := false
				for _, v := range sp.Enum {
					found = found || v == c.value
				}
				if !found {
					t.Errorf("%s: %q is not one of %v", r.id, c.value, sp.Enum)
				}
			}
			for _, v := range sp.Enum {
				if v == "overwrite" { // the one value the service refuses to store: the GUI never offers it
					for _, c := range r.choices {
						if c.value == "overwrite" {
							t.Errorf("%s offers overwrite", r.id)
						}
					}
				}
			}
		}
	}
}

func settingsFixture() *guitest.Fake {
	svc := zeldaService(2)
	svc.Settings = []app.Setting{
		{Key: "download.concurrency", Value: 2, Source: "default"},
		{Key: "download.on_exists", Value: "skip", Source: "file"},
		{Key: "download.boxart", Value: true, Source: "default"},
		{Key: "download.prefer.languages", Value: []string{"en", "pt"}, Source: "file"},
		{Key: "download.prefer.size", Value: "none", Source: "default"},
		{Key: "gui.confirm_button", Value: "south", Source: "default"},
		{Key: "gui.text_size", Value: "normal", Source: "default"},
		{Key: "gui.safe_area", Value: "auto", Source: "default"},
		{Key: "interface.language", Value: "auto", Source: "default"},
		{Key: "interface.confirm_downloads", Value: true, Source: "default"},
	}
	svc.Cache = cache.Stats{TotalBytes: 84 << 20}
	return svc
}

// openTile walks Home to a tile and opens it.
func openTile(t *testing.T, h *harness, tile string) {
	t.Helper()
	for i := 0; i < 10 && h.m.focus != tile; i++ {
		if i%2 == 0 {
			h.press(input.Down)
		} else {
			h.press(input.Right)
		}
	}
	if h.m.focus != tile {
		t.Fatalf("could not focus the tile %q: %q", tile, h.m.focus)
	}
	h.press(input.Confirm)
}

func openSettingsFromHome(t *testing.T, svc *guitest.Fake, w, hh int) *harness {
	t.Helper()
	h := started(t, svc, w, hh)
	openTile(t, h, tileSettings)
	if h.m.screen != screenSettings {
		t.Fatalf("screen %d, want Settings", h.m.screen)
	}
	return h
}

func TestSettingsShowRowsValuesAndChangeInPlace(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	h.mustHave("mothdex › Settings", "Downloads", "Concurrent downloads", "When a file exists", "Skip", "Save cover art", "On",
		"Preferred languages", "En, Pt", "Preferred size", "Any", "Screen", "Confirm button", "Bottom (A)", "Screen margin", "Auto (5%)")
	h.mustHave("default", "file")

	h.press(input.Right) // the stepper changes in place: 2 to 3
	if svc.Count("config_set:download.concurrency=3") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.mustHave("Applies on next start")
	h.press(input.Left)
	if svc.Count("config_set:download.concurrency=2") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}

	h.press(input.Down) // When a file exists: a file value, Right cycles to Rename
	h.press(input.Right)
	if svc.Count("config_set:download.on_exists=rename") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Right) // the end of the list: nothing more
	if svc.Count("config_set:download.on_exists=") != 1 {
		t.Fatalf("a cycle does not wrap: %v", svc.Calls())
	}
	h.press(input.Download) // X resets a value that comes from the file
	if svc.Count("config_reset:download.on_exists") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Download) // now the default: nothing to reset
	if svc.Count("config_reset:download.on_exists") != 1 {
		t.Fatal("a default value has nothing to reset")
	}

	h.press(input.Down) // Save cover art: A flips it
	h.press(input.Confirm)
	if svc.Count("config_set:download.boxart=false") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.mustHave("Off")
	if h.m.o.Boxart {
		t.Fatal("applies now: the model stops saving covers")
	}
}

// A value set by the environment or a flag, and overwrite, cannot be changed here (docs/ux/gui.md section 5.10).
func TestSettingsReadOnlyRowsSayWhy(t *testing.T) {
	svc := settingsFixture()
	svc.Settings[0] = app.Setting{Key: "download.concurrency", Value: 4, Source: "env"}
	svc.Settings[1] = app.Setting{Key: "download.on_exists", Value: "overwrite", Source: "file"}
	h := openSettingsFromHome(t, svc, 1280, 720)
	h.mustHave("Set by MOTHDEX_DOWNLOAD_CONCURRENCY")
	h.press(input.Right)
	h.press(input.Left)
	h.press(input.Download)
	if len(svc.Calls()) > 0 && (svc.Count("config_set") != 0 || svc.Count("config_reset") != 0) {
		t.Fatalf("a row set by the environment is read-only: %v", svc.Calls())
	}
	h.press(input.Down)
	h.mustHave("Overwrite", "Change it from the command line.")
	h.press(input.Confirm)
	h.press(input.Right)
	if svc.Count("config_set") != 0 {
		t.Fatalf("overwrite is never written: %v", svc.Calls())
	}
}

func TestSettingsShowAServiceErrorUnderTheRowAndKeepTheValue(t *testing.T) {
	svc := settingsFixture()
	svc.ConfigSetErr = map[string]error{"download.concurrency": &app.Error{Code: app.CodeInvalidConfig, Message: "out of range", Args: map[string]any{"key": "download.concurrency", "value": "3", "source": "file"}}}
	h := openSettingsFromHome(t, svc, 1280, 720)
	h.press(input.Right)
	h.mustHave("Not saved: Invalid setting download.concurrency=3 (from file).")
	if h.m.settingString("download.concurrency") != "2" {
		t.Fatal("the old value stays")
	}
}

func TestSettingsLanguageEditorAddsMovesAndRemoves(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	for i := 0; i < 3; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm) // Preferred languages
	h.mustHave("En", "Pt", "+ Add")
	h.press(input.Down)
	h.press(input.Down)
	h.press(input.Confirm) // + Add
	h.mustHave("Fr", "De")
	h.mustNotHave("En", "Pt")
	h.press(input.Confirm) // Fr, the first one that is not chosen yet
	if svc.Count("config_set:download.prefer.languages=en,pt,fr") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Confirm) // back on the list with focus on Fr: its menu
	h.mustHave("Move up", "Remove")
	h.mustNotHave("Move down") // it is the last one
	h.press(input.Confirm)     // Move up
	if svc.Count("config_set:download.prefer.languages=en,fr,pt") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Confirm) // the list again, focus on Fr, now in the middle
	h.press(input.Down)
	h.press(input.Down)
	h.press(input.Confirm) // Remove
	if calls := svc.Calls(); calls[len(calls)-1] != "config_set:download.prefer.languages=en,pt" {
		t.Fatalf("calls %v", calls)
	}
	h.press(input.Back)
	h.mustNotHave("+ Add")
}

func TestSettingsMarginCalibrationSavesOnAAndCancelsOnB(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	for i := 0; i < 7; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm) // Screen margin: the calibration view, at Auto's 5%
	h.mustHave("5%", "Adjust until you can see all four corners.")
	h.press(input.Right)
	h.press(input.Right)
	h.mustHave("7%")
	h.press(input.Back) // B leaves without saving
	if svc.Count("config_set:gui.safe_area") != 0 {
		t.Fatalf("B must not save: %v", svc.Calls())
	}
	h.press(input.Confirm)
	h.press(input.Left)
	h.press(input.Confirm) // A saves 4
	if svc.Count("config_set:gui.safe_area=4") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	if h.m.cfg.safe != (SafeArea{Percent: 4}) {
		t.Fatalf("the margin applies now: %+v", h.m.cfg.safe)
	}
}

func TestSettingsCacheAsksBeforeClearing(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	for i := 0; i < 10; i++ {
		h.press(input.Down)
	}
	h.mustHave("84.0 MB", "Cache")
	h.press(input.Confirm)
	h.mustHave("Clear cache (84.0 MB)?", "Keep", "Clear")
	if h.m.dlgFocus != btnKeep {
		t.Fatalf("default %q: Keep", h.m.dlgFocus)
	}
	h.advance(200 * time.Millisecond)
	h.press(input.Back) // B is Keep
	if svc.Count("cache_clear") != 0 {
		t.Fatal("B keeps the cache")
	}
	h.press(input.Confirm)
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if svc.Count("cache_clear") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.mustHave("Cache cleared.", "0 B")
}

// Settings stays usable at 640x480: it scrolls, the last row can be reached and a section header never ends the page.
func TestSettingsScrollAtCompactAndKeepHeadersWithTheirRows(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 640, 480)
	for i := 0; i < 12; i++ {
		h.press(input.Down)
	}
	h.mustHave("About")
	h.mustNotHave("When a file exists") // scrolled away
	h.press(input.Confirm)
	h.mustHave("Version: 0.3.0")
	h.press(input.Back)
	for i := 0; i < 12; i++ {
		h.press(input.Up)
	}
	h.mustHave("Concurrent downloads", "Downloads")
}

func TestSettingsTextSizeAppliesNow(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	for i := 0; i < 6; i++ {
		h.press(input.Down)
	} // Text size
	before := h.m.metrics.Body
	h.press(input.Right)
	if !h.m.cfg.largeText || h.m.metrics.Body <= before {
		t.Fatalf("large text applies now: %v %d -> %d", h.m.cfg.largeText, before, h.m.metrics.Body)
	}
}

// The second page of Settings: Language, Confirm large downloads and the line about the command line.
func TestSettingsSecondPageHasLanguageConfirmAndTheCommandLineNote(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	for i := 0; i < 12; i++ {
		h.press(input.Down)
	}
	h.mustHave("Language", "Auto", "Confirm large downloads", "On", "Storage and system", "Cache", "System", "About")
	h.mustHave("More settings: run \"mothdex config list\" over SSH.")
	for i := 0; i < 3; i++ {
		h.press(input.Up)
	}
	h.press(input.Left) // the language row is not first any more: Right on it picks the first locale in place
	h.press(input.Right)
}

// The lists that Settings opens name what they are about, since the scrim hides the screen that asked (Bússola, m3).
func TestSettingsEditorsHaveATitle(t *testing.T) {
	svc := settingsFixture()
	h := openSettingsFromHome(t, svc, 1280, 720)
	h.press(input.Down)
	h.press(input.Confirm) // When a file exists
	h.mustHave("When a file exists", "Skip", "Rename")
	h.press(input.Back)
	for i := 0; i < 2; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm) // Preferred languages
	h.mustHave("Preferred languages", "En", "+ Add")
	h.press(input.Confirm) // En: its sheet is titled by the language
	h.mustHave("En", "Move down", "Remove")
}

// A label that is wider than the column wraps to a second line instead of being cut, in the longest languages at
// the smallest size (Bússola, m1).
func TestSettingsLabelsAreNeverCut(t *testing.T) {
	for _, lang := range []string{"es_es", "ru_ru", "pt_br"} {
		svc := settingsFixture()
		h := started(t, svc, 640, 480, func(o *Options) { o.T = shared.bundle.New(lang) })
		h.press(input.Menu)
		for i := 0; i < 3; i++ {
			h.press(input.Down)
		}
		h.press(input.Confirm)
		for i := 0; i < 12; i++ {
			h.press(input.Down)
		}
		for _, line := range h.texts() {
			if strings.Contains(line, "…") && !strings.Contains(line, "/") {
				t.Errorf("%s: %q is cut", lang, line)
			}
		}
	}
}
