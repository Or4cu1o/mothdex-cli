package i18n

import (
	"fmt"
	"slices"
	"strings"
)

func sprint(v any) string { return fmt.Sprint(v) }

// Normalize turns a POSIX or BCP 47 locale name (pt_BR.UTF-8, pt-BR, en_US@euro) into the
// lower-case xx_yy form of the locale files. It returns "" for "C", "POSIX" and unusable input.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.IndexAny(name, ".@"); i >= 0 {
		name = name[:i]
	}
	name = strings.ReplaceAll(name, "-", "_")
	if name == "" || name == "c" || name == "posix" {
		return ""
	}
	lang, region, _ := strings.Cut(name, "_")
	if len(lang) < 2 || len(lang) > 3 {
		return ""
	}
	if len(region) > 2 { // script or variant (zh_hans_cn): keep the last two-letter part
		parts := strings.Split(name, "_")
		region = ""
		if last := parts[len(parts)-1]; len(last) == 2 {
			region = last
		}
	}
	if region == "" {
		return lang
	}
	return lang + "_" + region
}

// Detect returns the locale name the environment asks for. On every OS the POSIX variables win
// in the order LC_ALL, LC_MESSAGES, LANG; on Windows, when none is set, system supplies the user
// preferred UI language (nil system means none).
func Detect(env func(string) string, system func() []string) string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if n := Normalize(env(v)); n != "" {
			return n
		}
	}
	if system != nil {
		for _, name := range system() {
			if n := Normalize(name); n != "" {
				return n
			}
		}
	}
	return ""
}

// Resolve picks the locale to use from the setting (interface.language: "auto" or xx_yy,
// case-insensitive) and the loaded locales: an exact match, else the first locale of the same
// language (pt gives pt_br, zh_tw gives zh_cn), else en_us. For "auto" the detected name is used.
func (b *Bundle) Resolve(setting string, env func(string) string, system func() []string) string {
	want := Normalize(setting)
	if strings.EqualFold(strings.TrimSpace(setting), "auto") || strings.TrimSpace(setting) == "" {
		want = Detect(env, system)
	}
	if want == "" {
		return Fallback
	}
	if b.locales[want] != nil {
		return want
	}
	lang, _, _ := strings.Cut(want, "_")
	if own := lang + "_" + lang; b.locales[own] != nil {
		return own
	}
	ids := b.Locales()
	if i := slices.IndexFunc(ids, func(id string) bool { return strings.HasPrefix(id, lang+"_") }); i >= 0 {
		return ids[i]
	}
	return Fallback
}
