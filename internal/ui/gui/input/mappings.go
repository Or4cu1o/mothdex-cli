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

// LoadMappings returns the controller mapping lines to give SDL, in the order of docs/contracts/cli/gui.md
// section 4: the embedded database first, then the user's file, so a later line for the same pad replaces an
// earlier one. SDL itself applies SDL_GAMECONTROLLERCONFIG after these. Lines for another platform are
// dropped; a malformed line, and an unreadable user file other than a missing one, are reported to warn and
// skipped.
func LoadMappings(embedded, userFile, platform string, warn func(msg string)) []string {
	lines, bad := MappingLines(embedded, platform)
	for _, l := range bad {
		warn("controller database: skipped a malformed line: " + truncateForLog(l))
	}
	if userFile == "" {
		return lines
	}
	data, err := os.ReadFile(userFile) //nolint:gosec // the user's own file under paths.data
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return lines
	case err != nil:
		warn("controller database: cannot read " + userFile + ": " + err.Error())
		return lines
	}
	user, bad := MappingLines(string(data), platform)
	for _, l := range bad {
		warn("gamecontrollerdb.txt: skipped a malformed line: " + truncateForLog(l))
	}
	return append(lines, user...)
}

func truncateForLog(s string) string {
	const limit = 60
	if r := []rune(s); len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
