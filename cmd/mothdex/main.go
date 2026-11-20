// Command mothdex is the Mothdex command-line client.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mothdex:", err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintf(w, "mothdex %s\n", version)
	return err
}
