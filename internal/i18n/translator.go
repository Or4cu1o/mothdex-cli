package i18n

import (
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Args are the named placeholder values of one message.
type Args map[string]any

// Translator answers messages for one locale, falling back to en_us per key and finally to the
// key itself, so a missing translation is visible but never fatal.
type Translator struct {
	id     string
	tag    language.Tag
	own    Catalog
	source Catalog
}

// New returns a Translator for the locale id (xx_yy); an unknown id behaves as en_us.
func (b *Bundle) New(id string) *Translator {
	if b.locales[id] == nil {
		id = Fallback
	}
	return &Translator{id: id, tag: language.Make(strings.ReplaceAll(id, "_", "-")), own: b.locales[id], source: b.locales[Fallback]}
}

// Locale is the locale id in use.
func (t *Translator) Locale() string { return t.id }

// Has reports whether key resolves in this locale or in en_us.
func (t *Translator) Has(key string) bool { _, ok := t.lookup(key); return ok }

func (t *Translator) lookup(key string) (string, bool) {
	if v, ok := t.own[key]; ok {
		return v, true
	}
	v, ok := t.source[key]
	return v, ok
}

// T returns the message for key with its {name} placeholders replaced. Unknown keys return the
// key; a placeholder without an argument stays as written.
func (t *Translator) T(key string, args Args) string {
	v, ok := t.lookup(key)
	if !ok {
		return key
	}
	return format(v, args)
}

// N returns the plural form of key for count: key.zero, key.one, key.two, key.few, key.many or
// key.other by the CLDR rules of the locale, with key.other as the last resort. {count} is
// filled in unless args set it.
func (t *Translator) N(key string, count int, args Args) string {
	merged := Args{"count": count}
	for k, v := range args {
		merged[k] = v
	}
	forms := map[plural.Form]string{plural.Zero: "zero", plural.One: "one", plural.Two: "two", plural.Few: "few", plural.Many: "many", plural.Other: "other"}
	if v, ok := t.lookup(key + "." + forms[plural.Cardinal.MatchPlural(t.tag, absInt(count), 0, 0, 0, 0)]); ok {
		return format(v, merged)
	}
	if v, ok := t.lookup(key + ".other"); ok {
		return format(v, merged)
	}
	return key
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// format replaces {name} with the matching argument. Arguments are stripped of control and
// format characters. A single pass, so a value that contains "{other}" is never expanded again.
func format(s string, args Args) string {
	if !strings.Contains(s, "{") {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:open])
		s = s[open:]
		end := strings.IndexByte(s, '}')
		if end < 0 {
			b.WriteString(s)
			break
		}
		name := s[1:end]
		if v, ok := args[name]; ok && keyPattern.MatchString(name) {
			b.WriteString(stripControls(toString(v)))
		} else {
			b.WriteString(s[:end+1])
		}
		s = s[end+1:]
	}
	return b.String()
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(sprint(v))
}
