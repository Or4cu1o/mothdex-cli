// Command mothdex is the Mothdex command-line client.
package main

import (
	"fmt"
	"io"
	"os"

	// Fallback CA roots for systems without a CA bundle (CR-12); verification is never disabled.
	_ "golang.org/x/crypto/x509roots/fallback"
)

// Overridden at build time with -ldflags "-X main.version=..." (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mothdex:", err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintf(w, "mothdex %s (commit %s, built %s)\n", version, commit, date)
	return err
}
