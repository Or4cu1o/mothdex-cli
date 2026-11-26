// Command gen-errors regenerates the user-facing error reference from locale keys.
package main

import (
	"log"
	"os"

	"github.com/Or4cu1o/mothdex-cli/internal/helpreference"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func main() {
	b, err := i18n.Load(locales.FS)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("docs/errors.md", []byte(helpreference.Errors(b, i18n.Fallback)), 0o644); err != nil { //nolint:gosec // Public generated documentation.
		log.Fatal(err)
	}
}
