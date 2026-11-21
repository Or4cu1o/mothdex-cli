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
	"github.com/Or4cu1o/mothdex-cli/locales"
)

// secondInterrupt is the window in which a second Ctrl+C ends the process at once.
const secondInterrupt = 2 * time.Second

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { // the first signal cancels the context; a second one within 2 s exits at once
		<-ctx.Done()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		select {
		case <-sig:
			os.Exit(130)
		case <-time.After(secondInterrupt):
		}
	}()
	return cli.Execute(ctx, deps(stdin, stdout, stderr), args)
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
