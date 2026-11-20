package config

import (
	"path/filepath"
	"testing"
)

func platform(goos, home, exeDir string, env map[string]string, existing ...string) Platform {
	files := map[string]bool{}
	for _, f := range existing {
		files[f] = true
	}
	return Platform{
		GOOS: goos, HomeDir: home, ExeDir: exeDir,
		Getenv: func(k string) string { return env[k] },
		Exists: func(p string) bool { return files[p] },
	}
}

func j(parts ...string) string { return filepath.Join(parts...) }

func TestResolvePathsPerOS(t *testing.T) {
	tests := []struct {
		name string
		p    Platform
		want ResolvedPaths
	}{
		{"linux defaults", platform("linux", "/home/u", "", nil), ResolvedPaths{
			ConfigFile: "/home/u/.config/mothdex/config.yaml", Temp: "/home/u/.cache/mothdex",
			Data: "/home/u/.local/state/mothdex", Logs: "/home/u/.local/state/mothdex/logs", ROMRoot: "/home/u/.local/state/mothdex/ROMs"}},
		{"linux XDG", platform("linux", "/home/u", "", map[string]string{
			"XDG_CONFIG_HOME": "/xc", "XDG_CACHE_HOME": "/xk", "XDG_STATE_HOME": "/xs"}), ResolvedPaths{
			ConfigFile: "/xc/mothdex/config.yaml", Temp: "/xk/mothdex", Data: "/xs/mothdex", Logs: "/xs/mothdex/logs", ROMRoot: "/xs/mothdex/ROMs"}},
		{"linux relative XDG ignored", platform("linux", "/home/u", "", map[string]string{"XDG_CONFIG_HOME": "rel"}), ResolvedPaths{
			ConfigFile: "/home/u/.config/mothdex/config.yaml", Temp: "/home/u/.cache/mothdex",
			Data: "/home/u/.local/state/mothdex", Logs: "/home/u/.local/state/mothdex/logs", ROMRoot: "/home/u/.local/state/mothdex/ROMs"}},
		{"darwin", platform("darwin", "/Users/u", "", nil), ResolvedPaths{
			ConfigFile: "/Users/u/Library/Application Support/mothdex/config.yaml", Temp: "/Users/u/Library/Caches/mothdex",
			Data: "/Users/u/Library/Application Support/mothdex", Logs: "/Users/u/Library/Logs/mothdex",
			ROMRoot: "/Users/u/Library/Application Support/mothdex/ROMs"}},
		{"windows", platform("windows", "H", "", map[string]string{"AppData": "A", "LocalAppData": "L"}), ResolvedPaths{
			ConfigFile: j("A", "mothdex", "config.yaml"), Temp: j("L", "mothdex", "cache"), Data: j("L", "mothdex"),
			Logs: j("L", "mothdex", "logs"), ROMRoot: j("L", "mothdex", "ROMs")}},
		{"windows without AppData vars", platform("windows", "H", "", nil), ResolvedPaths{
			ConfigFile: j("H", "AppData", "Roaming", "mothdex", "config.yaml"), Temp: j("H", "AppData", "Local", "mothdex", "cache"),
			Data: j("H", "AppData", "Local", "mothdex"), Logs: j("H", "AppData", "Local", "mothdex", "logs"),
			ROMRoot: j("H", "AppData", "Local", "mothdex", "ROMs")}},
		{"portable", platform("linux", "/home/u", "/opt/app", nil, j("/opt/app", "mothdex.yaml")), ResolvedPaths{
			ConfigFile: j("/opt/app", "mothdex.yaml"), Temp: j("/opt/app", "mothdex-data", "cache"), Data: j("/opt/app", "mothdex-data"),
			Logs: j("/opt/app", "mothdex-data", "logs"), ROMRoot: j("/opt/app", "ROMs"), Portable: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Expected values are written with "/" for readability; normalize to the host separator.
			want := tt.want
			for _, f := range []*string{&want.ConfigFile, &want.Temp, &want.Data, &want.Logs, &want.ROMRoot} {
				*f = filepath.FromSlash(*f)
			}
			got, err := ResolvePaths(tt.p, Config{}, "")
			if err != nil || got != want {
				t.Fatalf("got %+v, %v\nwant %+v", got, err, want)
			}
		})
	}
}

func TestConfigFilePrecedence(t *testing.T) {
	exe := "/opt/app"
	portable := j(exe, "mothdex.yaml")
	env := map[string]string{EnvConfig: "/from/env.yaml"}
	tests := []struct {
		name string
		p    Platform
		flag string
		want string
	}{
		{"flag first", platform("linux", "/h", exe, env, portable), "/from/flag.yaml", "/from/flag.yaml"},
		{"env second", platform("linux", "/h", exe, env, portable), "", "/from/env.yaml"},
		{"portable third", platform("linux", "/h", exe, nil, portable), "", portable},
		{"OS default last", platform("linux", "/h", exe, nil), "", filepath.FromSlash("/h/.config/mothdex/config.yaml")},
	}
	for _, tt := range tests {
		got, err := FilePath(tt.p, tt.flag)
		if err != nil || got != filepath.FromSlash(tt.want) && got != tt.want {
			t.Errorf("%s: %q, %v (want %q)", tt.name, got, err, tt.want)
		}
	}
}

func TestResolvePathsHonorsUserOverrides(t *testing.T) {
	p := platform("linux", "/h", "", nil)
	cfg := Config{Paths: Paths{Data: "/data"}}
	got, err := ResolvePaths(p, cfg, "")
	if err != nil || got.Data != "/data" || got.Logs != filepath.FromSlash("/data/logs") {
		t.Fatalf("data override should move logs: %+v, %v", got, err)
	}
	cfg = Config{Paths: Paths{Temp: "/t", Logs: "/l"}}
	if got, _ = ResolvePaths(p, cfg, ""); got.Temp != "/t" || got.Logs != "/l" {
		t.Fatalf("temp and logs overrides: %+v", got)
	}
}

func TestNoHomeIsAnError(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		if _, err := ResolvePaths(platform(goos, "", "", nil), Config{}, ""); err == nil {
			t.Errorf("%s without home and XDG must fail", goos)
		}
	}
}
