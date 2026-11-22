// Package i18n provides translations: locales embedded in the binary plus an optional external
// folder, automatic language detection, named placeholders, plural forms, and a per-key
// fallback to en_us (cli-conventions.md section 9).
//
// Translation files are data, not code: values are plain text with {name} placeholders, nothing
// is executed, control characters are stripped when a file is loaded, and every substituted
// argument is stripped too, so a hostile locale file or API string cannot inject terminal
// escapes.
package i18n

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Fallback is the locale every other one falls back to, per key.
const Fallback = "en_us"

const (
	maxFileBytes = 1 << 20
	maxKeys      = 5000
	maxValueLen  = 2000
	metaKey      = "locale"
)

var (
	idPattern  = regexp.MustCompile(`^[a-z]{2,3}_[a-z]{2}$`)
	keyPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)
)

// Catalog is the set of messages of one locale, flattened to dotted keys.
type Catalog map[string]string

// Bundle holds every loaded locale.
type Bundle struct {
	locales  map[string]Catalog
	warnings []string
}

// Load reads every xx_yy.yaml file of the given file systems, in order: a later file system
// overrides an earlier one key by key, and may add locales (the external folder over the
// embedded files). A bad file is skipped and reported in Warnings; the embedded en_us is
// required.
func Load(sources ...fs.FS) (*Bundle, error) {
	b := &Bundle{locales: map[string]Catalog{}}
	for _, src := range sources {
		if src == nil {
			continue
		}
		entries, err := fs.ReadDir(src, ".")
		if err != nil {
			b.warn("locale folder unreadable: %v", err)
			continue
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".yaml")
			if e.IsDir() || !ok || !idPattern.MatchString(id) {
				continue
			}
			cat, err := readCatalog(src, e.Name())
			if err != nil {
				b.warn("%s skipped: %v", e.Name(), err)
				continue
			}
			if b.locales[id] == nil {
				b.locales[id] = Catalog{}
			}
			for k, v := range cat {
				b.locales[id][k] = v
			}
		}
	}
	if len(b.locales[Fallback]) == 0 {
		return nil, errors.New("i18n: the en_us locale is missing")
	}
	return b, nil
}

// warn records a skipped file. The text can echo a file name or YAML text from the locale folder, so
// it is cleaned like a substituted argument before it can reach a terminal.
func (b *Bundle) warn(format string, args ...any) {
	b.warnings = append(b.warnings, stripArgument(fmt.Sprintf(format, args...)))
}

// readCatalog opens the file once: the type and size checks run on the open handle, and the read
// is capped, so a file replaced or grown between a stat and a read cannot slip past the limit.
func readCatalog(src fs.FS, name string) (Catalog, error) {
	f, err := src.Open(path.Clean(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, errors.New("not a regular file or larger than 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, errors.New("not a regular file or larger than 1 MiB")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	delete(doc, metaKey) // name, authors, version: metadata, not messages
	cat := Catalog{}
	if err := flatten("", doc, cat); err != nil {
		return nil, err
	}
	return cat, nil
}

func flatten(prefix string, node map[string]any, out Catalog) error {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if !keyPattern.MatchString(key) {
			return fmt.Errorf("invalid key %q", key)
		}
		switch t := v.(type) {
		case map[string]any:
			if err := flatten(key, t, out); err != nil {
				return err
			}
		case string:
			if len(t) > maxValueLen {
				return fmt.Errorf("value of %q is too long", key)
			}
			out[key] = stripControls(t)
		case nil:
			out[key] = "" // an explicit empty value, e.g. errors.canceled.hint
		case int, int64, uint64, float64, bool:
			out[key] = fmt.Sprint(t) // an unquoted scalar is text for an author who forgot the quotes
		default:
			return fmt.Errorf("value of %q must be text", key)
		}
		if len(out) > maxKeys {
			return errors.New("too many keys")
		}
	}
	return nil
}

// stripControls drops Cc and Cf runes (ANSI escapes, bidi controls) but keeps newline and tab.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// LoadDir is Load with an optional external folder opened through os.Root, so a symlink inside
// the folder cannot point at a file outside it. A missing folder means no external locales.
// This is the only supported way to load a folder.
func LoadDir(embedded fs.FS, dir string) (*Bundle, error) {
	if dir == "" {
		return Load(embedded)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		b, lerr := Load(embedded)
		if b != nil && !errors.Is(err, fs.ErrNotExist) {
			b.warn("locale folder unreadable: %v", err)
		}
		return b, lerr
	}
	defer func() { _ = root.Close() }()
	return Load(embedded, root.FS())
}

// Locales lists the loaded locale ids, sorted.
func (b *Bundle) Locales() []string {
	ids := make([]string, 0, len(b.locales))
	for id := range b.locales {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Warnings lists the files skipped at load time.
func (b *Bundle) Warnings() []string { return slices.Clone(b.warnings) }

// Catalog returns the messages of a locale (nil if unknown). The map must not be modified.
func (b *Bundle) Catalog(id string) Catalog { return b.locales[id] }

// Missing lists the en_us keys that locale id does not define, sorted: the CI check for
// translations that fall back.
func (b *Bundle) Missing(id string) []string {
	var out []string
	for k := range b.locales[Fallback] {
		if _, ok := b.locales[id][k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Extra lists keys that locale id defines and en_us does not (typos, stale keys), sorted. A
// plural form that en_us lacks (ru few and many) is fine when en_us has the key's .other form.
func (b *Bundle) Extra(id string) []string {
	var out []string
	for k := range b.locales[id] {
		if _, ok := b.locales[Fallback][k]; ok {
			continue
		}
		if base, form, found := cutLast(k); found && isPluralForm(form) {
			if _, ok := b.locales[Fallback][base+".other"]; ok {
				continue
			}
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func cutLast(k string) (base, last string, ok bool) {
	i := strings.LastIndexByte(k, '.')
	if i < 0 {
		return "", k, false
	}
	return k[:i], k[i+1:], true
}

func isPluralForm(s string) bool {
	switch s {
	case "zero", "one", "two", "few", "many", "other":
		return true
	}
	return false
}
