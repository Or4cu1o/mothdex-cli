package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

const maxChoiceTries = 5 // a wrong answer is asked again this many times before giving up

// choiceAnswer is what the user typed at the link chooser, read by parseChoice.
type choiceAnswer int

const (
	answerPick    choiceAnswer = iota // a link: index is set
	answerCancel                      // q
	answerInvalid                     // not a link of the list
)

// parseChoice reads the answer to "Link [n]:" (shell.md section 4): empty takes the default, the
// top-ranked downloadable link; a number picks that link when it is downloadable; q cancels.
func parseChoice(answer string, choices []app.LinkChoice) (index int, kind choiceAnswer) {
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "q") {
		return 0, answerCancel
	}
	if answer == "" {
		if d := defaultChoice(choices); d > 0 {
			return d, answerPick
		}
		return 0, answerInvalid
	}
	n, err := strconv.Atoi(answer)
	if err != nil {
		return 0, answerInvalid
	}
	for _, c := range choices {
		if c.Index == n && c.Available {
			return n, answerPick
		}
	}
	return 0, answerInvalid
}

// defaultChoice is the link number the chooser marks with *: the first downloadable one in rank
// order, or 0 when none is.
func defaultChoice(choices []app.LinkChoice) int {
	for _, c := range choices {
		if c.Available {
			return c.Index
		}
	}
	return 0
}

func downloadable(choices []app.LinkChoice) int {
	n := 0
	for _, c := range choices {
		if c.Available {
			n++
		}
	}
	return n
}

// chooserApplies says whether `download` asks which link to use: inside an interactive Shell, for
// one entry, when the user gave no --link or preference and the output is for a person.
func (r *runtime) chooserApplies(f downloadFlags, pref app.Preference, entries int) bool {
	return r.run != nil && r.run.interactive && !r.g.JSON && !r.g.Quiet &&
		entries == 1 && f.search == "" && f.link == 0 && !preferActive(pref)
}

// chooseLink shows the links and asks which one to download. It returns 0 when there is nothing
// to choose from (fewer than two downloadable links), so the service takes its default.
func (r *runtime) chooseLink(ctx context.Context, p *printer, s *Session, ref app.Ref, choices []app.LinkChoice) (int, error) {
	if downloadable(choices) < 2 {
		return 0, nil
	}
	title := ""
	if e, err := s.Svc.Entry(ctx, ref); err == nil {
		title = e.Value.Title
	}
	def := defaultChoice(choices)
	box := *p // the chooser is a dialogue: it goes to the same stream as the prompt
	box.out = p.err
	_, _ = fmt.Fprintln(p.err, p.bold(clean(p.N("ui.shell.chooser.title", len(choices), i18n.Args{"title": title}))))
	rows := make([][]string, len(choices))
	for i, c := range choices {
		mark := strconv.Itoa(c.Index)
		note := strings.Join(c.Reasons, ", ")
		if c.Index == def {
			mark += "*"
			note = p.T("ui.shell.chooser.default", nil)
		}
		rows[i] = []string{mark, c.Link.Type, c.Link.Format, c.Link.SizeStr, strings.Join(c.Link.Extras, ","), c.Link.SourceName, note}
	}
	box.renderTable([]column{{}, {}, {}, {right: true}, {}, {}, {blank: true}}, rows, false)
	label := p.T("ui.shell.chooser.prompt", i18n.Args{"default": def}) + " "
	for try := 0; try < maxChoiceTries; try++ {
		answer, err := r.readAnswer(label)
		if err != nil {
			return 0, r.chooserCanceled(p)
		}
		switch n, kind := parseChoice(answer, choices); kind {
		case answerPick:
			return n, nil
		case answerCancel:
			return 0, r.chooserCanceled(p)
		}
		_, _ = fmt.Fprintln(p.err, p.warn(p.T("ui.shell.chooser.invalid", nil)))
	}
	return 0, r.chooserCanceled(p)
}

// chooserCanceled says so once and ends the command quietly with the interrupted exit code.
func (r *runtime) chooserCanceled(p *printer) error {
	p.info("%s", p.dim(p.T("errors.canceled.message", nil)))
	return &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "canceled"}, code: 130, silent: true}
}
