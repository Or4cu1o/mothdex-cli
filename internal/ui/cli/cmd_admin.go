package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// ---- history ----

func (r *runtime) historyCmd() *cobra.Command {
	var failed bool
	var limit int
	var export, format string
	c := &cobra.Command{
		Use:   "history",
		Short: "Show past downloads",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				recs, err := s.Svc.History(ctx, app.HistoryFilter{Failed: failed, Limit: limit})
				if err != nil {
					return outcome{}, err
				}
				if recs == nil {
					recs = []app.HistoryRecord{}
				}
				if export != "" {
					all := recs
					if limit > 0 { // an export is the whole history, not the page shown
						if all, err = s.Svc.History(ctx, app.HistoryFilter{Failed: failed}); err != nil {
							return outcome{}, err
						}
					}
					if err := r.exportHistory(export, format, all); err != nil {
						return outcome{}, err
					}
				}
				return outcome{data: map[string]any{"records": recs}, human: func(p *printer) { r.historyHuman(p, recs) }}, nil
			})
		},
	}
	fl := c.Flags()
	fl.BoolVar(&failed, "failed", false, "only failed downloads")
	fl.IntVar(&limit, "limit", 50, "newest records to show (0 = all)")
	fl.StringVarP(&export, "export", "e", "", "write the records to a file or directory")
	fl.StringVar(&format, "format", "", "export format: txt | json | csv")
	return c
}

func (r *runtime) exportHistory(path, format string, recs []app.HistoryRecord) error {
	target, fm, err := app.ExportTarget(path, format, "history", time.Now())
	if err != nil {
		return err
	}
	version := ""
	if r.d.Version != nil {
		version = r.d.Version().Version
	}
	err = app.WriteExport(target, r.g.Force, func(w io.Writer) error {
		return app.ExportHistory(w, fm, recs, app.HistoryExportMeta{CLIVersion: version, FetchedAt: time.Now()})
	})
	if err != nil {
		return err
	}
	p := r.printer()
	p.info("%s", p.dim(clean(p.T("ui.cli.export.saved", i18n.Args{"path": target}))))
	return nil
}

func (r *runtime) historyHuman(p *printer, recs []app.HistoryRecord) {
	rows := make([][]string, len(recs))
	for i, x := range recs {
		state := x.State
		if x.Error != nil {
			state = "failed:" + x.Error.Code
		}
		rows[i] = []string{x.StartedAt.Local().Format("2006-01-02 15:04"), state, x.Title, x.Platform, humanSize(x.Bytes), humanDuration(time.Duration(x.DurationMS) * time.Millisecond), strconv.Itoa(x.Attempts)}
	}
	p.table([]column{{head: p.T("ui.cli.history.date", nil)}, {head: p.T("ui.cli.history.state", nil)}, {head: p.T("ui.labels.title", nil), title: true}, {head: p.T("ui.labels.platform", nil), drop: narrowWidth}, {head: p.T("ui.labels.size", nil), right: true, drop: narrowWidth}, {head: p.T("ui.cli.history.time", nil), right: true, drop: narrowWidth}, {head: p.T("ui.cli.history.tries", nil), right: true, drop: narrowWidth}}, rows)
}

// ---- cache ----

func (r *runtime) cacheCmd() *cobra.Command {
	c := &cobra.Command{Use: "cache", Short: "Inspect or clear the cache"}
	c.AddCommand(&cobra.Command{
		Use: "stats", Short: "Show cache usage", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				st, err := s.Svc.CacheStats()
				if err != nil {
					return outcome{}, err
				}
				ns := make([]map[string]any, len(st.Namespaces))
				rows := make([][]string, len(st.Namespaces))
				for i, n := range st.Namespaces {
					ns[i] = map[string]any{"name": n.Name, "items": n.Items, "bytes": n.Bytes, "expired": n.Expired}
					rows[i] = []string{n.Name, strconv.Itoa(n.Items), humanSize(n.Bytes), strconv.Itoa(n.Expired)}
				}
				return outcome{data: map[string]any{"namespaces": ns, "total_bytes": st.TotalBytes, "max_bytes": st.MaxBytes}, human: func(p *printer) {
					p.table([]column{{head: p.T("ui.cli.cache.namespace", nil)}, {head: p.T("ui.cli.cache.items", nil), right: true}, {head: p.T("ui.labels.size", nil), right: true}, {head: p.T("ui.cli.cache.expired", nil), right: true}}, rows)
					p.info("%s", p.dim(p.T("ui.cli.cache.total", i18n.Args{"used": humanSize(st.TotalBytes), "max": humanSize(st.MaxBytes)})))
				}}, nil
			})
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "clear [namespace...]", Short: "Clear cached data (default: everything except the session and state)",
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				if err := s.Svc.CacheClear(args...); err != nil {
					return outcome{}, err
				}
				if args == nil {
					args = []string{}
				}
				return outcome{data: map[string]any{"cleared": args}, human: func(p *printer) {
					p.info("%s", p.T("ui.cli.cache.cleared", nil))
				}}, nil
			})
		},
	})
	return c
}

// ---- config ----

func settingJSON(s app.Setting) map[string]any {
	return map[string]any{"key": s.Key, "value": s.Value, "source": string(s.Source)}
}

func settingText(v any) string {
	if l, ok := v.([]string); ok {
		return strings.Join(l, ",")
	}
	return fmt.Sprint(v)
}

func (r *runtime) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Read or change settings"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "List every setting", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				list, err := s.Svc.ConfigList()
				if err != nil {
					return outcome{}, err
				}
				return r.settingsOutcome(list), nil
			})
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "get <key>", Short: "Show one setting", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				st, err := s.Svc.ConfigGet(args[0])
				if err != nil {
					return outcome{}, err
				}
				return r.settingsOutcome([]app.Setting{st}), nil
			})
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "set <key> <value>", Short: "Change a setting in the configuration file", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				if err := s.Svc.ConfigSet(args[0], args[1]); err != nil {
					return outcome{}, err
				}
				st, err := s.Svc.ConfigGet(args[0])
				if err != nil {
					return outcome{}, err
				}
				return r.settingsOutcome([]app.Setting{st}), nil
			})
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "reset <key|all>", Short: "Remove a setting from the configuration file", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(_ context.Context, s *Session) (outcome, error) {
				if err := s.Svc.ConfigReset(args[0]); err != nil {
					return outcome{}, err
				}
				list, err := s.Svc.ConfigList()
				if err != nil {
					return outcome{}, err
				}
				return r.settingsOutcome(list), nil
			})
		},
	})
	return c
}

func (r *runtime) settingsOutcome(list []app.Setting) outcome {
	js := make([]map[string]any, len(list))
	for i, s := range list {
		js[i] = settingJSON(s)
	}
	return outcome{data: map[string]any{"settings": js}, human: func(p *printer) {
		if len(list) == 1 {
			_, _ = fmt.Fprintln(p.out, clean(settingText(list[0].Value)))
			return
		}
		rows := make([][]string, len(list))
		for i, s := range list {
			rows[i] = []string{s.Key, settingText(s.Value), string(s.Source)}
		}
		p.table([]column{{head: p.T("ui.cli.config.key", nil)}, {head: p.T("ui.cli.config.value", nil)}, {head: p.T("ui.cli.config.source", nil)}}, rows)
	}}
}

// ---- profile ----

func (r *runtime) profileCmd() *cobra.Command {
	c := &cobra.Command{Use: "profile", Short: "Show or detect the frontend profile"}
	show := func(detect bool) func(*cobra.Command, []string) error {
		return func(*cobra.Command, []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				info := s.Svc.ActiveProfile()
				if detect {
					var err error
					if info, err = s.Svc.DetectProfile(ctx); err != nil {
						return outcome{}, err
					}
				}
				warns := nonNil(info.Warnings)
				data := map[string]any{
					"profile":    map[string]any{"id": info.ID, "name": info.Name, "host": info.Host, "source": info.Source, "rom_root": info.RomRoot, "verified": info.Verified, "warnings": warns},
					"unverified": nonNil(info.Unverified),
				}
				return outcome{data: data, human: func(p *printer) {
					kv := func(k, v string) {
						if v == "" {
							v = "-"
						}
						_, _ = fmt.Fprintf(p.out, "%s  %s\n", p.accent(padTo(p.T("ui.cli.profile."+k, nil), 9, false)), clean(v))
					}
					kv("id", info.ID)
					kv("name", info.Name)
					kv("host", info.Host)
					kv("source", info.Source)
					kv("rom_root", info.RomRoot)
					kv("verified", strconv.FormatBool(info.Verified))
					for _, w := range info.Warnings {
						p.warning(w)
					}
					for _, u := range info.Unverified {
						p.info("%s", p.dim(clean(p.T("ui.cli.profile.unverified", i18n.Args{"item": u}))))
					}
				}}, nil
			})
		}
	}
	c.AddCommand(&cobra.Command{Use: "show", Short: "Show the active profile", Args: cobra.NoArgs, RunE: show(false)})
	c.AddCommand(&cobra.Command{Use: "detect", Short: "Detect the profile again", Args: cobra.NoArgs, RunE: show(true)})
	return c
}

// ---- version and the interfaces that are not built yet ----

func (r *runtime) versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print the version", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return r.runVersion() }}
}

func (r *runtime) runVersion() error {
	if r.cmd == "" || r.cmd == "mothdex" {
		r.cmd = "version"
	}
	return r.local(func(_ *printer) (any, func(*printer), error) {
		info := buildinfo.Info{}
		if r.d.Version != nil {
			info = r.d.Version()
		}
		return info, func(p *printer) { _, _ = fmt.Fprintln(p.out, info.String()) }, nil
	})
}

func (r *runtime) soonCmd(name string) *cobra.Command {
	return &cobra.Command{Use: name, Short: "Not available yet", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return r.soon(name) }}
}

// soon is the answer of a command that is not built yet.
func (r *runtime) soon(name string) error {
	return r.local(func(_ *printer) (any, func(*printer), error) {
		return map[string]any{"available": false}, func(p *printer) {
			_, _ = fmt.Fprintln(p.out, p.T("ui.cli.soon", i18n.Args{"name": name}))
		}, nil
	})
}
