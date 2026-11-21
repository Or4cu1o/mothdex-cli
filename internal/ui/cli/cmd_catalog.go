package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// jsonEntry is a search result in --json: the API v1 Entry fields plus the session index.
type jsonEntry struct {
	Index int `json:"index"`
	mothdex.Entry
}

type searchFlags struct {
	platforms, regions []string
	serial, sort       string
	page, pageSize     int
	export, format     string
}

func (r *runtime) searchCmd() *cobra.Command {
	var f searchFlags
	c := &cobra.Command{
		Use:   "search [text...]",
		Short: "Search the catalog",
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				return r.runSearch(ctx, s, strings.Join(args, " "), f)
			})
		},
	}
	fl := c.Flags()
	fl.StringSliceVarP(&f.platforms, "platform", "p", nil, "platform id (repeatable)")
	fl.StringSliceVarP(&f.regions, "region", "r", nil, "region id (repeatable)")
	fl.StringVar(&f.serial, "serial", "", "exact serial")
	fl.IntVar(&f.page, "page", 1, "page number")
	fl.IntVar(&f.pageSize, "page-size", 0, "results per page (1..100; default interface.page_size)")
	fl.StringVar(&f.sort, "sort", "relevance", "relevance | title | size (current page only)")
	fl.StringVarP(&f.export, "export", "e", "", "write the results to a file or directory")
	fl.StringVar(&f.format, "format", "", "export format: txt | json | csv")
	return c
}

func (r *runtime) runSearch(ctx context.Context, s *Session, text string, f searchFlags) (outcome, error) {
	if f.sort != "relevance" && f.sort != "title" && f.sort != "size" {
		return outcome{}, usageErr("search", "--sort takes relevance, title or size")
	}
	if f.page < 1 {
		return outcome{}, usageErr("search", "--page starts at 1")
	}
	q := app.SearchQuery{Text: text, Platforms: splitList(f.platforms), Regions: splitList(f.regions), Serial: f.serial,
		Page: f.page, PageSize: f.pageSize, Sort: app.SortKey(f.sort), Refresh: r.g.Refresh}
	page, err := s.Svc.Search(ctx, q)
	if err != nil {
		return outcome{}, err
	}
	newSearch := r.notePage(page)
	pageSize := f.pageSize
	if pageSize < 1 {
		pageSize = s.PageSize
	}
	results := make([]jsonEntry, len(page.Results))
	for i, e := range page.Results {
		results[i] = jsonEntry{Index: e.Index, Entry: e.Entry}
	}
	data := map[string]any{
		"query":          map[string]any{"text": text, "platforms": nonNil(q.Platforms), "regions": nonNil(q.Regions), "serial": q.Serial, "sort": f.sort},
		"page":           page.Page,
		"page_size":      pageSize,
		"total_pages":    page.TotalPages,
		"total_results":  page.TotalResults,
		"past_last_page": page.PastLastPage,
		"results":        results,
	}
	out := outcome{data: data, meta: &page.Meta}
	out.human = func(p *printer) {
		r.searchHuman(p, page, text, pageSize, f.page)
		if newSearch {
			p.info("%s", p.dim(p.T("ui.shell.indexes_now", nil)))
		}
	}
	if f.export != "" {
		if err := r.exportSearch(f, page, results, data, &page.Meta); err != nil {
			return outcome{}, err
		}
	}
	return out, nil
}

// notePage remembers the page for the Shell (n, p, last, the prompt) and reports whether it starts a
// different search from the one shown before: the old indexes stop being valid (shell.md section 3).
func (r *runtime) notePage(page app.SearchPage) (newSearch bool) {
	if r.run == nil {
		return false
	}
	prev, had := r.run.LastPage()
	r.run.setPage(page)
	return had && !sameSearch(prev.Query, page.Query)
}

// sameSearch compares what is searched; paging and sorting do not change it.
func sameSearch(a, b app.SearchQuery) bool {
	return a.Text == b.Text && a.Serial == b.Serial && slices.Equal(a.Platforms, b.Platforms) && slices.Equal(a.Regions, b.Regions)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func bestLink(e mothdex.Entry) mothdex.Link {
	for _, l := range e.Links {
		if l.Type == "Game" {
			return l
		}
	}
	if len(e.Links) > 0 {
		return e.Links[0]
	}
	return mothdex.Link{}
}

func (p *printer) searchTable(page app.SearchPage) {
	cols := []column{
		{head: "#", right: true},
		{head: p.T("ui.labels.title", nil), title: true},
		{head: p.T("ui.labels.platform", nil)},
		{head: p.T("ui.labels.region", nil), drop: narrowWidth},
		{head: p.T("ui.labels.links", nil), right: true, drop: narrowWidth},
		{head: p.T("ui.labels.size", nil), right: true, drop: narrowWidth},
	}
	rows := make([][]string, len(page.Results))
	for i, e := range page.Results {
		rows[i] = []string{strconv.Itoa(e.Index), e.Title, e.Platform, strings.Join(e.Regions, ","), strconv.Itoa(len(e.Links)), bestLink(e.Entry).SizeStr}
	}
	if p.tty && p.width < minWidth+1 {
		cols = cols[:2]
		for i := range rows {
			rows[i] = rows[i][:2]
		}
	}
	p.table(cols, rows)
}

func (r *runtime) searchHuman(p *printer, page app.SearchPage, text string, pageSize, asked int) {
	if page.PastLastPage {
		p.warning(p.T("ui.cli.search.past_last", i18n.Args{"asked": asked, "shown": page.Page}))
	}
	if len(page.Results) == 0 {
		p.info("%s", clean(p.T("ui.cli.search.none", i18n.Args{"text": text})))
		return
	}
	p.searchTable(page)
	footKey := "ui.cli.search.footer"
	if r.run != nil {
		footKey = "ui.shell.search.footer"
	}
	foot := p.T(footKey, i18n.Args{"page": page.Page, "pages": page.TotalPages, "total": page.TotalResults, "size": pageSize})
	if page.Meta.Cached {
		foot += " · " + p.T("ui.cli.search.cached", i18n.Args{"age": humanAge(time.Since(page.Meta.FetchedAt))})
	}
	if r.run != nil { // inside the Shell the next steps are its own shortcuts, on the same line
		for _, key := range shellNav(page) {
			foot += " · " + p.T(key, nil)
		}
	}
	p.info("%s", p.dim(foot))
	if page.Meta.Stale {
		p.info("%s", p.warn(p.T("ui.cli.search.stale", nil)))
	}
	if r.run != nil {
		return
	}
	hint := p.T("ui.cli.search.hint", i18n.Args{"text": text, "next": page.Page + 1})
	if page.Page >= page.TotalPages {
		hint = p.T("ui.cli.search.hint_last", nil)
	}
	p.info("%s", p.dim(clean(hint)))
}

// shellNav are the i18n keys of the shortcuts the footer offers inside the Shell: none for a single
// page (shell.md section 2), otherwise paging as far as it goes, then info and download.
func shellNav(page app.SearchPage) []string {
	if page.TotalPages <= 1 {
		return nil
	}
	var keys []string
	if page.Page < page.TotalPages {
		keys = append(keys, "ui.shell.search.next")
	}
	if page.Page > 1 {
		keys = append(keys, "ui.shell.search.prev")
	}
	return append(keys, "ui.shell.search.info", "ui.shell.search.download")
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// exportSearch writes the results to -e (cli-output.md section 5): json is the envelope, csv has
// the fixed columns with the formula guard, txt is the plain table at 120 columns.
func (r *runtime) exportSearch(f searchFlags, page app.SearchPage, results []jsonEntry, data any, m *app.ResultMeta) error {
	target, format, err := app.ExportTarget(f.export, f.format, "search", time.Now())
	if err != nil {
		return err
	}
	p := r.printer()
	render := func(w io.Writer) error {
		switch format {
		case app.FormatJSON:
			sub := *p
			sub.out, sub.meta = w, m
			return sub.writeJSON("search", data, nil)
		case app.FormatCSV:
			return writeSearchCSV(w, results)
		}
		sub := *p
		sub.out, sub.tty, sub.width, sub.color = w, true, 120, false
		sub.searchTable(page)
		return nil
	}
	if err := app.WriteExport(target, r.g.Force, render); err != nil {
		return err
	}
	p.info("%s", p.dim(clean(p.T("ui.cli.export.saved", i18n.Args{"path": target}))))
	return nil
}

func writeSearchCSV(w io.Writer, results []jsonEntry) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"index", "slug", "title", "platform", "regions", "serial", "links", "best_link_filename", "best_link_size"})
	for _, e := range results {
		b := bestLink(e.Entry)
		_ = cw.Write([]string{strconv.Itoa(e.Index), app.CSVCell(e.Slug), app.CSVCell(e.Title), app.CSVCell(e.Platform),
			app.CSVCell(strings.Join(e.Regions, ";")), app.CSVCell(e.Serial), strconv.Itoa(len(e.Links)),
			app.CSVCell(b.Filename), strconv.FormatInt(b.Size, 10)})
	}
	cw.Flush()
	return cw.Error()
}

// ---- info ----

func (r *runtime) infoCmd() *cobra.Command {
	var slug bool
	var prefer []string
	c := &cobra.Command{
		Use:   "info <#|slug>",
		Short: "Show one entry and its links",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				ref, err := parseRef("info", args[0], slug)
				if err != nil {
					return outcome{}, err
				}
				pref, set, err := parsePrefer("info", prefer, s.Prefer)
				if err != nil {
					return outcome{}, err
				}
				res, err := s.Svc.Entry(ctx, ref)
				if err != nil {
					return outcome{}, err
				}
				if r.run != nil {
					r.run.setEntry(res.Value)
				}
				var choices []app.LinkChoice
				if set || preferActive(s.Prefer) {
					if choices, err = s.Svc.Links(ctx, app.Ref{Slug: res.Value.Slug}, app.LinkSelector{Prefer: pref}); err != nil {
						return outcome{}, err
					}
				}
				return outcome{data: map[string]any{"entry": res.Value}, meta: &res.Meta,
					human: func(p *printer) { r.infoHuman(p, res.Value, choices) }}, nil
			})
		},
	}
	c.Flags().BoolVar(&slug, "slug", false, "read the argument as a slug")
	c.Flags().StringArrayVar(&prefer, "prefer", nil, "rank links: key=value[,value...] (repeatable)")
	return c
}

func (r *runtime) infoHuman(p *printer, e mothdex.Entry, choices []app.LinkChoice) {
	out := func(s string) { _, _ = fmt.Fprintln(p.out, s) }
	out(p.bold(clean(e.Title)))
	kv := func(k, v string) {
		if v == "" {
			v = "-"
		}
		out(fmt.Sprintf("  %s  %s", p.accent(padTo(p.T("ui.cli.info."+k, nil), 8, false)), clean(v)))
	}
	kv("slug", e.Slug)
	kv("platform", e.Platform)
	kv("regions", strings.Join(e.Regions, ","))
	kv("serial", e.Serial)
	out(p.T("ui.cli.info.links", nil))
	winner, why := 0, map[int]string{}
	if len(choices) > 0 {
		for _, c := range choices {
			if c.Available {
				winner = c.Index
				break
			}
		}
		for _, c := range choices {
			why[c.Index] = strings.Join(c.Reasons, ", ")
		}
	}
	rows := make([][]string, len(e.Links))
	for i, l := range e.Links {
		mark := ""
		if l.Type != "" && winner == i+1 {
			mark = "*"
		}
		row := []string{strconv.Itoa(i + 1), l.Type, l.Format, l.SizeStr, strings.Join(l.Extras, ","), l.SourceName}
		if len(choices) > 0 {
			row = append(row, mark, why[i+1])
		}
		rows[i] = row
	}
	cols := []column{{head: "#", right: true}, {head: p.T("ui.cli.info.type", nil)}, {head: p.T("ui.cli.info.format", nil)}, {head: p.T("ui.labels.size", nil), right: true}, {head: p.T("ui.cli.info.extras", nil)}, {head: p.T("ui.cli.info.source", nil)}}
	if len(choices) > 0 {
		cols = append(cols, column{head: ""}, column{head: p.T("ui.cli.info.why", nil)})
	}
	p.table(cols, rows)
}

// ---- random ----

func (r *runtime) randomCmd() *cobra.Command {
	var platforms, regions []string
	var serial string
	var count int
	c := &cobra.Command{
		Use:   "random",
		Short: "Show random entries",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				if count < 1 || count > 20 {
					return outcome{}, usageErr("random", "--count takes 1 to 20")
				}
				res, err := s.Svc.Random(ctx, app.RandomQuery{Filters: mothdex.Filters{Platforms: splitList(platforms), Regions: splitList(regions), Serial: serial}, Count: count})
				if err != nil {
					return outcome{}, err
				}
				return outcome{data: map[string]any{"entries": res.Value}, meta: &res.Meta, human: func(p *printer) {
					rows := make([][]string, len(res.Value))
					for i, e := range res.Value {
						rows[i] = []string{e.Slug, e.Title, e.Platform, strings.Join(e.Regions, ","), strconv.Itoa(len(e.Links))}
					}
					p.table([]column{{head: "Slug"}, {head: p.T("ui.labels.title", nil), title: true}, {head: p.T("ui.labels.platform", nil)}, {head: p.T("ui.labels.region", nil), drop: narrowWidth}, {head: p.T("ui.labels.links", nil), right: true, drop: narrowWidth}}, rows)
				}}, nil
			})
		},
	}
	fl := c.Flags()
	fl.StringSliceVarP(&platforms, "platform", "p", nil, "platform id (repeatable)")
	fl.StringSliceVarP(&regions, "region", "r", nil, "region id (repeatable)")
	fl.StringVar(&serial, "serial", "", "exact serial")
	fl.IntVarP(&count, "count", "n", 1, "how many entries (1..20)")
	return c
}

// ---- platforms, regions, db-info ----

func (r *runtime) platformsCmd() *cobra.Command {
	var brand, class string
	c := &cobra.Command{
		Use:   "platforms",
		Short: "List platforms",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				res, err := s.Svc.Platforms(ctx, app.PlatformFilter{Brand: brand, Class: class})
				if err != nil {
					return outcome{}, err
				}
				list := res.Value
				sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
				return outcome{data: map[string]any{"platforms": list}, meta: &res.Meta, human: func(p *printer) {
					rows := make([][]string, len(list))
					for i, x := range list {
						rows[i] = []string{x.ID, x.Name, x.Brand}
					}
					p.table([]column{{head: "id"}, {head: p.T("ui.cli.name", nil)}, {head: p.T("ui.cli.brand", nil), drop: narrowWidth}}, rows)
				}}, nil
			})
		},
	}
	c.Flags().StringVar(&brand, "brand", "", "only this brand")
	c.Flags().StringVar(&class, "class", "", "only this class")
	return c
}

func (r *runtime) regionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "regions",
		Short: "List regions",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				res, err := s.Svc.Regions(ctx)
				if err != nil {
					return outcome{}, err
				}
				list := res.Value
				sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
				return outcome{data: map[string]any{"regions": list}, meta: &res.Meta, human: func(p *printer) {
					rows := make([][]string, len(list))
					for i, x := range list {
						rows[i] = []string{x.ID, x.Name}
					}
					p.table([]column{{head: "id"}, {head: p.T("ui.cli.name", nil)}}, rows)
				}}, nil
			})
		},
	}
}

func (r *runtime) dbInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "db-info",
		Short: "Show catalog statistics",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				res, err := s.Svc.DBInfo(ctx)
				if err != nil {
					return outcome{}, err
				}
				return outcome{data: map[string]any{"total_entries": res.Value.TotalEntries}, meta: &res.Meta, human: func(p *printer) {
					_, _ = fmt.Fprintln(p.out, p.T("ui.cli.db_info", i18n.Args{"count": res.Value.TotalEntries}))
				}}, nil
			})
		},
	}
}
