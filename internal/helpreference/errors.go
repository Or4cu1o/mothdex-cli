// Package helpreference renders reference material shared by command help and documentation.
package helpreference

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// Errors renders the error reference from the source-of-truth locale keys.
func Errors(b *i18n.Bundle, lang string) string {
	var codes []string
	for key := range b.Catalog(i18n.Fallback) {
		if code, ok := strings.CutSuffix(strings.TrimPrefix(key, "errors."), ".message"); ok && strings.HasPrefix(key, "errors.") {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	var out strings.Builder
	out.WriteString("# Error reference\n\n<!-- Generated from errors.<code> locale keys. Run go run ./scripts/gen-errors. -->\n\n| Code | Meaning | How to fix |\n| --- | --- | --- |\n")
	t := b.New(lang)
	escape := func(s string) string {
		return strings.NewReplacer("|", "\\|", "\n", " ", "{", "[", "}", "]").Replace(s)
	}
	for _, code := range codes {
		fmt.Fprintf(&out, "| `%s` | %s | %s |\n", code, escape(t.T("errors."+code+".message", nil)), escape(referenceHint(t, code)))
	}
	return out.String()
}

func referenceHint(t *i18n.Translator, code string) string {
	hint := t.T("errors."+code+".hint", nil)
	if hint == "" {
		return "No action required; run the command again to retry."
	}
	return hint
}
