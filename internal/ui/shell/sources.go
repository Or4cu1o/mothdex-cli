package shell

import (
	"context"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// lazyList is a list fetched on first use and kept for the process. A failed fetch gives nothing
// and is tried again only after retryAfter, so a dead network costs one wait per half minute.
type lazyList struct {
	fetch    func() ([]string, error)
	now      func() time.Time
	items    []string
	loaded   bool
	failedAt time.Time
}

func (l *lazyList) get() []string {
	if l.loaded {
		return l.items
	}
	if !l.failedAt.IsZero() && l.now().Sub(l.failedAt) < retryAfter {
		return nil
	}
	items, err := l.fetch()
	if err != nil {
		l.failedAt = l.now()
		return nil
	}
	l.items, l.loaded = items, true
	return items
}

// completer is the word completer handed to the line editor.
func (s *Shell) completer(ctx context.Context) func(string, int) (string, []string, string) {
	src := s.sources(ctx)
	return func(line string, pos int) (string, []string, string) { return complete(line, pos, src) }
}

// sources wires Tab to the service and to the session (shell.md section 6, ui.md section 2.3).
func (s *Shell) sources(ctx context.Context) completionSources {
	timed := func(fn func(ctx context.Context) ([]string, error)) *lazyList {
		return &lazyList{now: s.o.Now, fetch: func() ([]string, error) {
			c, cancel := context.WithTimeout(ctx, networkWait)
			defer cancel()
			return fn(c)
		}}
	}
	platforms := timed(func(c context.Context) ([]string, error) {
		res, err := s.o.Service.Platforms(c, app.PlatformFilter{})
		ids := make([]string, len(res.Value))
		for i, p := range res.Value {
			ids[i] = p.ID
		}
		return typable(ids), err
	})
	regions := timed(func(c context.Context) ([]string, error) {
		res, err := s.o.Service.Regions(c)
		ids := make([]string, len(res.Value))
		for i, r := range res.Value {
			ids[i] = r.ID
		}
		return typable(ids), err
	})
	keys := timed(func(context.Context) ([]string, error) {
		list, err := s.o.Service.ConfigList()
		out := make([]string, len(list))
		for i, st := range list {
			out[i] = st.Key
		}
		return out, err
	})
	namespaces := timed(func(context.Context) ([]string, error) {
		st, err := s.o.Service.CacheStats()
		out := make([]string, len(st.Namespaces))
		for i, n := range st.Namespaces {
			out[i] = n.Name
		}
		return out, err
	})
	return completionSources{
		commands:   s.o.Runner.Commands(),
		platforms:  platforms.get,
		regions:    regions.get,
		configKeys: keys.get,
		cacheNS:    namespaces.get,
		entryLinks: s.entryLinks,
		pageLen: func() int {
			page, _ := s.o.Runner.LastPage()
			return len(page.Results)
		},
		paths: s.o.Paths,
	}
}

// entryLinks are the language, format and source values of the links of the last entry shown by
// info. Values that cannot be typed as one word (a space or a comma) are left out.
func (s *Shell) entryLinks() (langs, formats, sources []string) {
	e, ok := s.o.Runner.LastEntry()
	if !ok {
		return nil, nil, nil
	}
	for _, l := range e.Links {
		for _, extra := range l.Extras {
			langs = append(langs, typable(strings.FieldsFunc(extra, func(r rune) bool { return r == ',' || r == ' ' }))...)
		}
		formats = append(formats, typable([]string{l.Format})...)
		sources = append(sources, typable([]string{l.SourceName})...)
	}
	return langs, formats, sources
}

// typable cleans server values before they reach the terminal as completion candidates (case 49)
// and keeps only those that can be typed as one word: no space, comma or empty value.
func typable(values []string) []string {
	var out []string
	for _, v := range values {
		if v = app.SanitizeText(v); v != "" && !strings.ContainsAny(v, " ,\t") {
			out = append(out, v)
		}
	}
	return out
}

// typableAsIs reports whether a name is already clean and one word: cleaning it would change what
// the user sees, so such a name is refused rather than offered in a different form.
func typableAsIs(v string) bool {
	clean := typable([]string{v})
	return len(clean) == 1 && clean[0] == v
}
