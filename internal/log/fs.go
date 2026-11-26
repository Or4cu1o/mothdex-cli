package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Names under paths.logs (docs/contracts/cli/logs.md section 1).
const (
	LatestName  = "latest.log"
	lockName    = "latest.lock"
	SessionsDir = "sessions"
	CrashDir    = "crash"
	BundlesDir  = "bundles"
)

// Retention of the files this package creates.
const (
	keepCrashes = 10
	keepBundles = 5
)

const sessionIDExpr = `\d{8}-\d{6}-(?:cli|gui)-\d+(?:-\d+)?`

var (
	sessionIDPattern   = regexp.MustCompile(`^` + sessionIDExpr + `$`)
	sessionFilePattern = regexp.MustCompile(`^` + sessionIDExpr + `\.log$`)
	crashFilePattern   = regexp.MustCompile(`^` + sessionIDExpr + `\.txt$`)
	bundleFilePattern  = regexp.MustCompile(`^mothdex-logs-\d{8}-\d{6}(?:-\d+)?\.zip$`)
)

var errUnsafeDir = errors.New("the log folder is not safe to write to")

// safeDir creates dir when missing and refuses one another user could use to redirect or read the
// logs: a link, a folder of another owner, or one others can write to without the sticky bit
// (TM-20).
func safeDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is a link or not a folder", errUnsafeDir, dir)
	}
	return checkOwner(dir, info)
}

// matching lists the regular files of dir whose names match pattern, oldest name first. Links,
// folders and other names are not listed, so nothing else can ever be read or deleted (TM-22).
// A dir that is a link is not entered.
func matching(dir string, pattern *regexp.Regexp) []string {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir) // sorted by name, and the names start with the time
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && pattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return sortByStem(names)
}

// sortByStem orders by the name without its extension, so "x-2.log" comes after "x.log".
func sortByStem(names []string) []string {
	stem := func(s string) string { return strings.TrimSuffix(s, filepath.Ext(s)) }
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && stem(names[j]) < stem(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// prune removes all but the newest keep matching files, never the one named own. Best effort.
func prune(dir string, pattern *regexp.Regexp, keep int, own string) {
	names := matching(dir, pattern)
	excess := len(names) - keep
	for _, n := range names {
		if excess <= 0 {
			return
		}
		if n != own {
			_ = os.Remove(filepath.Join(dir, n))
			excess--
		}
	}
}

// createNew creates a file that did not exist, as base+ext or base-2+ext and so on, and returns it
// with the name used (without the extension).
func createNew(dir, base, ext string) (*os.File, string, error) {
	const attempts = 100
	name := base
	for i := 2; i <= attempts; i++ {
		f, err := openFile(filepath.Join(dir, name+ext), os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return nil, "", fmt.Errorf("no free name for %s%s in %s", base, ext, dir)
}
