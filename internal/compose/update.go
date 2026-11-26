package compose

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

// NewUpdater supplies the same process identity and release options to every interface.
func NewUpdater(cfg config.Update, dataDir string, log *slog.Logger) (*update.Updater, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, err
	}
	return update.New(update.Options{Exe: exe, Current: buildinfo.Get().Version, Channel: cfg.Channel, DataDir: dataDir, Log: log})
}
