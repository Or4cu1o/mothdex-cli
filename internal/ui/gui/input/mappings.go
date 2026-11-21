package input

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// SDLPlatform is the name SDL_GameControllerDB uses for an operating system, as in `platform:Linux`.
func SDLPlatform(goos string) string {
	switch goos {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "darwin":
		return "Mac OS X"
	}
	return goos
}

// MappingLines reads a game controller database and returns the lines worth giving to SDL: comments and
// blank lines are dropped, and so are lines for another platform, which SDL would only ignore after
// parsing them (docs/contracts/cli/gui.md section 4). A line that is not a mapping (no 32-digit GUID, no
// name, no mapping) is returned in skipped, so the caller can log it at warn. A line with no `platform:`
// field is kept: SDL treats it as valid everywhere.
func MappingLines(db, platform string) (keep, skipped []string) {
	for _, line := range strings.Split(db, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 3 || !isGUID(fields[0]) {
			skipped = append(skipped, line)
			continue
		}
		if p, ok := platformOf(fields[2:]); ok && p != platform {
			continue
		}
		keep = append(keep, line)
	}
	return keep, skipped
}

func platformOf(fields []string) (string, bool) {
	for _, f := range fields {
		if p, ok := strings.CutPrefix(strings.TrimSpace(f), "platform:"); ok {
			return p, true
		}
	}
	return "", false
}

// isGUID reports whether s is the 32 hexadecimal digits of an SDL joystick GUID.
func isGUID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !isHexDigit(r) {
			return false
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

// Environment variables SDL itself reads for controller mappings.
const (
	envMappings    = "SDL_GAMECONTROLLERCONFIG"      // mapping lines, one per line
	envMappingFile = "SDL_GAMECONTROLLERCONFIG_FILE" // a database file
)

// LoadMappings returns the controller mapping lines to give SDL, in the order of docs/contracts/cli/gui.md
// section 4, later lines replacing earlier ones for the same pad: the embedded database, the user's
// `<paths.data>/gamecontrollerdb.txt`, the file named by SDL_GAMECONTROLLERCONFIG_FILE, and the lines of
// SDL_GAMECONTROLLERCONFIG, which wins (a frontend that sets it for its Ports keeps its mapping). SDL applies
// those two variables itself, but at SDL_Init, before these lines are added, so lines added afterwards would
// replace them: they are read here and added last. Lines for another platform are dropped; a malformed line,
// and an unreadable file other than a missing one, are reported to warn and skipped.
func LoadMappings(embedded, userFile, platform string, getenv func(string) string, warn func(msg string)) []string {
	lines, bad := MappingLines(embedded, platform)
	for _, l := range bad {
		warn("controller database: skipped a malformed line: " + truncateForLog(l))
	}
	add := func(source, text string) {
		more, bad := MappingLines(text, platform)
		for _, l := range bad {
			warn(source + ": skipped a malformed line: " + truncateForLog(l))
		}
		lines = append(lines, more...)
	}
	for _, f := range []struct{ source, path string }{
		{"gamecontrollerdb.txt", userFile},
		{envMappingFile, getenv(envMappingFile)},
	} {
		if f.path == "" {
			continue
		}
		data, err := os.ReadFile(f.path) //nolint:gosec // the user's own file, or the one the frontend names
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			warn("controller database: cannot read " + f.path + ": " + err.Error())
		default:
			add(f.source, string(data))
		}
	}
	if env := getenv(envMappings); env != "" {
		add(envMappings, env)
	}
	return lines
}

func truncateForLog(s string) string {
	const limit = 60
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
