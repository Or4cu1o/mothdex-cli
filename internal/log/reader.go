package log

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ShowContext is how many lines `logs show --ref` prints on each side of the error.
const ShowContext = 10

const maxLineBytes = 64 << 10 // a longer line is cut when read back

// ErrSession is returned for a session id that is not the name of a file in sessions/.
var ErrSession = errors.New("log: no such session")

// SessionSummary is one row of `logs list`.
type SessionSummary struct {
	ID         string `json:"id"`
	Binary     string `json:"binary"`
	Mode       string `json:"mode"`
	StartedAt  string `json:"started_at"`
	DurationMS int64  `json:"duration_ms"`
	Errors     int    `json:"errors"`
	Current    bool   `json:"current"`
}

// Match is one occurrence of a ref in a session, with the lines around it.
type Match struct {
	Session string   `json:"session"`
	Ref     string   `json:"ref"`
	Line    int      `json:"line"` // 1-based
	Context []string `json:"context"`
}

// List returns the sessions in dir, newest first. current marks the running one. A session with no
// "session end" line (still running, or crashed) lasts until its last line.
func List(dir, current string) []SessionSummary {
	names := matching(filepath.Join(dir, SessionsDir), sessionFilePattern)
	out := make([]SessionSummary, 0, len(names))
	for _, name := range slices.Backward(names) {
		id := strings.TrimSuffix(name, ".log")
		out = append(out, summarize(dir, id, id == current))
	}
	return out
}

func summarize(dir, id string, current bool) SessionSummary {
	s := SessionSummary{ID: id, Binary: strings.Split(id, "-")[2], Current: current}
	var last string
	ended := false
	_ = eachLine(sessionFile(dir, id), func(line string) { // an unreadable file lists with what is known
		attrs := parseLine(line)
		if attrs["time"] != "" {
			last = attrs["time"]
		}
		switch attrs["msg"] {
		case msgStart:
			s.StartedAt = attrs["started_at"]
		case msgInfo:
			s.Mode = attrs["mode"]
		case msgEnd:
			s.DurationMS, _ = strconv.ParseInt(attrs["duration_ms"], 10, 64)
			ended = true
		}
		if attrs["level"] == "ERROR" {
			s.Errors++
		}
	})
	if !ended {
		from, e1 := time.Parse(time.RFC3339, s.StartedAt)
		to, e2 := time.Parse(time.RFC3339Nano, last)
		if e1 == nil && e2 == nil && to.After(from) {
			s.DurationMS = to.Sub(from).Milliseconds()
		}
	}
	return s
}

// Last is the newest session that is not current; ok is false when there is none.
func Last(dir, current string) (string, bool) {
	for _, name := range slices.Backward(matching(filepath.Join(dir, SessionsDir), sessionFilePattern)) {
		if id := strings.TrimSuffix(name, ".log"); id != current {
			return id, true
		}
	}
	return "", false
}

// Lines reads a session, safe to show: redacted again, because the runtime writes crash traces
// to the file as they are and older logs were never redacted, and without control characters.
// secrets are the configured values to hide besides the default API. tail > 0 keeps only the last
// tail lines. id must name a file in sessions/ (TM-24).
func Lines(dir, id string, tail int, secrets ...string) ([]string, error) {
	if !sessionIDPattern.MatchString(id) {
		return nil, ErrSession
	}
	redact := newRedactor(append([]string{defaultAPI}, secrets...)...)
	out := []string{}
	err := eachLine(sessionFile(dir, id), func(line string) {
		out = append(out, redact.text(printable(line)))
		if tail > 0 && len(out) > tail {
			out = out[1:]
		}
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrSession
	}
	return out, err
}

// Find returns every error line carrying ref, in one session or, with id empty, in all of them,
// newest first.
func Find(dir, id, ref string, secrets ...string) ([]Match, error) {
	ids := []string{id}
	if id == "" {
		ids = ids[:0]
		for _, name := range slices.Backward(matching(filepath.Join(dir, SessionsDir), sessionFilePattern)) {
			ids = append(ids, strings.TrimSuffix(name, ".log"))
		}
	}
	out := []Match{}
	for _, session := range ids {
		lines, err := Lines(dir, session, 0, secrets...)
		if err != nil {
			return nil, err
		}
		for i, line := range lines {
			if attrs := parseLine(line); attrs["level"] == "ERROR" && attrs["ref"] == ref {
				around := lines[max(i-ShowContext, 0):min(i+ShowContext+1, len(lines))]
				out = append(out, Match{Session: session, Ref: ref, Line: i + 1, Context: slices.Clone(around)})
			}
		}
	}
	return out, nil
}

// Removed counts what Clear deleted.
type Removed struct {
	Sessions int `json:"sessions"`
	Crashes  int `json:"crashes"`
	Bundles  int `json:"bundles"`
}

// Clear deletes the sessions except current, the crash files and the bundles: only regular files
// with the exact names this package creates (TM-22).
func Clear(dir, current string) (Removed, error) {
	var r Removed
	var errs []error
	for _, group := range []struct {
		sub     string
		pattern *regexp.Regexp
		n       *int
	}{
		{SessionsDir, sessionFilePattern, &r.Sessions},
		{CrashDir, crashFilePattern, &r.Crashes},
		{BundlesDir, bundleFilePattern, &r.Bundles},
	} {
		for _, name := range matching(filepath.Join(dir, group.sub), group.pattern) {
			if group.sub == SessionsDir && name == current+".log" {
				continue
			}
			if err := os.Remove(filepath.Join(dir, group.sub, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			*group.n++
		}
	}
	return r, errors.Join(errs...)
}

func sessionFile(dir, id string) string { return filepath.Join(dir, SessionsDir, id+".log") }

// eachLine calls fn for every line of a regular file, cutting oversized ones.
func eachLine(path string, fn func(string)) error {
	f, err := openFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(io.LimitReader(f, 2*maxSessionBytes), maxLineBytes)
	cut := false
	for {
		b, more, err := r.ReadLine()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if !cut {
			fn(string(b))
		}
		cut = more
	}
}

// printable strips control and format characters: a crash trace or an old log may hold escape
// sequences (TM-25).
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s)
}

// parseLine reads the key=value pairs of a line written by slog's text handler. A line that does
// not start with "time=" is not a record.
func parseLine(line string) map[string]string {
	out := map[string]string{}
	if !strings.HasPrefix(line, "time=") {
		return out
	}
	for line != "" {
		key, rest, ok := strings.Cut(line, "=")
		if !ok {
			break
		}
		var val string
		if q, err := strconv.QuotedPrefix(rest); err == nil && strings.HasPrefix(rest, `"`) {
			val, _ = strconv.Unquote(q)
			rest = rest[len(q):]
		} else {
			val, rest, _ = strings.Cut(rest, " ")
		}
		if _, seen := out[key]; !seen {
			out[key] = val
		}
		line = strings.TrimLeft(rest, " ")
	}
	return out
}
