package shell

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// maxIndexes caps the entries one range or one line can name (ui.md section 4).
const maxIndexes = 100

// keyError is a user mistake with a translatable message.
type keyError struct {
	key  string
	args i18n.Args
}

func (e *keyError) Error() string { return e.key }

// splitLine splits a command line into words. Spaces separate words; single or double quotes group
// them and are removed. A backslash is a normal character, so Windows paths work unquoted.
func splitLine(line string) ([]string, error) {
	var (
		words  []string
		cur    strings.Builder
		quote  rune
		inWord bool
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, &keyError{key: "ui.shell.bad_quote"}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

var rangeWord = regexp.MustCompile(`^(\d+)-(\d+)$`)

// expandIndexes turns the ranges among the arguments of info, download and boxart into the
// numbers they name: `1 3 5` stays, `2-4` becomes `2 3 4`. Words that are not ranges pass through
// untouched (numbers, slugs, flags), and so does the value of a flag that takes one, so `-o 2020-21`
// is a folder. takesValue says whether a flag word such as `-o` or `--link` consumes the next word.
func expandIndexes(args []string, takesValue func(flag string) bool) ([]string, error) {
	out := make([]string, 0, len(args))
	named := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			out = append(out, a)
			if !strings.Contains(a, "=") && takesValue != nil && takesValue(a) && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		m := rangeWord.FindStringSubmatch(a)
		if m == nil {
			out = append(out, a)
			named++
			continue
		}
		lo, loErr := strconv.Atoi(m[1])
		hi, hiErr := strconv.Atoi(m[2])
		if loErr != nil || hiErr != nil || lo < 1 || lo > hi { // an overflowing number is no range
			return nil, &keyError{key: "ui.shell.bad_range", args: i18n.Args{"range": a}}
		}
		// Compare before adding: hi-lo cannot overflow (lo >= 1), and named never passes the cap.
		if hi-lo >= maxIndexes-named {
			return nil, &keyError{key: "ui.shell.range_too_big", args: i18n.Args{"max": maxIndexes}}
		}
		named += hi - lo + 1
		for i := 0; i <= hi-lo; i++ { // counting from lo, not up to hi: hi may be the largest int
			out = append(out, strconv.Itoa(lo+i))
		}
	}
	return out, nil
}

// isNumber reports whether a word is only digits: a bare number is the shortcut for info.
func isNumber(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
