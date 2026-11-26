package log

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	// MaxBundleBytes is the size limit of the zip, so it can be attached to an issue.
	MaxBundleBytes = 10 << 20
	// DefaultBundleSessions is how many sessions a bundle holds unless asked otherwise.
	DefaultBundleSessions = 5
	// MaxBundleSessions is the largest BundleOptions.Sessions.
	MaxBundleSessions = 20

	bundleSessionBytes = 2 << 20 // the end of each session file
	bundleTailBytes    = 1 << 20 // the end of mothdex.log
	tailName           = "mothdex.log.tail"
)

// ErrOutDir is returned when the folder for the bundle is missing, a link or not a folder.
var ErrOutDir = errors.New("log: the output folder must be an existing folder, not a link")

// ErrSessions is returned for a session count outside 1..MaxBundleSessions.
var ErrSessions = errors.New("log: the number of sessions is out of range")

// ConfigEntry is one setting as `config list` shows it.
type ConfigEntry struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

// Manifest describes the machine and the configuration in the bundle. Version, system and the
// MOTHDEX_* variable names are filled by Bundle; Config values are masked there.
type Manifest struct {
	SchemaVersion int           `json:"schema_version"`
	Version       string        `json:"version"`
	Commit        string        `json:"commit"`
	OS            string        `json:"os"`
	Arch          string        `json:"arch"`
	Profile       string        `json:"profile"`
	Lang          string        `json:"lang"`
	Config        []ConfigEntry `json:"config"`
	Env           []string      `json:"env"`
}

// BundleOptions are the inputs of Bundle.
type BundleOptions struct {
	Dir      string // paths.logs
	OutDir   string // empty: Dir/bundles
	Sessions int    // newest sessions to add; 0 means DefaultBundleSessions
	Skip     string // a session id left out (the one-shot command that is bundling)
	Manifest Manifest
	Secrets  []string
	Home     string
	Environ  []string         // nil: os.Environ()
	Now      func() time.Time // nil: time.Now
	limit    int              // tests lower it; 0 means MaxBundleBytes
}

// BundleResult says what was written.
type BundleResult struct {
	Path     string   `json:"path"`
	Bytes    int64    `json:"bytes"`
	Sessions []string `json:"sessions"`
}

// BundleOptions of a session without a configuration: its folder, and nothing in the manifest but
// the build.
func (s *Session) BundleOptions() BundleOptions {
	_ = s.attachDefault() // Bundle reports a folder that cannot be used
	s.mu.Lock()
	defer s.mu.Unlock()
	return BundleOptions{
		Dir: s.dir, Home: s.home, Secrets: slices.Clone(s.secrets),
		Manifest: Manifest{Version: s.opts.Build.Version, Commit: s.opts.Build.Commit, Profile: s.info.Profile, Lang: s.info.Lang},
	}
}

// clearKeys are the settings shown as they are. Every other key, including one added later, is
// masked; a test fails on a schema key that is in neither list (TM-18).
var clearKeys = []string{
	"update.check", "update.channel",
	"api.timeout", "api.rate_limit", "api.max_retries",
	"download.concurrency", "download.max_attempts", "download.not_found_retries", "download.first_byte_timeout",
	"download.idle_timeout", "download.on_exists", "download.max_file_size", "download.verify", "download.boxart",
	"download.prefer.languages", "download.prefer.formats", "download.prefer.sources", "download.prefer.size",
	"cache.enabled", "cache.max_size_mb", "cache.ttl.search", "cache.ttl.entry", "cache.ttl.meta", "cache.ttl.info", "cache.ttl.covers",
	"interface.language", "interface.color", "interface.confirm_downloads", "interface.page_size",
	"profile.id", "profile.refresh",
	"log.level", "log.file", "log.console", "log.max_size_mb", "log.max_files", "log.session_level", "log.sessions_keep",
	"gui.confirm_button", "gui.text_size", "gui.safe_area",
}

var envName = regexp.MustCompile(`^MOTHDEX_[A-Z0-9_]+$`)

// entry is one file of the zip.
type entry struct {
	name string
	data []byte
}

// bundleParts are what a zip too large gives up, in this order: all but the newest session, oldest
// first; the oldest crash files; then the mothdex.log tail is shortened; the newest session last.
type bundleParts struct {
	fixed    []entry
	crashes  []entry // newest first
	sessions []entry // newest first
	tail     []byte
}

// Bundle writes the zip a tester attaches to an issue: a manifest, a readme, the newest sessions,
// the crash files and the end of mothdex.log, all redacted again and without the person's home
// folder or name (TM-18, TM-19). It opens no network connection and never overwrites a file.
func Bundle(ctx context.Context, o BundleOptions) (BundleResult, error) {
	if o.Sessions == 0 {
		o.Sessions = DefaultBundleSessions
	}
	if o.Sessions < 1 || o.Sessions > MaxBundleSessions {
		return BundleResult{}, ErrSessions
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Environ == nil {
		o.Environ = os.Environ()
	}
	out, err := outDir(o)
	if err != nil {
		return BundleResult{}, err
	}
	parts, err := collect(ctx, o)
	if err != nil {
		return BundleResult{}, err
	}
	if o.limit == 0 {
		o.limit = MaxBundleBytes
	}
	data, sessions, err := pack(ctx, parts, o.limit)
	if err != nil {
		return BundleResult{}, err
	}
	path, err := writeExclusive(out, "mothdex-logs-"+o.Now().UTC().Format("20060102-150405"), data)
	if err != nil {
		return BundleResult{}, err
	}
	if o.OutDir == "" {
		prune(out, bundleFilePattern, keepBundles, filepath.Base(path))
	}
	return BundleResult{Path: path, Bytes: int64(len(data)), Sessions: sessions}, nil
}

// outDir is the folder the zip goes to: the one asked for, which must exist, else Dir/bundles.
func outDir(o BundleOptions) (string, error) {
	if !filepath.IsAbs(o.Dir) {
		return "", errors.New("log: there is no log folder")
	}
	if o.OutDir != "" {
		if info, err := os.Lstat(o.OutDir); err != nil || !info.IsDir() {
			return "", ErrOutDir
		}
		return o.OutDir, nil
	}
	if err := safeDir(o.Dir); err != nil {
		return "", err
	}
	out := filepath.Join(o.Dir, BundlesDir)
	return out, safeDir(out)
}

func collect(ctx context.Context, o BundleOptions) (bundleParts, error) {
	redact := newRedactor(append([]string{defaultAPI}, o.Secrets...)...)
	scrub := newScrubber(o.Home, hostname())
	clean := func(b []byte) []byte { return []byte(scrub.apply(redact.text(string(b)))) }
	var p bundleParts
	manifest, err := manifestJSON(o)
	if err != nil {
		return p, err
	}
	p.fixed = []entry{{"manifest.json", clean(manifest)}, {"README.txt", []byte(readme)}}
	add := func(list *[]entry, sub, name string, limit int64) {
		// a file that vanished or cannot be read is left out: the rest still helps
		if b, err := readTail(filepath.Join(o.Dir, sub, name), limit); err == nil {
			*list = append(*list, entry{sub + "/" + name, clean(b)})
		}
	}
	for _, name := range slices.Backward(matching(filepath.Join(o.Dir, SessionsDir), sessionFilePattern)) {
		if ctx.Err() != nil || len(p.sessions) == o.Sessions {
			break
		}
		if name != o.Skip+".log" {
			add(&p.sessions, SessionsDir, name, bundleSessionBytes)
		}
	}
	crashes := matching(filepath.Join(o.Dir, CrashDir), crashFilePattern)
	for _, name := range slices.Backward(crashes[max(len(crashes)-keepCrashes, 0):]) {
		add(&p.crashes, CrashDir, name, maxCrashBytes)
	}
	if b, err := readTail(filepath.Join(o.Dir, FileName), bundleTailBytes); err == nil {
		p.tail = clean(b)
	}
	return p, ctx.Err()
}

func manifestJSON(o BundleOptions) ([]byte, error) {
	m := o.Manifest
	m.SchemaVersion = 1
	m.OS, m.Arch = runtime.GOOS, runtime.GOARCH
	config := make([]ConfigEntry, len(m.Config))
	for i, c := range m.Config {
		if !slices.Contains(clearKeys, c.Key) {
			c.Value = redacted
		}
		config[i] = c
	}
	m.Config, m.Env = config, []string{}
	for _, kv := range o.Environ {
		if name, _, ok := strings.Cut(kv, "="); ok && envName.MatchString(name) {
			m.Env = append(m.Env, name)
		}
	}
	slices.Sort(m.Env)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // <redacted> stays readable
	enc.SetIndent("", "  ")
	err := enc.Encode(m)
	return buf.Bytes(), err
}

// readTail returns the last limit bytes of a regular file, starting at a whole line.
func readTail(path string, limit int64) ([]byte, error) {
	f, err := openFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	cut := info.Size() > limit
	if cut {
		if _, err := f.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return nil, err
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if cut {
		_, b, _ = bytes.Cut(b, []byte("\n")) // a half line could hold half a secret, unrecognizable
	}
	return b, err
}

// pack zips the parts, giving up content until the result fits in limit.
func pack(ctx context.Context, p bundleParts, limit int) ([]byte, []string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		entries := slices.Concat(p.fixed, p.crashes, p.sessions)
		if len(p.tail) > 0 {
			entries = append(entries, entry{tailName, p.tail})
		}
		data, err := zipBytes(entries)
		if err != nil {
			return nil, nil, err
		}
		if len(data) <= limit {
			ids := []string{}
			for _, e := range p.sessions {
				ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(e.name, SessionsDir+"/"), ".log"))
			}
			return data, ids, nil
		}
		switch {
		case len(p.sessions) > 1:
			p.sessions = p.sessions[:len(p.sessions)-1]
		case len(p.crashes) > 0:
			p.crashes = p.crashes[:len(p.crashes)-1]
		case len(p.tail) > 0:
			p.tail = p.tail[(len(p.tail)+1)/2:]
		case len(p.sessions) > 0:
			p.sessions = nil
		default:
			return nil, nil, errors.New("log: the bundle does not fit its size limit")
		}
	}
}

func zipBytes(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := z.Create(e.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeExclusive writes data to a temporary file and moves it to base.zip, or base-2.zip and so on,
// claiming the name with O_EXCL first so that nothing is ever overwritten.
func writeExclusive(dir, base string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".mothdex-logs-*.tmp") // mode 0600
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone already after the rename
	_, werr := tmp.Write(data)
	if err := errors.Join(werr, tmp.Sync(), tmp.Close()); err != nil {
		return "", err
	}
	claim, name, err := createNew(dir, base, ".zip")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".zip")
	if err := claim.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", errors.Join(fmt.Errorf("cannot write the bundle: %w", err), os.Remove(path))
	}
	return path, nil
}

const readme = `Mothdex diagnostic bundle
=========================

1. Review this zip before sharing it: a GitHub issue is public. It holds your search terms and
   file names. Tokens, the API address and your home folder were removed.
2. Open https://github.com/Or4cu1o/mothdex-cli/issues/new?template=bug.yml
3. Paste the ref shown with the error (six characters), say what you were doing, the version
   (mothdex --version) and your system, and attach this zip.

Contents: manifest.json (version, system, settings), sessions/ (one log per run), crash/ (stack
traces, only after a crash), mothdex.log.tail (the end of the general log).
`
