package library

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// validateProbes checks the shape of every probe of the profile.
func (p Profile) validateProbes() error {
	for _, pr := range append(append([]Probe{}, p.Detect.All...), p.Detect.Any...) {
		if err := pr.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (pr Probe) validate() error {
	kinds := 0
	for _, set := range []bool{pr.File != "", len(pr.Files) > 0, pr.Dir != "", pr.Glob != "", len(pr.OSRelease) > 0, pr.Config != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return errors.New("a probe must set exactly one of file, files, dir, glob, os_release or config")
	}
	if pr.Config != "" && !configKeys[pr.Config] {
		return errors.New("probe config key is not readable by profiles: " + pr.Config)
	}
	if pr.Glob != "" && wildcardLevels(pr.Glob) > 2 {
		return errors.New("glob has more than two wildcard levels")
	}
	for _, s := range append([]string{pr.File, pr.Dir, pr.Glob}, pr.Files...) {
		if err := checkVariables(s); err != nil {
			return err
		}
		if strings.Contains(s, "${MATCH}") {
			return errors.New("${MATCH} is only valid in paths that follow detection")
		}
	}
	return nil
}

// wildcardLevels counts the path segments of a glob that contain wildcards.
func wildcardLevels(glob string) int {
	n := 0
	for _, seg := range strings.FieldsFunc(glob, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.ContainsAny(seg, "*?[") {
			n++
		}
	}
	return n
}

// describe names a probe for the list of unverified items.
func (pr Probe) describe() string {
	switch {
	case pr.File != "":
		return "file " + pr.File
	case len(pr.Files) > 0:
		return "files " + strings.Join(pr.Files, ", ")
	case pr.Dir != "":
		return "dir " + pr.Dir
	case pr.Glob != "":
		return "glob " + pr.Glob
	case len(pr.OSRelease) > 0:
		return "os-release " + formatPairs(pr.OSRelease)
	default:
		return "config " + pr.Config
	}
}

func formatPairs(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	return strings.Join(parts, " ")
}

// fsPath turns an absolute host path into a path of the fs.FS rooted at "/": separators
// become "/" and the leading slash goes ("C:\\x" becomes "C:/x").
func fsPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimLeft(p, "/")
	return path.Clean(p)
}

// match is the outcome of a probe.
type match struct {
	ok   bool
	path string // directory or glob match found, for ${MATCH}
}

// run evaluates a probe against the environment. A probe that errors counts as not matched.
func (pr Probe) run(env Env, v values) match {
	switch {
	case pr.File != "":
		return fileMatch(env, v, pr.File)
	case len(pr.Files) > 0:
		return filesMatch(env, v, pr.Files)
	case pr.Dir != "":
		return dirMatch(env, v, pr.Dir)
	case pr.Glob != "":
		return globMatch(env, v, pr.Glob)
	case len(pr.OSRelease) > 0:
		return match{ok: osReleaseMatches(env, pr.OSRelease)}
	default:
		return configMatch(env, v, pr.Config)
	}
}

func fileMatch(env Env, v values, raw string) match {
	p, err := v.expand(raw)
	if err != nil || !isFile(env.FS, p) {
		return match{}
	}
	return match{ok: true, path: dirOf(p)}
}

func filesMatch(env Env, v values, raw []string) match {
	first := ""
	for i, r := range raw {
		p, err := v.expand(r)
		if err != nil || !isFile(env.FS, p) {
			return match{}
		}
		if i == 0 {
			first = dirOf(p)
		}
	}
	return match{ok: true, path: first}
}

func dirMatch(env Env, v values, raw string) match {
	p, err := v.expand(raw)
	if err != nil || !isDir(env.FS, p) {
		return match{}
	}
	return match{ok: true, path: p}
}

func globMatch(env Env, v values, raw string) match {
	p, err := v.expand(raw)
	if err != nil {
		return match{}
	}
	hits, err := fs.Glob(env.FS, fsPath(p))
	if err != nil || len(hits) == 0 {
		return match{}
	}
	sort.Strings(hits)
	return match{ok: true, path: "/" + hits[0]}
}

// configMatch passes when the config value is non-empty and the path exists.
func configMatch(env Env, v values, key string) match {
	val := v.config[key]
	if val == "" || (!isDir(env.FS, val) && !isFile(env.FS, val)) {
		return match{}
	}
	return match{ok: true, path: val}
}

func isFile(fsys fs.FS, p string) bool {
	st, err := fs.Stat(fsys, fsPath(p))
	return err == nil && !st.IsDir()
}

func isDir(fsys fs.FS, p string) bool {
	st, err := fs.Stat(fsys, fsPath(p))
	return err == nil && st.IsDir()
}

// dirOf returns the directory part of a path written with either separator.
func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i > 0 {
		return p[:i]
	}
	return p
}

// osReleaseMatches reads /etc/os-release and requires every pair to match exactly.
func osReleaseMatches(env Env, want map[string]string) bool {
	got := readOSRelease(env.FS)
	for k, v := range want {
		if have, ok := got[k]; !ok || have != v {
			return false
		}
	}
	return true
}

func readOSRelease(fsys fs.FS) map[string]string {
	raw, err := fs.ReadFile(fsys, "etc/os-release")
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		out[k] = strings.Trim(v, `"'`)
	}
	return out
}
