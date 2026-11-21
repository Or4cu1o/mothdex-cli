package proxy

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Sanitize limits (downloader.md section 6.1).
const (
	maxNameBytes = 200
	maxExtBytes  = 16
)

// Name errors. ErrUnsafeName maps to Failed(unsafe_name).
var (
	ErrUnsafeName = errors.New("proxy: file name has an executable extension")
	ErrEmptyName  = errors.New("proxy: file name is empty after sanitizing and no fallback was given")
	errInvariant  = errors.New("proxy: sanitized name violates an invariant")
)

var formatRe = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

// reserved holds the Windows device names (upper case), enforced on every GOOS because ROM
// drives on Linux are often exFAT or NTFS (T-16 F10).
var reserved = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true}
	for _, d := range "0123456789" {
		m["COM"+string(d)] = true
		m["LPT"+string(d)] = true
	}
	for _, d := range "¹²³" {
		m["COM"+string(d)] = true
		m["LPT"+string(d)] = true
	}
	return m
}()

// executableExt is the deny-list of last extensions (lower case). bin is a ROM extension and
// is deliberately absent.
var executableExt = func() map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Fields(`sh bash zsh csh ksh fish command run desktop appimage py pl rb jar
		exe com scr pif bat cmd ps1 psm1 psd1 vbs vbe js jse wsf wsh hta msi msp msc cpl dll lnk url reg scf
		inf library-ms settingcontent-ms appref-ms`) {
		m[e] = true
	}
	return m
}()

type nameOpts struct {
	slug, format, jobID string
	allowExec           bool
}

// NameOption adjusts Sanitize.
type NameOption func(*nameOpts)

// WithFallback supplies the values used when nothing usable is left of the name: slug and
// format come from the API and are sanitized too; jobID is the last resort.
func WithFallback(slug, format, jobID string) NameOption {
	return func(o *nameOpts) { o.slug, o.format, o.jobID = slug, format, jobID }
}

// AllowExecutable lifts the executable deny-list (the --allow-executable flag, nothing else).
func AllowExecutable() NameOption { return func(o *nameOpts) { o.allowExec = true } }

// Sanitize turns an untrusted file name into a single safe path component. It is pure and has
// the same rules on every GOOS (downloader.md 6.1, T-16 section 5). It returns ErrUnsafeName
// for an executable extension unless AllowExecutable is given, and ErrEmptyName when nothing
// is left and no fallback was provided.
func Sanitize(raw string, opts ...NameOption) (string, error) {
	var o nameOpts
	for _, f := range opts {
		f(&o)
	}
	name := cleanName(raw)
	if isBlank(name) {
		name = cleanName(fallbackName(o))
		if isBlank(name) {
			if o.jobID == "" {
				return "", ErrEmptyName
			}
			name = cleanName("download-" + o.jobID)
		}
	}
	if !o.allowExec && executableExt[strings.ToLower(extOf(name))] {
		return "", ErrUnsafeName
	}
	if err := checkName(name); err != nil {
		return "", err
	}
	return name, nil
}

func fallbackName(o nameOpts) string {
	stem := cleanName(o.slug)
	if isBlank(stem) {
		return ""
	}
	if formatRe.MatchString(o.format) {
		return stem + "." + o.format
	}
	return stem
}

// isBlank reports a name with nothing but dots, underscores and spaces.
func isBlank(s string) bool { return strings.Trim(s, "._ ") == "" }

// cleanName applies steps 1 to 6 of the specification.
func cleanName(raw string) string {
	s := lastComponent(raw)
	s = dropUnsafeRunes(s)
	s = replaceForbidden(s)
	for range 8 { // 4, 5 and 6 feed each other; they settle in two or three rounds
		next := capLength(prefixReserved(trimEdges(s)))
		if next == s {
			break
		}
		s = next
	}
	return s
}

// lastComponent is step 1: the last non-empty element after splitting on / and \.
func lastComponent(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// dropUnsafeRunes is step 2: invalid UTF-8 and invisible or format runes go, look-alikes of
// the separators and the colon become underscores, and the result is NFC.
func dropUnsafeRunes(s string) string {
	s = norm.NFC.String(strings.ToValidUTF8(s, ""))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs):
			continue
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case strings.ContainsAny(norm.NFKC.String(string(r)), `/\:`):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return norm.NFC.String(b.String()) // dropping a rune may expose a composable pair
}

// replaceForbidden is step 3, including the collapse of runs of '_' and of spaces.
func replaceForbidden(s string) string {
	var b strings.Builder
	var last rune
	for _, r := range s {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 || r == 0x7f {
			r = '_'
		}
		if (r == '_' || r == ' ') && r == last {
			continue
		}
		b.WriteRune(r)
		last = r
	}
	return b.String()
}

// trimEdges is step 4.
func trimEdges(s string) string {
	for {
		t := strings.TrimRight(strings.TrimLeft(s, ". -"), ". ")
		if t == s {
			return s
		}
		s = t
	}
}

// prefixReserved is step 5.
func prefixReserved(s string) string {
	stem, _, _ := strings.Cut(s, ".")
	if reserved[strings.ToUpper(strings.TrimRight(stem, " "))] {
		return "_" + s
	}
	return s
}

// capLength is step 6: at most 200 bytes, the stem cut on a rune boundary, the extension kept
// when it is short.
func capLength(s string) string {
	if len(s) <= maxNameBytes {
		return s
	}
	ext := ""
	if i := strings.LastIndexByte(s, '.'); i > 0 && len(s)-i <= maxExtBytes {
		ext = s[i:]
	}
	stem := s[:len(s)-len(ext)]
	cut := maxNameBytes - len(ext)
	for cut > 0 && !utf8.RuneStart(stem[cut]) {
		cut--
	}
	return stem[:cut] + ext
}

// extOf returns the text after the last dot, or "".
func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// checkName is step 9: the invariants, as an error at run time.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > maxNameBytes || !utf8.ValidString(name) ||
		name != filepath.Base(name) || !filepath.IsLocal(name) ||
		strings.ContainsAny(name, `<>:"/\|?*`) || strings.ContainsFunc(name, func(r rune) bool {
		return r < 0x20 || r == 0x7f || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
	}) {
		return errInvariant
	}
	return nil
}
