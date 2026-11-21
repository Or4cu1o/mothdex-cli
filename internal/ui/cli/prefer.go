package cli

import (
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// parsePrefer reads the repeatable --prefer key=value[,value...] flag (docs/ux/prefer-syntax.md).
// It returns the baseline with every key given on the command line replacing that key's list.
// set is false when no --prefer was given. Unknown keys and malformed values are usage errors.
func parsePrefer(command string, flags []string, base app.Preference) (pref app.Preference, set bool, err error) {
	pref = base
	if len(flags) == 0 {
		return pref, false, nil
	}
	seen := map[string][]string{}
	for _, raw := range flags {
		key, val, ok := strings.Cut(raw, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		var values []string
		for _, v := range strings.Split(val, ",") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		if !ok || key == "" || len(values) == 0 {
			return base, false, usageErr(command, "--prefer needs key=value, for example lang=En,Pt")
		}
		switch key {
		case "lang", "format", "source", "size":
			seen[key] = append(seen[key], values...)
		default:
			return base, false, usageErr(command, "unknown --prefer key %q. Valid keys: lang, format, source, size", key)
		}
	}
	if v, ok := seen["lang"]; ok {
		pref.Languages = v
	}
	if v, ok := seen["format"]; ok {
		pref.Formats = v
	}
	if v, ok := seen["source"]; ok {
		pref.Sources = v
	}
	if v, ok := seen["size"]; ok {
		if len(v) != 1 || (v[0] != "smallest" && v[0] != "largest") {
			return base, false, usageErr(command, "--prefer size takes exactly one of smallest or largest")
		}
		pref.Size = app.SizePref(v[0])
	}
	return pref, true, nil
}

// preferActive reports whether any preference is in force (shows the Why column and the winner).
func preferActive(p app.Preference) bool {
	return len(p.Languages)+len(p.Formats)+len(p.Sources) > 0 || p.Size != app.SizeNone
}
