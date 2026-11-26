package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

type upgradeData struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	Available       bool   `json:"available"`
	Downgrade       bool   `json:"downgrade"`
	Updated         bool   `json:"updated"`
	Channel         string `json:"channel"`
	Signature       string `json:"signature"`
	RestartRequired bool   `json:"restart_required"`
	Notes           string `json:"notes"`
}

func (r *runtime) upgradeCmd() *cobra.Command {
	var check bool
	var version, channel string
	c := &cobra.Command{Use: "upgrade", Aliases: []string{"update"}, Short: "Update a managed installation", Long: "Verify and update all installed Mothdex components. A specific version permits an explicitly confirmed downgrade.", Example: "mdex upgrade\nmdex upgrade --check\nmdex upgrade --channel beta\nmdex upgrade --version 0.2.0 -f", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if check && version != "" {
			return usageErr("upgrade", "--check and --version cannot be combined")
		}
		if !check && !r.g.Force && (r.g.JSON || !r.canPrompt()) {
			return &app.Error{Code: app.CodeConfirmationNeeded, Message: "upgrade needs confirmation or -f"}
		}
		return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
			u := s.Updater
			if channel != "" {
				if s.NewUpdater == nil {
					return outcome{}, usageErr("upgrade", "update channel is unavailable")
				}
				var err error
				u, err = s.NewUpdater(channel)
				if err != nil {
					return outcome{}, err
				}
			}
			if u == nil {
				return outcome{}, &app.Error{Code: app.CodeUpdateUnavailable, Message: "updater unavailable", Args: map[string]any{"reason": "dev_build", "install_command": update.InstallCommand()}}
			}
			data := upgradeData{Channel: channel}
			if data.Channel == "" {
				data.Channel = s.UpdateChannel
			}
			if check {
				result, err := u.Check(ctx, true)
				if err != nil {
					return outcome{}, err
				}
				data.Current = result.Current
				data.Latest = result.Latest
				data.Available = result.Available
			} else {
				result, err := u.Apply(ctx, update.ApplyOptions{Version: version, AllowDowngrade: version != "", Confirm: func(st update.Staged) bool {
					p := r.printer()
					kind := "update.confirm"
					if st.Downgrade {
						kind = "update.confirm_downgrade"
					}
					message := clean(p.T(kind, i18n.Args{"current": st.From, "latest": st.To, "signature": signatureLabel(st.Signature)}))
					data.Downgrade = st.Downgrade
					if st.Downgrade {
						p.warns = append(p.warns, message)
						if !r.g.JSON {
							_, _ = fmt.Fprintln(p.err, message)
						}
					}
					if r.g.Force {
						return true
					}
					if !st.Downgrade {
						_, _ = fmt.Fprintln(p.err, message)
					}
					_, _ = fmt.Fprintln(p.err, st.Notes)
					answer, err := r.readUpdateAnswer(ctx, p.T("update.prompt", nil)+" ")
					return err == nil && p.isYes(strings.TrimSpace(answer))
				}})
				if err != nil {
					return outcome{}, err
				}
				data.Current = result.From
				data.Latest = result.To
				data.Available = result.From != result.To
				data.Updated = result.RestartRequired
				data.Signature = result.Signature
				data.RestartRequired = result.RestartRequired
				data.Notes = result.Notes
			}
			return outcome{data: data, human: func(p *printer) {
				key := "update.current"
				if data.Available {
					key = "update.available"
				}
				if data.Updated {
					key = "update.updated"
				}
				_, _ = fmt.Fprintln(p.out, p.T(key, i18n.Args{"current": data.Current, "latest": data.Latest, "signature": signatureLabel(data.Signature)}))
				if data.RestartRequired {
					p.info("%s", p.T("update.restart", nil))
				}
			}}, nil
		})
	}}
	c.Flags().BoolVar(&check, "check", false, "check for a newer version without downloading archives")
	c.Flags().StringVar(&version, "version", "", "install this exact version (allows downgrade)")
	c.Flags().StringVar(&channel, "channel", "", "release channel: stable | beta")
	return c
}

func signatureLabel(s string) string {
	if s == "none" {
		return "SHA-256 only"
	}
	return s
}

// Only a standalone command may abandon its stdin reader: it exits after cancellation.
// The Shell owns a shared editor and must finish its synchronous prompt before reading again.
func (r *runtime) readUpdateAnswer(ctx context.Context, label string) (string, error) {
	if r.run != nil {
		return r.readAnswer(label)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type answer struct {
		line string
		err  error
	}
	ch := make(chan answer, 1)
	go func() { line, err := r.readAnswer(label); ch <- answer{line, err} }()
	select {
	case a := <-ch:
		return a.line, a.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
