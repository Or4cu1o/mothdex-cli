// Command mothdex is the Mothdex command-line client.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	// Fallback CA roots for systems without a CA bundle (CR-12); verification is never disabled.
	_ "golang.org/x/crypto/x509roots/fallback"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/shell"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	router := newSignalRouter(cancel, os.Exit, time.Now)
	go func() {
		for sig := range sigs {
			router.handle(sig)
		}
	}()
	d := deps(stdin, stdout, stderr)
	d.Interrupts = router.interrupts
	return cli.Execute(ctx, d, args)
}

func deps(stdin io.Reader, stdout, stderr io.Writer) cli.Deps {
	return cli.Deps{
		Open:       open,
		Translator: embeddedTranslator,
		Version:    buildinfo.Get,
		Stdin:      stdin, Stdout: stdout, Stderr: stderr,
		IsTerminal: isTerminal,
		Width:      terminalWidth,
		Getenv:     os.Getenv,
		Shell:      shell.Run,
	}
}

// embeddedTranslator serves errors that happen before the service opens (a bad flag, a broken
// configuration file): only the built-in locales, no file system access.
func embeddedTranslator(lang string) *i18n.Translator {
	b, err := i18n.Load(locales.FS)
	if err != nil {
		return nil
	}
	return b.New(b.Resolve(lang, os.Getenv, i18n.SystemLanguages))
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// terminalWidth reads COLUMNS; without it the width is unknown and the caller uses 80.
func terminalWidth() int {
	n, err := strconv.Atoi(os.Getenv("COLUMNS"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}
