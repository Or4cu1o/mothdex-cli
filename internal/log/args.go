package log

import (
	"regexp"
	"strings"
)

// FlagRule says how the session header treats a flag: whether it takes a value, and whether that
// value may be written (the cobra annotation mothdex.log_value). Keys are long names and short
// letters, without dashes.
type FlagRule struct {
	TakesValue bool
	Log        bool
}

var flagName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,40}$`)

// maskArgs is the allowlist of TM-17: flag names are kept, flag values only when their rule allows
// it, the value of `config set` and anything shaped like an address never.
func maskArgs(args []string, rules map[string]FlagRule, scrub *scrubber) []string {
	m := &masker{rules: rules, scrub: scrub}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			m.out = append(m.out, a)
			for _, rest := range args[i+1:] {
				m.positional(rest)
			}
			return m.out
		case strings.HasPrefix(a, "--"):
			i += m.long(a[2:], args[i+1:])
		case len(a) > 1 && a[0] == '-':
			i += m.short(a[1:], args[i+1:])
		default:
			m.positional(a)
		}
	}
	return m.out
}

type masker struct {
	rules map[string]FlagRule
	scrub *scrubber
	words []string // positionals so far
	out   []string
}

func (m *masker) value(v string, allowed bool) string {
	if !allowed || addressLike(v) {
		return redacted
	}
	return m.scrub.apply(v)
}

// long handles "--name[=value]" and returns how many of the following args it consumed.
func (m *masker) long(body string, rest []string) int {
	name, val, hasVal := strings.Cut(body, "=")
	rule, known := m.rules[name]
	if !flagName.MatchString(name) {
		name, rule, known = redacted, FlagRule{}, false
	}
	if hasVal {
		m.out = append(m.out, "--"+name+"="+m.value(val, known && rule.Log))
		return 0
	}
	m.out = append(m.out, "--"+name)
	return m.next(rule, known, rest)
}

// short handles "-q", "-qv", "-ovalue" and "-o=value".
func (m *masker) short(body string, rest []string) int {
	for i := 0; i < len(body); i++ {
		name := body[i : i+1]
		rule, known := m.rules[name]
		if !flagName.MatchString(name) {
			m.out = append(m.out, redacted)
			return 0
		}
		m.out = append(m.out, "-"+name)
		if known && !rule.TakesValue {
			continue
		}
		if tail := strings.TrimPrefix(body[i+1:], "="); tail != "" {
			m.out = append(m.out, m.value(tail, known && rule.Log))
			return 0
		}
		return m.next(rule, known, rest)
	}
	return 0
}

// next consumes the value that follows a flag. An unknown flag is assumed to take one, so that a
// secret is never read as a search term.
func (m *masker) next(rule FlagRule, known bool, rest []string) int {
	if (known && !rule.TakesValue) || len(rest) == 0 || (!known && strings.HasPrefix(rest[0], "-")) {
		return 0
	}
	m.out = append(m.out, m.value(rest[0], known && rule.Log))
	return 1
}

func (m *masker) positional(a string) {
	secret := len(m.words) >= 3 && m.words[0] == "config" && m.words[1] == "set"
	m.words = append(m.words, a)
	m.out = append(m.out, m.value(a, !secret))
}

func addressLike(v string) bool { return strings.Contains(v, "://") || userinfoPattern.MatchString(v) }
