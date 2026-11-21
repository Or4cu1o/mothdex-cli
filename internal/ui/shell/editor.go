package shell

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/peterh/liner"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
)

// errAborted is what Prompt returns when the user pressed Ctrl+C.
var errAborted = cli.ErrPromptAborted

// lineEditor reads command lines. Prompt prints the label, and returns io.EOF at the end of input
// (Ctrl+D) and errAborted at Ctrl+C. The Shell depends on this small interface, so its tests run
// without a terminal, and the library behind it can change (ui.md section 2.4).
type lineEditor interface {
	Prompt(label string) (string, error)
	AppendHistory(line string)
	SetCompleter(fn func(line string, pos int) (head string, candidates []string, tail string))
	Close() error
}

// linerEditor is the terminal editor: history, Tab completion, Ctrl+R (peterh/liner).
type linerEditor struct{ st *liner.State }

func newLinerEditor() *linerEditor {
	st := liner.NewLiner()
	st.SetCtrlCAborts(true)
	st.SetTabCompletionStyle(liner.TabPrints)
	return &linerEditor{st: st}
}

func (e *linerEditor) Prompt(label string) (string, error) {
	line, err := e.st.Prompt(label)
	if errors.Is(err, liner.ErrPromptAborted) {
		return "", errAborted
	}
	return line, err
}

func (e *linerEditor) AppendHistory(line string) { e.st.AppendHistory(line) }

func (e *linerEditor) SetCompleter(fn func(string, int) (string, []string, string)) {
	e.st.SetWordCompleter(fn)
}

func (e *linerEditor) Close() error { return e.st.Close() }

// lineReader reads plain lines: a piped script, or a terminal that cannot do line editing (TERM
// dumb). With echo set it prints the prompt, so a person on a dumb terminal still sees it.
type lineReader struct {
	in   *bufio.Reader
	out  io.Writer
	echo bool
}

func newLineReader(in io.Reader, out io.Writer, echo bool) *lineReader {
	return &lineReader{in: bufio.NewReader(in), out: out, echo: echo}
}

func (l *lineReader) Prompt(label string) (string, error) {
	if l.echo {
		_, _ = fmt.Fprint(l.out, label)
	}
	line, err := l.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (l *lineReader) AppendHistory(string)                                      {}
func (l *lineReader) SetCompleter(func(string, int) (string, []string, string)) {}
func (l *lineReader) Close() error                                              { return nil }

// linerSupported reports whether the terminal can run the line editor (not TERM=dumb).
func linerSupported() bool { return liner.TerminalSupported() }
