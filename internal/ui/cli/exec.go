package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// outcome is what a command produced: the --json data, the human renderer, and where the answer
// came from. A command that fails after producing partial data (a batch download) returns both.
type outcome struct {
	data  any
	meta  *app.ResultMeta
	human func(p *printer)
}

// exec opens the service, runs fn, and prints the result in the mode the flags select.
func (r *runtime) exec(fn func(ctx context.Context, s *Session) (outcome, error)) error {
	s, err := r.open()
	if err != nil {
		return err
	}
	out, ferr := fn(r.ctx, s)
	p := r.printer()
	if ferr != nil && out.data == nil {
		return ferr
	}
	p.meta = out.meta
	if p.g.JSON {
		r.jsonDone = true
		if werr := p.writeJSON(r.cmd, out.data, ferr); werr != nil {
			return werr
		}
	} else if out.human != nil {
		out.human(p)
	}
	return ferr
}

// noSession is for commands that never need the service (version, shell, tui, gui).
func (r *runtime) local(fn func(p *printer) (any, func(p *printer), error)) error {
	p := r.printer()
	data, human, err := fn(p)
	if err != nil {
		return err
	}
	if p.g.JSON {
		r.jsonDone = true
		return p.writeJSON(r.cmd, data, nil)
	}
	if human != nil {
		human(p)
	}
	return nil
}

// parseRef reads <#|slug>: all digits is a 1-based index of the last search, anything else a slug.
// --slug forces the slug reading (a numeric slug is possible).
func parseRef(command, arg string, forceSlug bool) (app.Ref, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return app.Ref{}, usageErr(command, "an entry number or slug is required")
	}
	if !forceSlug {
		if n, err := strconv.Atoi(arg); err == nil {
			if n < 1 {
				return app.Ref{}, usageErr(command, "the entry number starts at 1")
			}
			return app.Ref{Index: n}, nil
		}
	}
	return app.Ref{Slug: arg}, nil
}

// splitList flattens repeated and comma-separated flag values.
func splitList(in []string) []string {
	var out []string
	for _, v := range in {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
