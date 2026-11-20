package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	embedded := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-11-20T10:00:00Z"},
		},
	}
	tests := []struct {
		name   string
		linked Info
		bi     *debug.BuildInfo
		ok     bool
		want   Info
	}{
		{"linked values win", Info{"0.1.0", "ldcommit", "2026-01-01T00:00:00Z"}, embedded, true, Info{"0.1.0", "ldcommit", "2026-01-01T00:00:00Z"}},
		{"fallback fills gaps and trims v", Info{}, embedded, true, Info{"1.2.3", "abc123", "2026-11-20T10:00:00Z"}},
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
	tests := []struct {
		info Info
		want string
	}{
		{Info{}, "mothdex dev"},
		{Info{Version: "0.1.0", Commit: "abc", Date: "2026-11-20T10:00:00Z"}, "mothdex 0.1.0 (commit abc, commit date 2026-11-20T10:00:00Z)"},
		{Info{Version: "0.1.0", Commit: "abc"}, "mothdex 0.1.0 (commit abc)"},
	}
	for _, tt := range tests {
		if got := tt.info.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
