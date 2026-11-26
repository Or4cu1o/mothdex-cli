package log

import (
	"os"
	"runtime"
	"strings"
)

// DisplayPath is a path as it is shown on screen: %LocalAppData% or %USERPROFILE% on Windows, ~
// elsewhere, so a screenshot does not carry the user name.
func DisplayPath(path string) string {
	home, _ := os.UserHomeDir()
	return displayPath(runtime.GOOS, home, os.Getenv("LocalAppData"), path)
}

func displayPath(goos, home, localAppData, path string) string {
	under := func(base, marker string) (string, bool) {
		base = strings.TrimRight(base, `\/`)
		if base == "" || len(path) < len(base) {
			return "", false
		}
		head := path[:len(base)]
		if head != base && (goos != "windows" || !strings.EqualFold(head, base)) {
			return "", false
		}
		rest := path[len(base):]
		if rest != "" && rest[0] != '/' && rest[0] != '\\' {
			return "", false
		}
		return marker + rest, true
	}
	if goos == "windows" {
		if p, ok := under(localAppData, "%LocalAppData%"); ok {
			return p
		}
		if p, ok := under(home, "%USERPROFILE%"); ok {
			return p
		}
		return path
	}
	if p, ok := under(home, "~"); ok {
		return p
	}
	return path
}
