//go:build windows

package proxy_test

import (
	"testing"
	"time"
)

// ageLink is a no-op on Windows: a symlink's own mtime cannot be set portably; the junction
// and reparse-point cases are covered by the realfs tests.
func ageLink(t *testing.T, _ string, _ time.Time) {
	t.Log("ageLink: the old-symlink branch of case 39 is not exercised on Windows")
}
