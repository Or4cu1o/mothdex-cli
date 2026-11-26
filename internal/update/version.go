package update

import (
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func validVersion(v string) bool {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	for _, p := range strings.Split(m[4], ".") {
		if numeric(p) && len(p) > 1 && p[0] == '0' {
			return false
		}
	}
	return true
}
func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func numberCompare(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
func compare(a, b string) int {
	am, bm := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		if c := numberCompare(am[i], bm[i]); c != 0 {
			return c
		}
	}
	if am[4] == bm[4] {
		return 0
	}
	if am[4] == "" {
		return 1
	}
	if bm[4] == "" {
		return -1
	}
	ap, bp := strings.Split(am[4], "."), strings.Split(bm[4], ".")
	for i := 0; i < min(len(ap), len(bp)); i++ {
		x, y := ap[i], bp[i]
		c := strings.Compare(x, y)
		if numeric(x) && numeric(y) {
			c = numberCompare(x, y)
		} else if numeric(x) {
			c = -1
		} else if numeric(y) {
			c = 1
		}
		if c != 0 {
			return c
		}
	}
	if len(ap) < len(bp) {
		return -1
	}
	return 1
}
