// Package buildinfo exposes the version, commit and commit date of the running
// binary. Release builds set the variables with -ldflags -X (see
// .goreleaser.yaml); builds made with `go install` or `go build` fall back to
// the information Go embeds in the binary.
package buildinfo

import "runtime/debug"

// Set at link time by GoReleaser. Empty means "not set".
var (
	// Version is the semantic version without the leading "v".
	Version string
	// Commit is the full git commit hash.
	Commit string
	// Date is the commit time in RFC3339, UTC (not the build time, so builds are reproducible).
	Date string
)

// Info describes a build. Fields are empty when unknown; there are no placeholder strings.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get returns the build information of the running binary.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(Info{Version: Version, Commit: Commit, Date: Date}, bi, ok)
}

// resolve fills the fields missing from linked, using the embedded build info.
func resolve(linked Info, bi *debug.BuildInfo, ok bool) Info {
	if !ok || bi == nil {
		return linked
	}
	out := linked
	if out.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		out.Version = trimV(bi.Main.Version)
	}
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && out.Commit == "":
			out.Commit = s.Value
		case s.Key == "vcs.time" && out.Date == "":
			out.Date = s.Value
		}
	}
	return out
}

func trimV(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}

// String is the human-readable line printed by the binary.
func (i Info) String() string {
	version := i.Version
	if version == "" {
		version = "dev"
	}
	s := "mothdex " + version
	switch {
	case i.Commit != "" && i.Date != "":
		s += " (commit " + i.Commit + ", commit date " + i.Date + ")"
	case i.Commit != "":
		s += " (commit " + i.Commit + ")"
	}
	return s
}
