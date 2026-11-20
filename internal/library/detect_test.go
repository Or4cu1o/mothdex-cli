package library

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

// testEnv builds a Linux environment over a fake tree; every directory is writable
// unless listed in readOnly.
func testEnv(files map[string]string, readOnly ...string) Env {
	ro := map[string]bool{}
	for _, r := range readOnly {
		ro[r] = true
	}
	return Env{FS: mapFS(files), GOOS: "linux", Home: "/home/u", ExeDir: "/opt/mothdex",
		Writable: func(dir string) bool { return !ro[dir] }}
}

func osRelease(id, name string) string { return `NAME="` + name + `"` + "\nID=" + id + "\n" }

func detect(t *testing.T, env Env, cfg Config) ProfileInfo {
	t.Helper()
	cfg.GenericROMRoot = firstNonEmpty(cfg.GenericROMRoot, "/home/u/.local/state/mothdex/ROMs")
	return Detect(env, loadEmbedded(t), cfg)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func TestDetectEachSystem(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		cfg      Config
		goos     string
		wantID   string
		wantRoot string
	}{
		{"batocera", map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}, Config{}, "", "batocera", "/userdata/roms"},
		{"emuelec", map[string]string{"etc/os-release": osRelease("emuelec", "EmuELEC"), "storage/roms/.keep": ""}, Config{}, "", "emuelec", "/storage/roms"},
		{"lakka", map[string]string{"etc/os-release": osRelease("lakka", "Lakka"), "storage/roms/.keep": ""}, Config{}, "", "lakka", "/storage/roms"},
		{"recalbox", map[string]string{"recalbox/recalbox.version": "10", "recalbox/share/roms/.keep": ""}, Config{}, "", "recalbox", "/recalbox/share/roms"},
		{"emudeck", map[string]string{"home/u/Emulation/roms/.keep": ""}, Config{}, "", "emudeck", "/home/u/Emulation/roms"},
		{"emudeck on an SD card", map[string]string{"run/media/mmcblk0p1/Emulation/roms/.keep": ""}, Config{}, "", "emudeck", "/run/media/mmcblk0p1/Emulation/roms"},
		{"retrodeck", map[string]string{"home/u/retrodeck/roms/.keep": ""}, Config{}, "", "retrodeck", "/home/u/retrodeck/roms"},
		{"retrobat default path", map[string]string{"C:/RetroBat/RetroBat.exe": "", "C:/RetroBat/roms/.keep": ""}, Config{}, "windows", "retrobat", `C:\RetroBat\roms`},
		{"retrobat configured path", map[string]string{"D:/Games/RB/RetroBat.exe": "", "D:/Games/RB/retrobat.ini": "", "D:/Games/RB/roms/.keep": ""},
			Config{RetroBatPath: `D:\Games\RB`}, "windows", "retrobat", `D:\Games\RB\roms`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testEnv(tt.files)
			if tt.goos != "" {
				env.GOOS = tt.goos
			}
			info := detect(t, env, tt.cfg)
			if info.ID != tt.wantID || info.RomRoot != tt.wantRoot || info.Source != SourceAuto || info.Profile.Roms.Root != tt.wantRoot {
				t.Fatalf("got %s root %q source %s; warnings %v", info.ID, info.RomRoot, info.Source, info.Warnings)
			}
		})
	}
}

func TestDetectFailsSafeToGeneric(t *testing.T) {
	tests := []struct {
		name    string
		env     Env
		wantMsg string
	}{
		{"nothing matches", testEnv(map[string]string{"etc/hostname": "pc"}), "no known system"},
		{"root missing", testEnv(map[string]string{"usr/share/batocera/batocera.version": "40"}), "missing or not writable"},
		{"root read-only", testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}, "/userdata/roms"), "missing or not writable"},
		{"emudeck and retrodeck are ambiguous", testEnv(map[string]string{"home/u/Emulation/roms/.keep": "", "home/u/retrodeck/roms/.keep": ""}), "ambiguous"},
		{"windows profile on linux", testEnv(map[string]string{"C:/RetroBat/RetroBat.exe": "", "C:/RetroBat/roms/.keep": ""}), "no known system"},
		{"recalbox needs both probes", testEnv(map[string]string{"recalbox/share/roms/.keep": ""}), "no known system"},
		{"no home directory", func() Env {
			e := testEnv(map[string]string{"home/u/Emulation/roms/.keep": ""})
			e.Home = ""
			return e
		}(), "no known system"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := detect(t, tt.env, Config{})
			if info.ID != GenericID || info.RomRoot != "/home/u/.local/state/mothdex/ROMs" || !strings.Contains(strings.Join(info.Warnings, "|"), tt.wantMsg) {
				t.Fatalf("got %s root %q warnings %v, want generic with %q", info.ID, info.RomRoot, info.Warnings, tt.wantMsg)
			}
		})
	}
}

func TestPriorityPicksTheHighest(t *testing.T) {
	env := testEnv(map[string]string{
		"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": "",
		"home/u/Emulation/roms/.keep": "",
	})
	if info := detect(t, env, Config{}); info.ID != "batocera" {
		t.Fatalf("batocera (100) must beat emudeck (50), got %s", info.ID)
	}
}

func TestExplicitChoiceWins(t *testing.T) {
	env := testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""})
	tests := []struct {
		name   string
		cfg    Config
		wantID string
		source string
	}{
		{"flag beats config and auto", Config{Flag: "generic", ID: "batocera"}, "generic", SourceFlag},
		{"config beats auto", Config{ID: "generic"}, "generic", SourceConfig},
		{"auto in config means detect", Config{ID: "auto"}, "batocera", SourceAuto},
		{"flag auto means detect", Config{Flag: "auto", ID: "generic"}, "generic", SourceConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if info := detect(t, env, tt.cfg); info.ID != tt.wantID || info.Source != tt.source {
				t.Fatalf("got %s/%s", info.ID, info.Source)
			}
		})
	}
	// A forced profile is used even when its probes fail, with a warning.
	info := detect(t, testEnv(map[string]string{"etc/hostname": "pc"}), Config{ID: "emuelec", ROMRoot: "/mnt/roms"})
	joined := strings.Join(info.Warnings, "|")
	if info.ID != "emuelec" || info.RomRoot != "/mnt/roms" || !strings.Contains(joined, "chosen explicitly") || !strings.Contains(joined, "missing or not writable") {
		t.Fatalf("forced: %s %q %v", info.ID, info.RomRoot, info.Warnings)
	}
	unknown := detect(t, env, Config{Flag: "amiga"})
	if unknown.ID != GenericID || !strings.Contains(unknown.Warnings[0], "unknown profile") {
		t.Fatalf("unknown id: %s %v", unknown.ID, unknown.Warnings)
	}
}

func TestROMRootOverrideAndFolderOverrides(t *testing.T) {
	env := testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "mnt/usb/roms/.keep": ""})
	cfg := Config{ROMRoot: "/mnt/usb/roms", Folders: map[string]string{"scd": "segacd", "mame": "arcade", "nes": "ports", "gba": "a/b", "BAD": "x"}}
	info := detect(t, env, cfg)
	if info.ID != "batocera" || info.RomRoot != "/mnt/usb/roms" {
		t.Fatalf("override: %s %q %v", info.ID, info.RomRoot, info.Warnings)
	}
	f := info.Profile.Roms.Folders
	if f["scd"] != "segacd" || f["mame"] != "arcade" || f["nes"] != "nes" || f["gba"] != "gba" {
		t.Fatalf("merged folders: %v", f)
	}
	if n := strings.Count(strings.Join(info.Warnings, "|"), "ignored"); n != 3 {
		t.Fatalf("want 3 ignored overrides (ports, a/b, BAD), warnings %v", info.Warnings)
	}
	if loaded := loadEmbedded(t); loaded[0].Roms.Folders["scd"] == "segacd" && loaded[0].ID == "batocera" {
		t.Fatal("overrides must not mutate the loaded profile")
	}
}

func TestUnverifiedItemsAreListed(t *testing.T) {
	rb := detect(t, testEnv(map[string]string{"recalbox/recalbox.version": "10", "recalbox/share/roms/.keep": ""}), Config{})
	joined := strings.Join(rb.Unverified, "|")
	for _, want := range []string{"detection probe: dir /recalbox/share/roms", "detection probe: file /recalbox/recalbox.version", "folder names", "bin dir", "refresh hook: manual"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, rb.Unverified)
		}
	}
	if rb.Verified || !strings.Contains(strings.Join(rb.Warnings, "|"), "unverified") {
		t.Fatal("recalbox has unverified items")
	}
	bat := detect(t, testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}), Config{})
	if len(bat.Unverified) != 1 || !strings.Contains(bat.Unverified[0], "bin dir") {
		t.Fatalf("batocera: only the bin dir is unverified, got %v", bat.Unverified)
	}
	if g := detect(t, testEnv(nil), Config{}); !g.Verified || len(g.Unverified) != 0 {
		t.Fatalf("generic has nothing unverified: %v", g.Unverified)
	}
}

func TestBazzitIsAHostNotALayout(t *testing.T) {
	both := testEnv(map[string]string{"etc/os-release": osRelease("bazzite", "Bazzite"), "home/u/Emulation/roms/.keep": "", "home/u/retrodeck/roms/.keep": ""})
	info := detect(t, both, Config{})
	if info.ID != GenericID || info.Host != "bazzite" || !strings.Contains(strings.Join(info.Warnings, "|"), "profile.id") {
		t.Fatalf("both layouts on Bazzite: %s host %q %v", info.ID, info.Host, info.Warnings)
	}
	one := detect(t, testEnv(map[string]string{"etc/os-release": osRelease("bazzite", "Bazzite"), "home/u/Emulation/roms/.keep": ""}), Config{})
	if one.ID != "emudeck" || one.Host != "bazzite" {
		t.Fatalf("EmuDeck on Bazzite: %s host %q", one.ID, one.Host)
	}
}

func TestProbeErrorsCountAsNotMatched(t *testing.T) {
	env := testEnv(nil)
	env.FS = failingFS{mapFS(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""})}
	if info := detect(t, env, Config{}); info.ID != GenericID {
		t.Fatalf("an unreadable tree must give generic, got %s", info.ID)
	}
}

// failingFS reports a permission error for every stat and read.
type failingFS struct{ fs.FS }

func (failingFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func TestDetectIsPure(t *testing.T) {
	env := testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""})
	profiles := loadEmbedded(t)
	cfg := Config{GenericROMRoot: "/g", Folders: map[string]string{"scd": "segacd"}}
	a, b := Detect(env, profiles, cfg), Detect(env, profiles, cfg)
	if a.ID != b.ID || a.RomRoot != b.RomRoot || len(a.Warnings) != len(b.Warnings) || a.Profile.Roms.Folders["scd"] != b.Profile.Roms.Folders["scd"] {
		t.Fatal("two runs over the same input must agree")
	}
	if profiles[0].Roms.Folders["scd"] == "segacd" && profiles[0].ID == "batocera" {
		t.Fatal("input profiles were modified")
	}
}

func TestOSReleaseParsing(t *testing.T) {
	fsys := mapFS(map[string]string{"etc/os-release": "# c\nNAME=\"Lakka\"\nID='lakka'\nX=1=2\n\nBAD\n"})
	got := readOSRelease(fsys)
	if got["NAME"] != "Lakka" || got["ID"] != "lakka" || got["X"] != "1=2" {
		t.Fatalf("parsed %v", got)
	}
	if osReleaseMatches(Env{FS: fsys}, map[string]string{"ID": "Lakka"}) {
		t.Fatal("matching is case-sensitive")
	}
}

func TestGlobIsLimitedToExistingMatches(t *testing.T) {
	env := testEnv(map[string]string{"run/media/b/Emulation/roms/.keep": "", "run/media/a/Emulation/roms/.keep": ""})
	info := detect(t, env, Config{})
	if info.ID != "emudeck" || info.RomRoot != "/run/media/a/Emulation/roms" {
		t.Fatalf("the first sorted glob match wins: %s %q", info.ID, info.RomRoot)
	}
}
