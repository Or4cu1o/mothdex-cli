//go:build windows

package realfs

import (
	"os/exec"
	"testing"
)

// DirLink makes link a directory junction to target (mklink /J), the case that
// EvalSymlinks does not resolve (T-16 F7, case 17). Junctions need no
// privilege, so this does not skip.
func DirLink(t testing.TB, target, link string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("realfs: mklink /J: %v: %s", err, out)
	}
}
