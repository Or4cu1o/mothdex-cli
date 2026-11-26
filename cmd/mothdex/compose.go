package main

import (
	"context"
	"os"

	"github.com/Or4cu1o/mothdex-cli/internal/compose"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

// opener builds the service through the shared composition root and wraps it for the command
// line. logs is nil for a command that has no session log.
func opener(logs *mlog.Session) func(context.Context, cli.Globals) (*cli.Session, error) {
	return func(ctx context.Context, g cli.Globals) (*cli.Session, error) { return open(ctx, g, logs) }
}

func open(ctx context.Context, g cli.Globals, logs *mlog.Session) (*cli.Session, error) {
	if logs != nil && g.Shell {
		logs.Update(mlog.Info{Mode: g.Command}) // shell or tui
	}
	s, err := compose.Open(ctx, compose.Options{
		ConfigFile: g.Config, Profile: g.Profile, Lang: g.Lang, Jobs: g.Jobs,
		Force: g.Force, AllowExecutable: g.AllowExecutable, NoColor: g.NoColor,
		Downloads: g.Downloads, Session: g.Shell, Repair: g.Command == "config", Logs: logs,
	})
	if err != nil {
		return nil, err
	}
	return &cli.Session{
		Updater: s.Updater, UpdateCheck: s.UpdateCheck, UpdateChannel: s.UpdateChannel,
		NewUpdater: func(channel string) (*update.Updater, error) {
			return compose.NewUpdater(config.Update{Check: s.UpdateCheck, Channel: channel}, s.DataDir, s.Log)
		},
		Svc: s.Svc, T: s.T, Prefer: s.Prefer, Boxart: s.Boxart, Confirm: s.Confirm, PageSize: s.PageSize, Color: s.Color,
		Warnings: s.Warnings, LogPath: s.LogPath, Close: s.Close,
		DataDir: s.DataDir, NoColor: s.NoColor, UTF8: cli.IsUTF8(os.Getenv), Log: s.Log, Logs: s.Logs,
	}, nil
}
