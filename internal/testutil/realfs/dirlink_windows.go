//go:build windows

package realfs

import (
	"os/exec"
	"syscall"
	"testing"
)

// DirLink makes link a directory junction to target (mklink /J), the case that
// EvalSymlinks does not resolve (T-16 F7, case 17). Junctions need no
// privilege, so this does not skip. The command line is built by hand with
// quoted paths because cmd.exe drops quotes when Go quotes each argument,
// which breaks paths that contain spaces.
func DirLink(t testing.TB, target, link string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `cmd /c "mklink /J "` + link + `" "` + target + `""`, // cmd strips the outer quotes
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("realfs: mklink /J: %v: %s", err, out)
	}
}
