package log

import (
	"os"
	"regexp"
	"slices"
	"strings"
)

const userMarker = "<user>"

// scrubber removes who the person is from text: the home folder in every spelling, the user name
// as a path component and the host name (TM-18).
type scrubber struct {
	replacements []replacement
}

type replacement struct {
	needle string // lower case; a line without it cannot match
	re     *regexp.Regexp
	with   string
}

// genericHosts are host names that are ordinary words in the logs and identify nobody.
var genericHosts = []string{"localhost", "mothdex", "batocera", "recalbox", "emuelec", "lakka", "retrobat", "linux", "windows"}

var separators = regexp.MustCompile(`[\\/]+`)

func newScrubber(home, host string) *scrubber {
	s := &scrubber{}
	if home != "" {
		s.addHome(home)
	}
	if len(host) >= 3 && !slices.Contains(genericHosts, strings.ToLower(host)) {
		s.replacements = append(s.replacements, replacement{strings.ToLower(host), regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(host) + `\b`), "<host>"})
	}
	return s
}

func (s *scrubber) addHome(home string) {
	windows := strings.Contains(home, `\`) || (len(home) > 1 && home[1] == ':')
	flags, marker := "", "~"
	if windows {
		flags, marker = "(?i)", "%USERPROFILE%"
	}
	var user string
	for i, h := range []string{home, shortHome(home)} {
		parts := separators.Split(strings.TrimRight(h, `\/`), -1)
		if h == "" || len(parts) < 2 || parts[len(parts)-1] == "" {
			continue // "/" or a bare drive is no home to hide
		}
		if i == 0 {
			user = parts[len(parts)-1]
		}
		needle := strings.ToLower(parts[len(parts)-1])
		for j, p := range parts {
			parts[j] = regexp.QuoteMeta(p)
		}
		// any slash form, doubled by escaping or not
		re := regexp.MustCompile(flags + strings.Join(parts, `[\\/]+`) + `($|[^\p{L}\p{N}_-])`)
		s.replacements = append(s.replacements, replacement{needle, re, marker + "${1}"})
	}
	if user != "" {
		re := regexp.MustCompile(flags + `([\\/])` + regexp.QuoteMeta(user) + `($|[\\/\s"':,;)\]])`)
		s.replacements = append(s.replacements, replacement{strings.ToLower(user), re, "${1}" + userMarker + "${2}"})
	}
}

// apply works line by line and runs a pattern only on a line that holds its needle.
func (s *scrubber) apply(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for line := range strings.Lines(text) {
		lower := strings.ToLower(line)
		for _, r := range s.replacements {
			if strings.Contains(lower, r.needle) {
				// twice: neighbouring matches share the character that ends the first one
				line = r.re.ReplaceAllString(r.re.ReplaceAllString(line, r.with), r.with)
			}
		}
		out.WriteString(line)
	}
	return out.String()
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}
