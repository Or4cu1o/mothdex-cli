// Command mothdex is the Mothdex command-line client.
package main

import (
	"fmt"
	"io"
	"os"

	// Fallback CA roots for systems without a CA bundle (CR-12); verification is never disabled.
	_ "golang.org/x/crypto/x509roots/fallback"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mothdex:", err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintln(w, buildinfo.Get().String())
	return err
}
