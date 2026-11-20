// Package buildinfo exposes the version, commit and commit date of the running
// binary, plus the Go toolchain, OS and architecture (contract: cli-output.md
// section 3.1). Release builds set the variables with -ldflags -X (see
// .goreleaser.yaml); builds made with `go install module@version` fall back to
// the information Go embeds in the binary. Plain `go build` and `go run` builds
// have an empty version and print "mothdex dev".
package buildinfo

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set at link time by GoReleaser. Empty means "not set".
var (
	// Version is the semantic version without the leading "v".
	Version string
	// Commit is the full git commit hash.
	Commit string
	// Date is the commit time in RFC3339, UTC (not the build time, so builds are reproducible).
	Date string
)

const (
	shortCommitLen = len("1a2b3c4")
	dateLen        = len("2006-01-02")
)

// pseudoVersion matches the module versions Go stamps on builds made inside a
// git checkout (v0.0.0-20261120225344-78edbf3e047a, and after a tag
// v0.1.1-0.20261120225344-78edbf3e047a or v0.2.0-rc.1.0.20261120225344-78edbf3e047a,
// each with an optional +dirty) and any "+dirty" version: these are not releases.
var pseudoVersion = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}(\+dirty)?$|\+dirty$`)

// Info describes a build. Version, Commit and Date are empty when unknown (never
// placeholder strings); Go, OS and Arch are always set.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get returns the build information of the running binary.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	info := resolve(Info{Version: Version, Commit: Commit, Date: Date}, bi, ok)
	info.Go = runtime.Version()
	info.OS = runtime.GOOS
	info.Arch = runtime.GOARCH
	return info
}

// resolve fills the fields missing from linked, using the embedded build info.
func resolve(linked Info, bi *debug.BuildInfo, ok bool) Info {
	if !ok || bi == nil {
		return linked
	}
	out := linked
	if out.Version == "" && isRelease(bi.Main.Version) {
		out.Version = strings.TrimPrefix(bi.Main.Version, "v")
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

// isRelease reports whether a module version is a real release (not empty,
// "(devel)" or a pseudo/dirty version).
func isRelease(v string) bool {
	return v != "" && v != "(devel)" && !pseudoVersion.MatchString(v)
}

// String is the human-readable line: "mothdex 0.1.0 (commit 1a2b3c4, 2026-11-20)"
// (short commit and date only), or "mothdex dev" when the version is empty.
func (i Info) String() string {
	if i.Version == "" {
		return "mothdex dev"
	}
	var details []string
	if i.Commit != "" {
		details = append(details, "commit "+truncate(i.Commit, shortCommitLen))
	}
	if i.Date != "" {
		details = append(details, truncate(i.Date, dateLen))
	}
	s := "mothdex " + i.Version
	if len(details) > 0 {
		s += " (" + strings.Join(details, ", ") + ")"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
