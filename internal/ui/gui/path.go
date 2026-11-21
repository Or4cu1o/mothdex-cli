package gui

import "strings"

// shortenPath makes a folder path fit in maxW pixels by dropping folders from the middle, never the first
// ones that fit and never the last: /home/someone/Games/retro/collection becomes /home/someone/…/collection
// (docs/ux/gui.md section 1.3: no string is cut except titles, so the folder stays readable). Only when even
// "/…/last" is too wide is the last folder itself cut with an ellipsis.
func shortenPath(p painter, size int, path string, maxW int) string {
	if p.width(size, path) <= maxW {
		return path
	}
	sep := "/"
	if strings.Contains(path, `\`) && !strings.Contains(path, "/") {
		sep = `\`
	}
	parts := strings.Split(path, sep)
	last := parts[len(parts)-1]
	if last == "" && len(parts) > 1 { // a trailing separator
		last = parts[len(parts)-2]
		parts = parts[:len(parts)-1]
	}
	for keep := len(parts) - 2; keep >= 1; keep-- {
		candidate := strings.Join(parts[:keep], sep) + sep + "…" + sep + last
		if p.width(size, candidate) <= maxW {
			return candidate
		}
	}
	head := sep + "…" + sep
	if len(parts) > 0 && parts[0] != "" { // a relative path or a drive such as C:
		head = parts[0] + sep + "…" + sep
	}
	return head + p.fit(size, last, max(maxW-p.width(size, head), p.width(size, "…")))
}
