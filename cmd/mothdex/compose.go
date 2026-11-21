package main

import (
	"context"
	"os"

	"github.com/Or4cu1o/mothdex-cli/internal/compose"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
)

// open builds the service through the shared composition root and wraps it for the command line.
func open(ctx context.Context, g cli.Globals) (*cli.Session, error) {
	s, err := compose.Open(ctx, compose.Options{
		ConfigFile: g.Config, Profile: g.Profile, Lang: g.Lang, Jobs: g.Jobs,
		Force: g.Force, AllowExecutable: g.AllowExecutable, NoColor: g.NoColor,
		Downloads: g.Downloads, Session: g.Shell, Repair: g.Command == "config",
	})
	if err != nil {
		return nil, err
	}
	return &cli.Session{
		Svc: s.Svc, T: s.T, Prefer: s.Prefer, Boxart: s.Boxart, Confirm: s.Confirm, PageSize: s.PageSize, Color: s.Color,
		Warnings: s.Warnings, LogPath: s.LogPath, Close: s.Close,
		DataDir: s.DataDir, NoColor: s.NoColor, UTF8: cli.IsUTF8(os.Getenv), Log: s.Log,
	}, nil
}
