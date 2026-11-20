//go:build !windows

package realfs

import "testing"

// DirLink makes link a directory link to target: a symlink on this platform.
func DirLink(t testing.TB, target, link string) {
	t.Helper()
	Symlink(t, target, link)
}
