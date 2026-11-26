package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
)

const confirmBytes = app.ConfirmBytes // confirm_downloads asks above this size or for more than one file

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
	logValues(fl, "search", "link", "prefer", "slug", "boxart", "no-boxart", "jobs", "allow-executable")
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
	ask := r.chooserApplies(f, pref, len(refs))
	reqs := make([]app.DownloadRequest, len(refs))
	var total int64
	for i, ref := range refs {
		rsel := sel
		choices, err := s.Svc.Links(ctx, ref, rsel)
		if err != nil {
			return outcome{}, err
		}
		if ask {
			n, err := r.chooseLink(ctx, p, s, ref, choices)
			if err != nil {
				return outcome{}, err
			}
			rsel.Index = n
		}
		if size, ok := linkSize(choices, rsel.Index); ok {
			total += size
			// A Shell batch says which link each entry takes, so a number typed for several entries
			// never hides a choice; a single entry has the chooser (shell.md section 4).
			batchInShell := r.run != nil && len(refs) > 1 && len(choices) > 1
			r.announceLink(p, choices, preferActive(pref) || f.link != 0 || batchInShell)
		}
		reqs[i] = app.DownloadRequest{Ref: ref, Link: rsel, DestDir: r.g.Output, SizeConsent: sizeConsent(r.g.Force)}
	}
	if err := r.confirm(p, s, len(reqs), total); err != nil {
		return outcome{}, err
	}
	return r.transfer(ctx, s, reqs, f)
}

// sizeConsent turns the -f of this command into the consent for the real size (downloader.md 3.2
// step 6). It reads the flag of the command being run, never a start-up flag of a session.
func sizeConsent(force bool) int64 {
	if force {
		return app.ConsentAny
	}
	return 0
}

// linkSize is the size of the link the download will use: the one asked for by number, otherwise
// the top-ranked one. ok is false when it cannot be downloaded.
func linkSize(choices []app.LinkChoice, index int) (int64, bool) {
	for _, c := range choices {
		if (index == 0 || c.Index == index) && c.Available {
			return c.Link.Size, true
		}
	}
	return 0, false
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
	if r.g.JSON || r.g.Quiet || !r.canPrompt() {
		return &app.Error{Code: app.CodeConfirmationNeeded, Message: "this command needs confirmation"}
	}
	dest := r.g.Output
	if dest == "" {
		dest = p.T("ui.cli.download.dest_default", nil)
	}
	line, _ := r.readAnswer(clean(p.T("ui.cli.download.confirm", i18n.Args{"count": count, "size": humanSize(total), "dest": dest})) + " ")
	if !p.isYes(strings.TrimSpace(line)) {
		return &app.Error{Code: app.CodeCanceled, Message: "canceled"}
	}
	return nil
}

// canPrompt is true when a person can answer: stdin is a terminal, or the Shell says it is.
func (r *runtime) canPrompt() bool {
	if r.run != nil {
		return r.run.interactive
	}
	return r.isTTY(r.d.Stdin)
}

// readAnswer asks one question. Inside the Shell the line editor reads it (and prints the label);
// otherwise the label goes to stderr and the answer comes from stdin.
func (r *runtime) readAnswer(label string) (string, error) {
	if r.run != nil {
		if hook := r.run.promptHook(); hook != nil {
			return hook(label)
		}
	}
	_, _ = fmt.Fprint(r.printer().err, label)
	line, err := bufio.NewReader(r.d.Stdin).ReadString('\n')
	if line == "" && err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
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
	// A ref is given to an error that is shown: here only --json shows these, the human output
	// asks for the ref of each line it prints.
	jobRef := func(e *app.Error) string {
		if !r.g.JSON {
			return ""
		}
		return p.ref(e)
	}
	out := outcome{data: map[string]any{"jobs": jobsJSON(res.Jobs, recs, jobRef)}, human: func(p *printer) { r.downloadHuman(p, res, time.Since(started)) },
		warn: func(p *printer) { // the size drift notes (3.2 step 7), in both modes
			for _, j := range res.Jobs {
				if j.SizeDrift {
					p.warning(p.T("ui.cli.download.size_drift", i18n.Args{"title": clean(firstNonEmpty(j.Title, j.Slug)),
						"real": humanSize(j.Expected), "catalog": humanSize(j.CatalogSize)}))
				}
			}
		}}
	if stopped := consentJobs(res); len(stopped) > 0 && r.canAskConsent() {
		reqOf := make(map[app.JobID]app.DownloadRequest, len(ids))
		for i, id := range ids {
			reqOf[id] = reqs[i]
		}
		out.follow = func() (outcome, error) { return r.askConsent(ctx, s, f, stopped, reqOf, batchError(res)) }
	}
	return out, batchError(res)
}

// consentJobs are the jobs of a batch that stopped on their real size and wait for a person's OK.
func consentJobs(res app.BatchResult) []app.JobSnapshot {
	var out []app.JobSnapshot
	for _, j := range res.Jobs {
		if consent.Needed(j) {
			out = append(out, j)
		}
	}
	return out
}

// canAskConsent: the "Download anyway" question is asked only inside the Shell, when a person is at a
// terminal on both ends (a Runner is interactive only then), never with --json or -q. The plain command
// line keeps its -f and its exit code.
func (r *runtime) canAskConsent() bool {
	return r.run != nil && r.run.interactive && !r.g.JSON && !r.g.Quiet
}

// askConsent asks once per stopped job, after the batch ended, naming the real size and the catalog's,
// and queues again the ones answered yes with the consent for exactly that size. If the proxy then
// reports another size, the same question comes back with the new one. firstErr is what the batch
// returned: it stays the result when nothing is downloaded anyway, and also when some job was refused
// (the others may download, but the refused one still stopped), without a second error block, since the
// questions said it all. Ctrl+C or the end of input at a question aborts the lot: nothing is queued.
func (r *runtime) askConsent(ctx context.Context, s *Session, f downloadFlags, stopped []app.JobSnapshot, reqOf map[app.JobID]app.DownloadRequest, firstErr error) (outcome, error) {
	p := r.printer()
	var again []app.DownloadRequest
	refused := 0
	for _, j := range stopped {
		req, ok := reqOf[j.ID]
		if !ok {
			continue
		}
		suffix, args := consentArgs(j)
		line, err := r.readAnswer(clean(p.T("ui.consent.shell_ask"+suffix, args)) + " ")
		if err != nil {
			return outcome{}, &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "canceled"}, code: 130, silent: true}
		}
		if p.isYes(strings.TrimSpace(line)) {
			again = append(again, consent.WithConsent(req, j))
		} else {
			refused++
		}
	}
	kept := silent(firstErr)
	if len(again) == 0 {
		return outcome{}, kept
	}
	out, err := r.transfer(ctx, s, again, f)
	if refused == 0 {
		return out, err
	}
	// A refused job still stopped: the result of the new batch must not hide it, even when that batch is clean
	// or asks its own questions.
	if inner := out.follow; inner != nil {
		out.follow = func() (outcome, error) {
			next, nerr := inner()
			if nerr == nil {
				nerr = kept
			}
			return next, nerr
		}
	}
	if err == nil {
		err = kept
	}
	return out, err
}

// silent is the error of a batch with no second error block: the lines and questions already said it all.
func silent(err error) error {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee
	}
	var ae *app.Error
	if errors.As(err, &ae) {
		return &exitError{err: ae, code: ae.ExitCode(), silent: true}
	}
	return err
}

// consentArgs are the values of the "Download anyway" texts and the suffix of their keys (see consent.Args).
func consentArgs(j app.JobSnapshot) (suffix string, args i18n.Args) {
	suffix, a := consent.Args(j, humanSize)
	a["name"], a["dir"] = clean(firstNonEmpty(j.Name, firstNonEmpty(j.Title, j.Slug))), clean(consent.Dir(j))
	return suffix, i18n.Args(a)
}

// saveBoxarts writes the cover of every finished job, named after its file; a failure is a warning,
// never fatal. After Ctrl+C no cover is fetched: the exit must not wait for the network.
func (r *runtime) saveBoxarts(ctx context.Context, p *printer, s *Session, res app.BatchResult) {
	for _, j := range res.Jobs {
		if j.State != app.StateDone {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if _, err := s.Svc.SaveJobBoxart(ctx, j, r.g.Output); err != nil {
			ae := asAppError(err)
			if ae.Code != app.CodeNotFound { // an entry without a cover is normal
				p.warning(p.T("ui.cli.boxart.failed", i18n.Args{"title": clean(j.Title), "code": ae.Code}))
			}
		}
	}
}

// recordsFor finds the history record of each job by its id (crc, hash and attempts live there).
func (r *runtime) recordsFor(ctx context.Context, s *Session, jobs []app.JobSnapshot) map[string]app.HistoryRecord {
	out := map[string]app.HistoryRecord{}
	recs, err := s.Svc.History(context.WithoutCancel(ctx), app.HistoryFilter{})
	if err != nil {
		return out
	}
	want := make(map[string]bool, len(jobs))
	for _, j := range jobs {
		want[string(j.ID)] = true
	}
	for _, rec := range recs {
		if want[rec.ID] {
			out[rec.ID] = rec
		}
	}
	return out
}

// checkState is the crc/hash value of cli-output section 3: ok, skipped or failed. A job with no
// history record (the record could not be written) was not verified as far as we can tell.
func checkState(v string) string {
	switch v {
	case "ok", "failed":
		return v
	}
	return "skipped"
}

func jobsJSON(jobs []app.JobSnapshot, recs map[string]app.HistoryRecord, ref func(*app.Error) string) []map[string]any {
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		rec := recs[string(j.ID)]
		var e any
		if j.Err != nil {
			e = map[string]string{"code": j.Err.Code, "message": j.Err.Message, "ref": ref(j.Err)}
		}
		out = append(out, map[string]any{
			"slug": j.Slug, "link_index": j.LinkIndex, "filename": j.Name, "raw_filename": rec.RawName, "path": j.Path,
			"state": string(j.State), "bytes": j.Bytes, "expected_bytes": j.Expected, "catalog_size": j.CatalogSize, "sha256": j.SHA256,
			"crc": checkState(rec.CRC), "hash": checkState(rec.Hash), "attempts": j.Attempt, "duration_ms": j.Finished.Sub(j.Started).Milliseconds(), "error": e,
		})
	}
	return out
}

// batchError maps the result to the exit codes: 130 when the batch was interrupted (it wins over
// 7), 7 when some jobs failed and others did not, otherwise the code of the first failure. The
// per-job lines already printed the failures, so a batch error adds no second error block.
func batchError(res app.BatchResult) error {
	var first *app.Error
	for _, j := range res.Jobs {
		if j.Err != nil && j.State == app.StateFailed {
			first = j.Err
			break
		}
	}
	switch {
	case res.Cancel > 0:
		return &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "canceled"}, code: 130, silent: true}
	case first == nil:
		return nil
	case res.Done+res.Skipped > 0 || res.Failed > 1:
		return &exitError{err: first, code: 7, silent: true}
	}
	return first
}

func (r *runtime) downloadHuman(p *printer, res app.BatchResult, elapsed time.Duration) {
	lines := 0 // failed jobs printed one by one: their log is named once, after them
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
			switch {
			case consent.Needed(j) && len(res.Jobs) > 1:
				p.consentLine(title, j)
				lines++
			case len(res.Jobs) > 1:
				p.failedLine(title, j.Err)
				lines++
			}
		}
	}
	if lines > 0 {
		p.printLogLine()
	}
	if p.g.Quiet && res.Failed == 0 {
		return
	}
	if len(res.Jobs) == 1 && res.Jobs[0].State == app.StateCanceled {
		return // Ctrl+C on a single download: the exit code says it, nothing else is printed
	}
	waiting := len(consentJobs(res)) // stopped on their real size: nothing failed, the summary says so apart
	args := i18n.Args{"done": res.Done, "skipped": res.Skipped, "failed": max(res.Failed-waiting, 0) + res.Cancel, "elapsed": humanDuration(elapsed), "waiting": waiting}
	if waiting > 0 {
		p.info("%s", p.T("ui.download.summary_waiting", args))
		return
	}
	p.info("%s", p.T("ui.download.summary", args))
}

// consentLine prints a job that waits for the person's OK to its real size: a warning, not a failure,
// with the way out (-f) under it; the code stays visible for scripts.
func (p *printer) consentLine(title string, j app.JobSnapshot) {
	suffix, args := consentArgs(j)
	line := fmt.Sprintf("%s %s: %s [%s] ref: %s", p.sym("warn"), title, clean(p.T("ui.consent.row"+suffix, args)), j.Err.Code, p.ref(j.Err))
	_, _ = fmt.Fprintln(p.err, p.warn(line))
	if suffix == "" {
		_, _ = fmt.Fprintln(p.err, "  "+p.dim(clean(p.T("errors.confirmation_required.size_hint_cli", i18n.Args{"size": args["size"], "estimate": args["estimate"]}))))
	}
}

// failedLine prints one failed job: "x <title>: <message> [code]".
func (p *printer) failedLine(title string, e *app.Error) {
	if e == nil {
		e = &app.Error{Code: app.CodeInternal}
	}
	line := fmt.Sprintf("%s %s: %s [%s] ref: %s", p.sym("fail"), title, clean(p.localizedMessage(e, nil)), e.Code, p.ref(e))
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
	logValues(c.Flags(), "slug")
	return c
}
