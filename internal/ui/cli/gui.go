package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

func (r *runtime) guiCmd() *cobra.Command {
	return &cobra.Command{Use: "gui", Short: "Open the gamepad interface", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			locate := r.d.Executable
			if locate == nil {
				locate = os.Executable
			}
			exe, err := locate()
			if err != nil {
				return err
			}
			name := "mothdex-gui"
			if goruntime.GOOS == "windows" {
				name += ".exe"
			}
			path := filepath.Join(filepath.Dir(exe), name)
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
				return usageErr("gui", "mothdex-gui is missing; reinstall without -NoGui / --no-gui, or use mothdex tui")
			}
			if err != nil {
				return err
			}
			var args []string
			for _, flag := range []struct{ name, value string }{{"config", r.g.Config}, {"profile", r.g.Profile}, {"lang", r.g.Lang}} {
				if flag.value != "" {
					args = append(args, "--"+flag.name, flag.value)
				}
			}
			child := exec.CommandContext(r.ctx, path, args...) //nolint:gosec // Fixed sibling filename in the installed executable directory; no PATH or shell lookup.
			child.Stdin, child.Stdout, child.Stderr = r.d.Stdin, r.d.Stdout, r.d.Stderr
			if err := child.Run(); err != nil {
				var status *exec.ExitError
				if errors.As(err, &status) {
					return &exitError{err: &app.Error{Code: app.CodeInternal, Message: "GUI exited"}, code: status.ExitCode(), silent: true}
				}
				return err
			}
			return nil
		},
	}
}
