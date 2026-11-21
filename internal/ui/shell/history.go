package shell

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	historyFile = "shell_history"
	historyMax  = 1000 // lines kept
)

// secretKeySuffixes end the config keys whose values may be secrets: tokens and keys, and URLs,
// which can carry credentials (config.md, api.base_url).
var secretKeySuffixes = []string{"token", "key", "url"}

// userinfo matches `scheme://user:pass@host`: credentials in an address, whatever the key.
var userinfo = regexp.MustCompile(`://[^/\s@]*@`)

// recordable reports whether a line may go into the history (shell.md section 5): not when it
// starts with a space, not a `config set` of a key that may hold a secret, and not a `config set`
// whose value has credentials in an address.
func recordable(line string) bool {
	if strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}
	f := strings.Fields(line)
	if len(f) >= 3 && f[0] == "config" && f[1] == "set" {
		key := strings.ToLower(f[2])
		for _, suffix := range secretKeySuffixes {
			if strings.HasSuffix(key, suffix) {
				return false
			}
		}
		return !userinfo.MatchString(line)
	}
	return true
}

// record adds a line to the history: consecutive duplicates collapse and only the last
// historyMax lines stay. It returns a new slice.
func record(lines []string, line string) []string {
	if repeats(lines, line) {
		return lines
	}
	out := append(append(make([]string, 0, len(lines)+1), lines...), line)
	if len(out) > historyMax {
		out = out[len(out)-historyMax:]
	}
	return out
}

// repeats reports whether line is the one the history ends with.
func repeats(lines []string, line string) bool {
	return len(lines) > 0 && lines[len(lines)-1] == line
}

// historyStore is the history file of the Shell, `<paths.data>/shell_history`: one command per
// line. It never fails the Shell: a problem is logged at warn and the history stays in memory.
// Two Shells open at once do not merge: the last one to exit writes the file and the other's new
// lines are lost (last writer wins; review PR-53 s2, accepted).
type historyStore struct {
	path string // "" keeps the history in memory only
	log  *slog.Logger
}

func newHistoryStore(dataDir string, log *slog.Logger) historyStore {
	if dataDir == "" {
		return historyStore{log: log}
	}
	return historyStore{path: filepath.Join(dataDir, historyFile), log: log}
}

func (h historyStore) warn(msg string, err error) {
	if h.log != nil {
		h.log.Warn(msg, "path", h.path, "err", err)
	}
}

// load reads the file, applying the same rules as record, so an edited or older file is tidied.
func (h historyStore) load() []string {
	if h.path == "" {
		return nil
	}
	f, err := os.Open(h.path)
	if err != nil {
		if !os.IsNotExist(err) {
			h.warn("cannot read the shell history", err)
		}
		return nil
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if line := sc.Text(); recordable(line) {
			lines = record(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		h.warn("cannot read the shell history", err)
	}
	return lines
}

// save writes the lines atomically (a temporary file in the same folder, then a rename) with mode
// 0600.
func (h historyStore) save(lines []string) {
	if h.path == "" {
		return
	}
	if err := writeAtomic(h.path, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		h.warn("cannot write the shell history", err)
	}
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".shell_history-*") // created with mode 0600
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
