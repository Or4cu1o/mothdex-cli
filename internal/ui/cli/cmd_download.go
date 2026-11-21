package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

const confirmBytes = 500 << 20 // confirm_downloads asks above this size or for more than one file

type downloadFlags struct {
	search           string
	link             int
	prefer           []string
	slug             bool
	boxart, noBoxart bool
}

func (r *runtime) downloadCmd() *cobra.Command {
	var f downloadFlags
	c := &cobra.Command{
		Use:   "download [#|slug ...]",
		Short: "Download entries",
		RunE: func(_ *cobra.Command, args []string) error {
			r.g.Downloads = true
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				return r.runDownload(ctx, s, args, f)
			})
		},
	}
	fl := c.Flags()
	fl.StringVar(&f.search, "search", "", "search and download the first result")
	fl.IntVar(&f.link, "link", 0, "link number to use (wins over --prefer)")
	fl.StringArrayVar(&f.prefer, "prefer", nil, "rank links: key=value[,value...] (repeatable)")
	fl.BoolVar(&f.slug, "slug", false, "read the arguments as slugs")
	fl.BoolVar(&f.boxart, "boxart", false, "also save the cover")
	fl.BoolVar(&f.noBoxart, "no-boxart", false, "do not save the cover")
	fl.StringVarP(&r.g.Output, "output", "o", "", "destination folder (becomes the root; files go straight into it)")
	fl.IntVarP(&r.g.Jobs, "jobs", "j", 0, "downloads at the same time")
	fl.BoolVar(&r.g.AllowExecutable, "allow-executable", false, "allow file types that can run code")
	return c
}

// refsFor resolves the command line to entry references. With --search the first hit is used, by
// slug: the session is a convenience and must not decide what is downloaded.
func (r *runtime) refsFor(ctx context.Context, s *Session, args []string, f downloadFlags) ([]app.Ref, error) {
	if f.search != "" {
		if len(args) > 0 {
			return nil, usageErr("download", "--search cannot be combined with entry numbers")
		}
		page, err := s.Svc.Search(ctx, app.SearchQuery{Text: f.search, Page: 1, Refresh: r.g.Refresh})
		if err != nil {
			return nil, err
		}
		if len(page.Results) == 0 {
			return nil, &app.Error{Code: app.CodeNotFound, Message: "no entry matches the search", Args: map[string]any{"ref": f.search}}
		}
		return []app.Ref{{Slug: page.Results[0].Slug}}, nil
	}
	if len(args) == 0 {
		return nil, usageErr("download", "give at least one entry number or slug, or use --search")
	}
	refs := make([]app.Ref, 0, len(args))
	for _, a := range args {
		ref, err := parseRef("download", a, f.slug)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (r *runtime) runDownload(ctx context.Context, s *Session, args []string, f downloadFlags) (outcome, error) {
	p := r.printer()
	if f.link != 0 && len(f.prefer) > 0 {
		return outcome{}, usageErr("download", "--link and --prefer cannot be used together")
	}
	if f.boxart && f.noBoxart {
		return outcome{}, usageErr("download", "--boxart and --no-boxart cannot be used together")
	}
	pref, _, err := parsePrefer("download", f.prefer, s.Prefer)
	if err != nil {
		return outcome{}, err
	}
	refs, err := r.refsFor(ctx, s, args, f)
	if err != nil {
		return outcome{}, err
	}
	if f.link != 0 && len(refs) > 1 {
		return outcome{}, usageErr("download", "--link applies to one entry")
	}
	sel := app.LinkSelector{Index: f.link, Prefer: pref}
	reqs := make([]app.DownloadRequest, len(refs))
	var total int64
	for i, ref := range refs {
		choices, err := s.Svc.Links(ctx, ref, sel)
		if err != nil {
			return outcome{}, err
		}
		if len(choices) > 0 && choices[0].Available {
			total += choices[0].Link.Size
			r.announceLink(p, choices, preferActive(pref) || f.link != 0)
		}
		reqs[i] = app.DownloadRequest{Ref: ref, Link: sel, DestDir: r.g.Output}
	}
	if err := r.confirm(p, s, len(reqs), total); err != nil {
		return outcome{}, err
	}
	return r.transfer(ctx, s, reqs, f)
}

// announceLink prints "Link 2 chosen (...)" on stderr (prefer-syntax.md section 5).
func (r *runtime) announceLink(p *printer, choices []app.LinkChoice, show bool) {
	if !show {
		return
	}
	top := choices[0]
	why := strings.Join(top.Reasons, ", ")
	if why == "" {
		why = "-"
	}
	p.info("%s", p.dim(clean(p.T("ui.cli.download.link_chosen", i18n.Args{"index": top.Index, "why": why, "count": len(choices)}))))
}

// confirm asks before a batch of more than one file or more than 500 MB when
// interface.confirm_downloads is on. It never prompts with --json, -q or a piped stdin: it fails
// with confirmation_required unless --force.
func (r *runtime) confirm(p *printer, s *Session, count int, total int64) error {
	if !s.Confirm || r.g.Force || (count <= 1 && total <= confirmBytes) {
		return nil
	}
	if r.g.JSON || r.g.Quiet || r.d.IsTerminal == nil || !r.d.IsTerminal(r.d.Stdin) {
		return &app.Error{Code: app.CodeConfirmationNeeded, Message: "this command needs confirmation"}
	}
	dest := r.g.Output
	if dest == "" {
		dest = p.T("ui.cli.download.dest_default", nil)
	}
	_, _ = fmt.Fprintf(p.err, "%s ", clean(p.T("ui.cli.download.confirm", i18n.Args{"count": count, "size": humanSize(total), "dest": dest})))
	line, _ := bufio.NewReader(r.d.Stdin).ReadString('\n')
	if !p.isYes(strings.TrimSpace(line)) {
		return &app.Error{Code: app.CodeCanceled, Message: "canceled"}
	}
	return nil
}

func (p *printer) isYes(answer string) bool {
	a := strings.ToLower(answer)
	if a == "" {
		return false
	}
	for _, y := range []string{"y", "yes", strings.ToLower(p.T("ui.messages.yes", nil))} {
		if a == y || a == string([]rune(y)[:1]) && len([]rune(a)) == 1 {
			return true
		}
	}
	return false
}

// transfer queues the batch, streams the state changes to stderr, waits, saves covers and renders
// the summary. Ctrl+C cancels every job of the batch and waits for them to settle.
func (r *runtime) transfer(ctx context.Context, s *Session, reqs []app.DownloadRequest, f downloadFlags) (outcome, error) {
	p := r.printer()
	evCtx, stopEvents := context.WithCancel(ctx)
	var wg sync.WaitGroup
	if !p.g.JSON && !p.g.Quiet {
		events := s.Svc.Events(evCtx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.streamEvents(events)
		}()
	}
	started := time.Now()
	ids, err := s.Svc.Enqueue(ctx, reqs)
	if err != nil {
		stopEvents()
		wg.Wait()
		return outcome{}, err
	}
	p.info("%s", p.dim(p.N("ui.cli.download.start", len(ids), i18n.Args{"count": len(ids)})))
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			for _, id := range ids {
				_ = s.Svc.Cancel(id)
			}
		case <-stop:
		}
	}()
	res, werr := s.Svc.Wait(context.WithoutCancel(ctx))
	close(stop)
	stopEvents()
	wg.Wait()
	if werr != nil {
		return outcome{}, werr
	}
	wantBoxart := (s.Boxart || f.boxart) && !f.noBoxart
	if wantBoxart {
		r.saveBoxarts(ctx, p, s, res)
	}
	if res.RefreshWarning != "" {
		p.warning(res.RefreshWarning)
	}
	recs := r.recordsFor(ctx, s, res.Jobs)
	out := outcome{data: map[string]any{"jobs": jobsJSON(res.Jobs, recs)}, human: func(p *printer) { r.downloadHuman(p, res, time.Since(started)) }}
	return out, batchError(res)
}

// saveBoxarts writes the cover of every finished job; a failure is a warning, never fatal.
func (r *runtime) saveBoxarts(ctx context.Context, p *printer, s *Session, res app.BatchResult) {
	for _, j := range res.Jobs {
		if j.State != app.StateDone {
			continue
		}
		if _, err := s.Svc.SaveBoxart(context.WithoutCancel(ctx), app.Ref{Slug: j.Slug}, r.g.Output); err != nil {
			ae := asAppError(err)
			if ae.Code != app.CodeNotFound { // an entry without a cover is normal
				p.warning(p.T("ui.cli.boxart.failed", i18n.Args{"title": clean(j.Title), "code": ae.Code}))
			}
		}
	}
}

// recordsFor finds the history record of each job (crc, hash and attempts live there).
func (r *runtime) recordsFor(ctx context.Context, s *Session, jobs []app.JobSnapshot) map[string]history.Record {
	recs, err := s.Svc.History(context.WithoutCancel(ctx), history.Filter{Limit: len(jobs) + 50})
	out := map[string]history.Record{}
	if err != nil {
		return out
	}
	for _, rec := range recs {
		out[rec.ID] = rec
	}
	return out
}

func jobsJSON(jobs []app.JobSnapshot, recs map[string]history.Record) []map[string]any {
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		rec := recs[string(j.ID)]
		var e any
		if j.Err != nil {
			e = map[string]string{"code": j.Err.Code, "message": j.Err.Message}
		}
		out = append(out, map[string]any{
			"slug": j.Slug, "link_index": j.LinkIndex, "filename": j.Name, "raw_filename": rec.RawName, "path": j.Path,
			"state": string(j.State), "bytes": j.Bytes, "expected_bytes": j.Expected, "sha256": j.SHA256,
			"crc": rec.CRC, "hash": rec.Hash, "attempts": j.Attempt, "duration_ms": j.Finished.Sub(j.Started).Milliseconds(), "error": e,
		})
	}
	return out
}

// batchError maps the result to the exit codes: 130 when interrupted, 7 when some jobs failed and
// others did not, otherwise the code of the first failure.
func batchError(res app.BatchResult) error {
	var first *app.Error
	for _, j := range res.Jobs {
		if j.Err != nil && (j.State == app.StateFailed || j.State == app.StateCanceled) {
			first = j.Err
			break
		}
	}
	switch {
	case res.Cancel > 0 && res.Failed == 0:
		return &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "canceled"}, code: 130}
	case first == nil:
		return nil
	case res.Done+res.Skipped > 0:
		return &exitError{err: first, code: 7}
	}
	return first
}

func (r *runtime) downloadHuman(p *printer, res app.BatchResult, elapsed time.Duration) {
	for _, j := range res.Jobs {
		title := clean(firstNonEmpty(j.Title, j.Slug))
		switch j.State {
		case app.StateDone:
			if p.g.Quiet {
				_, _ = fmt.Fprintln(p.out, clean(j.Path))
				continue
			}
			p.info("%s", p.ok(p.sym("ok")+" "+p.T("ui.cli.download.saved", i18n.Args{"path": clean(j.Path), "size": humanSize(j.Bytes), "time": humanDuration(j.Finished.Sub(j.Started))})))
		case app.StateSkipped:
			if p.g.Quiet {
				_, _ = fmt.Fprintln(p.out, clean(j.Path))
				continue
			}
			p.info("%s", p.dim(p.T("ui.cli.download.skipped", i18n.Args{"path": clean(j.Path)})))
		case app.StateFailed, app.StateCanceled:
			p.failedLine(title, j.Err)
		}
	}
	if p.g.Quiet && res.Failed == 0 {
		return
	}
	p.info("%s", p.T("ui.download.summary", i18n.Args{"done": res.Done, "skipped": res.Skipped, "failed": res.Failed + res.Cancel, "elapsed": humanDuration(elapsed)}))
}

// failedLine prints one failed job: "x <title>: <message> [code]".
func (p *printer) failedLine(title string, e *app.Error) {
	code, msg := app.CodeInternal, ""
	if e != nil {
		code, msg = e.Code, e.Message
		if p.t.Has("errors." + code + ".message") {
			msg = p.t.T("errors."+code+".message", errorArgs(e))
		}
	}
	line := fmt.Sprintf("%s %s: %s [%s]", p.sym("fail"), title, clean(msg), code)
	_, _ = fmt.Fprintln(p.err, p.bad(line))
}

// streamEvents prints one line per state change (no live redraw: the lines are the same on a
// terminal and in a pipe).
func (p *printer) streamEvents(events <-chan app.Event) {
	last := map[app.JobID]app.State{}
	for ev := range events {
		j := ev.Job
		if last[j.ID] == j.State || j.Terminal() {
			continue
		}
		last[j.ID] = j.State
		label := clean(firstNonEmpty(j.Title, j.Slug))
		var line string
		switch j.State {
		case app.StateQueued:
			line = p.dim(p.T("ui.status.queued", nil))
		case app.StatePreflight, app.StateConnecting:
			line = p.T("ui.cli.download.connecting", nil)
		case app.StateWarming:
			line = p.warn(p.T("ui.cli.download.warming", nil))
		case app.StateTransferring:
			line = p.accent(p.T("ui.status.downloading", nil))
		case app.StateRetryWait:
			line = p.warn(p.T("ui.cli.download.retry", i18n.Args{"attempt": j.Attempt}))
		case app.StateVerifying:
			line = p.T("ui.cli.download.verifying", nil)
		case app.StateFinalizing:
			line = p.T("ui.cli.download.saving", nil)
		default:
			continue
		}
		_, _ = fmt.Fprintf(p.err, " %s: %s\n", label, line)
	}
}

func humanDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- boxart ----

func (r *runtime) boxartCmd() *cobra.Command {
	var slug bool
	c := &cobra.Command{
		Use:   "boxart <#|slug>",
		Short: "Save the cover of an entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			r.g.Downloads = true
			return r.exec(func(ctx context.Context, s *Session) (outcome, error) {
				ref, err := parseRef("boxart", args[0], slug)
				if err != nil {
					return outcome{}, err
				}
				entry, err := s.Svc.Entry(ctx, ref)
				if err != nil {
					return outcome{}, err
				}
				path, err := s.Svc.SaveBoxart(ctx, app.Ref{Slug: entry.Value.Slug}, r.g.Output)
				if err != nil {
					return outcome{}, err
				}
				return outcome{data: map[string]any{"slug": entry.Value.Slug, "path": path}, meta: &entry.Meta, human: func(p *printer) {
					_, _ = fmt.Fprintln(p.out, clean(path))
				}}, nil
			})
		},
	}
	c.Flags().BoolVar(&slug, "slug", false, "read the argument as a slug")
	c.Flags().StringVarP(&r.g.Output, "output", "o", "", "destination folder")
	return c
}
