package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	build := func(version string) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Version: version},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
				{Key: "vcs.time", Value: "2026-11-20T10:00:00Z"},
			},
		}
	}
	tests := []struct {
		name   string
		linked Info
		bi     *debug.BuildInfo
		ok     bool
		want   Info
	}{
		{"linked values win", Info{Version: "0.1.0", Commit: "ldcommit", Date: "2026-01-01T00:00:00Z"}, build("v1.2.3"), true,
			Info{Version: "0.1.0", Commit: "ldcommit", Date: "2026-01-01T00:00:00Z"}},
		{"module release version is used without the v", Info{}, build("v0.2.0-rc.1"), true,
			Info{Version: "0.2.0-rc.1", Commit: "abc123", Date: "2026-11-20T10:00:00Z"}},
		{"pseudo-version is ignored", Info{}, build("v0.0.0-20261120225344-78edbf3e047a"), true,
			Info{Commit: "abc123", Date: "2026-11-20T10:00:00Z"}},
		{"pseudo-version with dirty is ignored", Info{}, build("v0.1.1-0.20261120225344-78edbf3e047a+dirty"), true,
			Info{Commit: "abc123", Date: "2026-11-20T10:00:00Z"}},
		{"plain dirty version is ignored", Info{}, build("v0.3.0+dirty"), true,
			Info{Commit: "abc123", Date: "2026-11-20T10:00:00Z"}},
		{"devel version stays empty", Info{}, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, Info{}},
		{"no build info", Info{Version: "0.1.0"}, nil, false, Info{Version: "0.1.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolve(tt.linked, tt.bi, tt.ok); got != tt.want {
				t.Fatalf("resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	const sha = "1a2b3c4d5e6f7a8b9c0d1a2b3c4d5e6f7a8b9c0d"
	tests := []struct {
		name string
		info Info
		want string
	}{
		{"dev build", Info{}, "mothdex dev"},
		{"empty version wins over commit", Info{Commit: sha, Date: "2026-11-20T10:00:00Z"}, "mothdex dev"},
		{"full", Info{Version: "0.1.0", Commit: sha, Date: "2026-11-20T10:00:00Z"}, "mothdex 0.1.0 (commit 1a2b3c4, 2026-11-20)"},
		{"commit without date", Info{Version: "0.1.0", Commit: sha}, "mothdex 0.1.0 (commit 1a2b3c4)"},
		{"date without commit", Info{Version: "0.1.0", Date: "2026-11-20T10:00:00Z"}, "mothdex 0.1.0 (2026-11-20)"},
		{"short commit is kept", Info{Version: "0.1.0", Commit: "abc"}, "mothdex 0.1.0 (commit abc)"},
		{"version only", Info{Version: "0.1.0"}, "mothdex 0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetAlwaysSetsToolchainFields(t *testing.T) {
	got := Get()
	if got.Go == "" || got.OS == "" || got.Arch == "" {
		t.Fatalf("Get() left go/os/arch empty: %+v", got)
	}
}
