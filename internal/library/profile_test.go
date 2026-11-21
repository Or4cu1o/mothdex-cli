package library

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/profiles"
)

// platformSet reads the Mothdex platform ids from the recorded /platforms fixture.
func platformSet(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../../pkg/mothdex/testdata/api-v1/platforms.json")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var env struct {
		Data struct {
			Platforms map[string]json.RawMessage `json:"platforms"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	set := make(map[string]bool, len(env.Data.Platforms))
	for id := range env.Data.Platforms {
		set[id] = true
	}
	return set
}

func loadEmbedded(t *testing.T) []Profile {
	t.Helper()
	res := LoadAll(profiles.FS, nil, LoadOptions{Platforms: platformSet(t)})
	if len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("embedded profiles: errors %v warnings %v", res.Errors, res.Warnings)
	}
	return res.Profiles
}

func TestEmbeddedProfilesAreValid(t *testing.T) {
	list := loadEmbedded(t)
	want := map[string]int{"batocera": 100, "emuelec": 90, "lakka": 90, "recalbox": 80, "retrobat": 80, "emudeck": 50, "retrodeck": 50, "generic": 0}
	if len(list) != len(want) {
		t.Fatalf("got %d profiles, want %d", len(list), len(want))
	}
	for _, p := range list {
		if prio, ok := want[p.ID]; !ok || p.Priority != prio {
			t.Errorf("%s: priority %d, want %d", p.ID, p.Priority, prio)
		}
	}
}

func TestEmbeddedFolderMaps(t *testing.T) {
	byID := map[string]Profile{}
	for _, p := range loadEmbedded(t) {
		byID[p.ID] = p
	}
	// Names differing per frontend (T1e section 4): Mega CD is megacd on Batocera and RetroBat only.
	for id, want := range map[string]string{"batocera": "megacd", "retrobat": "megacd", "emuelec": "segacd", "recalbox": "segacd", "emudeck": "segacd", "retrodeck": "segacd"} {
		if got := byID[id].Roms.Folders["scd"]; got != want {
			t.Errorf("%s: scd -> %q, want %q", id, got, want)
		}
	}
	if byID["batocera"].Roms.Folders["ps1"] != "psx" || byID["emudeck"].Roms.Folders["md"] != "megadrive" {
		t.Error("ps1 and md names")
	}
	if _, mapped := byID["batocera"].Roms.Folders["mame"]; mapped {
		t.Error("arcade ids are deliberately unmapped")
	}
	if byID["lakka"].Roms.Layout != LayoutPerPlatformID || byID["generic"].Roms.Layout != LayoutPerPlatformID {
		t.Error("lakka and generic use per_platform_id")
	}
}

// TestEmbeddedFoldersAvoidLauncherDirs checks every mapped folder of every embedded profile
// (T-15c, T15b research): it is a valid folder name, its destination is not a launcher path,
// and arcade ids and pc stay unmapped (profiles.md section 5).
func TestEmbeddedFoldersAvoidLauncherDirs(t *testing.T) {
	unmapped := []string{"mame", "hbmame", "fbn", "naomi", "naomi2", "chih", "tri", "lind", "s246", "pc"}
	for _, p := range loadEmbedded(t) {
		sep, root := "/", "/roms" // a stand-in root: the rule under test is root + folder
		if p.OS == "windows" {
			sep, root = `\`, `C:\RetroBat\roms`
		}
		p.ownRoot = root
		for id, folder := range p.Roms.Folders {
			if err := config.ValidateFolderName(folder); err != nil {
				t.Errorf("%s: %s -> %q: %v", p.ID, id, folder, err)
			}
			if p.IsLauncherPath(root + sep + folder) {
				t.Errorf("%s: %s -> %q lands in a launcher folder", p.ID, id, folder)
			}
		}
		for _, id := range unmapped {
			if f, ok := p.Roms.Folders[id]; ok {
				t.Errorf("%s: %s must stay unmapped, got %q", p.ID, id, f)
			}
		}
	}
}

// TestEmbeddedFolderNamesFromT15b spot-checks names that differ per frontend (T15b table).
func TestEmbeddedFolderNamesFromT15b(t *testing.T) {
	byID := map[string]Profile{}
	for _, p := range loadEmbedded(t) {
		byID[p.ID] = p
	}
	for _, tt := range []struct{ profile, id, want string }{
		{"emuelec", "dos", "pc"}, {"emuelec", "jag", "atarijaguar"}, {"emuelec", "ngcd", "neocd"},
		{"emudeck", "gc", "gc"}, {"retrodeck", "cdi", "cdimono1"}, {"emudeck", "3ds", "n3ds"},
		{"batocera", "gc", "gamecube"}, {"batocera", "n3ds", "3ds"}, {"retrobat", "64dd", "n64dd"},
		{"recalbox", "64dd", "64dd"}, {"batocera", "tgcd", "pcenginecd"}, {"emuelec", "tg16", "pcengine"},
	} {
		if got := byID[tt.profile].Roms.Folders[tt.id]; got != tt.want {
			t.Errorf("%s: %s -> %q, want %q", tt.profile, tt.id, got, tt.want)
		}
	}
	for _, missing := range []struct{ profile, id string }{{"emuelec", "ps2"}, {"recalbox", "wiiu"}, {"batocera", "dsi"}} {
		if _, ok := byID[missing.profile].Roms.Folders[missing.id]; ok {
			t.Errorf("%s: %s has no system in that frontend and must be unmapped", missing.profile, missing.id)
		}
	}
}

const validBase = `schema: 1
id: test
name: Test
os: linux
priority: 10
roms: { root: /roms, layout: per_system, unmapped: error, folders: { nes: nes } }
launcher: { kind: sh, dir: /roms/ports }
refresh: { kind: none }
`

func TestLoadRejections(t *testing.T) {
	replace := func(old, new string) string { return strings.Replace(validBase, old, new, 1) }
	tests := map[string]string{
		"schema":                     replace("schema: 1", "schema: 2"),
		"bad id":                     replace("id: test", "id: Bad_ID"),
		"bad os":                     replace("os: linux", "os: amiga"),
		"unknown field":              validBase + "extra: 1\n",
		"bad layout":                 replace("per_system", "flat"),
		"no root":                    replace("root: /roms, ", ""),
		"folder ports":               replace("nes: nes", "nes: ports"),
		"folder ports_scripts":       replace("nes: nes", "nes: ports_scripts"),
		"folder windows":             replace("nes: nes", "nes: Windows"),
		"folder launcher dir":        strings.Replace(replace("dir: /roms/ports", "dir: /roms/scripts"), "nes: nes", "nes: scripts", 1),
		"folder separator":           replace("nes: nes", "nes: a/b"),
		"folder dotdot":              replace("nes: nes", "nes: '..'"),
		"folder look-alike":          replace("nes: nes", "nes: portſ"),
		"folder key not an id":       replace("nes: nes", "'NES!': nes"),
		"unknown variable":           replace("root: /roms", "root: ${WHERE}/roms"),
		"stray dollar":               replace("root: /roms", "root: /roms/$x"),
		"two probe kinds":            replace("roms:", "detect: { all: [ { file: /a, dir: /b } ] }\nroms:"),
		"empty probe":                replace("roms:", "detect: { all: [ {} ] }\nroms:"),
		"glob three wildcards":       replace("roms:", "detect: { any: [ { glob: '/a/*/*/*' } ] }\nroms:"),
		"match inside a probe":       replace("roms:", "detect: { all: [ { dir: '${MATCH}' } ] }\nroms:"),
		"config key not allowed":     replace("roms:", "detect: { all: [ { config: paths.data } ] }\nroms:"),
		"bad launcher kind":          replace("kind: sh", "kind: exe"),
		"MATCH without a path probe": strings.Replace(replace("root: /roms", "root: ${MATCH}/roms"), "roms:", "detect: { all: [ { os_release: { ID: x } } ] }\nroms:", 1),
		"relative roms root":         replace("root: /roms", "root: roms"),
		"relative bin dir":           replace("launcher:", "bin_dir: { path: bin }\nlauncher:"),
		"launcher without dir":       replace(", dir: /roms/ports", ""),
		"url without http_get":       replace("{ kind: none }", "{ kind: none, url: 'http://127.0.0.1:1234/reloadgames' }"),
		"refresh timeout > 3s":       replace("{ kind: none }", "{ kind: http_get, url: 'http://127.0.0.1:1234/reloadgames', timeout: 10s }"),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Load([]byte(doc), LoadOptions{}); !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("want ErrInvalidProfile, got %v", err)
			}
		})
	}
	if _, _, err := Load([]byte(validBase), LoadOptions{}); err != nil {
		t.Fatalf("the base document must load: %v", err)
	}
}

func TestUnknownPlatformKeyIsAWarning(t *testing.T) {
	doc := strings.Replace(validBase, "nes: nes", "nes: nes, zx81: zx", 1)
	_, warns, err := Load([]byte(doc), LoadOptions{Platforms: map[string]bool{"nes": true}})
	if err != nil || len(warns) != 1 || !strings.Contains(warns[0], "zx81") {
		t.Fatalf("warnings %v, err %v", warns, err)
	}
}

func TestRefreshURLPolicy(t *testing.T) {
	ok := []string{"http://127.0.0.1:1234/reloadgames", "http://[::1]:1234/reloadgames"}
	bad := []string{
		"http://localhost:1234/reloadgames", "https://127.0.0.1:1234/reloadgames", "http://127.0.0.1:1235/reloadgames",
		"http://127.0.0.1/reloadgames", "http://127.0.0.1:1234/reloadgames?x=1", "http://127.0.0.1:1234/other",
		"http://127.0.0.1:1234/reloadgames#f", "http://user@127.0.0.1:1234/reloadgames", "http://127.0.0.2:1234/reloadgames",
		"http://192.168.1.5:1234/reloadgames", "http://[::2]:1234/reloadgames", "", "reloadgames", "http://127.0.0.1:1234/reloadgames/",
	}
	for _, u := range ok {
		if err := ValidateRefreshURL(u); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
	for _, u := range bad {
		if ValidateRefreshURL(u) == nil {
			t.Errorf("%q must be rejected", u)
		}
	}
}

func TestLoadAllUserOverridesAndRejects(t *testing.T) {
	user := mapFS(map[string]string{
		"batocera.yaml":  strings.Replace(strings.Replace(validBase, "id: test", "id: batocera", 1), "priority: 10", "priority: 7", 1),
		"mybox.yaml":     strings.Replace(validBase, "id: test", "id: mybox", 1),
		"evil.yaml":      strings.Replace(strings.Replace(validBase, "id: test", "id: evil", 1), "nes: nes", "nes: ports", 1),
		"wrongname.yaml": validBase,
	})
	res := LoadAll(profiles.FS, user, LoadOptions{})
	got := map[string]Profile{}
	for _, p := range res.Profiles {
		got[p.ID] = p
	}
	if got["batocera"].Priority != 7 || got["mybox"].ID == "" || got["evil"].ID != "" || got["test"].ID != "" {
		t.Fatalf("overrides: %+v", got)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("want 2 rejected files (evil, wrongname), got %v", res.Errors)
	}
}
