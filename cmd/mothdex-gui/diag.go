//go:build gui

package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// diagnostics is what the start-up log line tells a beta tester's report about the machine, and nothing else
// (docs/contracts/cli/ports.md section 4, T-70 IC-13). Whatever is pasted from it into a public issue must carry no
// user name, no home directory and no path: so no variable is logged with its value except the two that are
// plain words (the video driver, and the locale), and only when they look like words.
type diagnostics struct {
	SDLVersion string
	Driver     string                      // the video driver SDL chose
	Lookup     func(string) (string, bool) // os.LookupEnv: a variable set to the empty string is "set"
	Mappings   input.MappingCounts
	Pads       []input.ControllerType
}

// setOnly are the variables the line names as "set" or "unset", never with a value: a path list or a mapping
// could hold a home directory, and LD_PRELOAD and SDL_DYNAMIC_API change which code runs (IC-13), on every system.
var setOnly = []string{
	"LD_LIBRARY_PATH", "SDL_VIDEODRIVER", "SDL_GAMECONTROLLERCONFIG", "SDL_GAMECONTROLLERCONFIG_FILE",
	"LD_PRELOAD", "SDL_DYNAMIC_API",
}

// wordValue is what may be logged as a value: a driver name or a locale such as pt_BR.UTF-8.
var wordValue = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,40}$`)

// attrs are the key and value pairs of the one "gui: diagnostics" line, for slog.
func (d diagnostics) attrs() []any {
	out := []any{"sdl", d.SDLVersion, "driver", wordOrNot(d.Driver)}
	for _, name := range setOnly {
		_, set := d.Lookup(name)
		out = append(out, name, setOrUnset(set))
	}
	value := func(name string) string { v, _ := d.Lookup(name); return v }
	out = append(out,
		"SDL_VIDEODRIVER_value", wordOrNot(value("SDL_VIDEODRIVER")),
		"LANG", wordOrNot(value("LANG")),
		"LC_ALL", wordOrNot(value("LC_ALL")),
		"mappings_embedded", d.Mappings.Embedded, "mappings_user", d.Mappings.User,
		"mappings_file", d.Mappings.File, "mappings_env", d.Mappings.Env,
		"controller_types", padTypes(d.Pads),
	)
	return out
}

func setOrUnset(set bool) string {
	if !set {
		return "unset"
	}
	return "set"
}

// wordOrNot is the value when it is a plain word, "unset" when it is empty, and a fixed note otherwise, so an odd
// value (a path, a control character) is never copied into the log.
func wordOrNot(v string) string {
	switch {
	case v == "":
		return "unset"
	case wordValue.MatchString(v):
		return v
	}
	return "not shown"
}

// padTypes lists the SDL_GameControllerType numbers of the open pads, "none" when there are none.
func padTypes(types []input.ControllerType) string {
	if len(types) == 0 {
		return "none"
	}
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = strconv.Itoa(int(t))
	}
	return strings.Join(parts, ",")
}
