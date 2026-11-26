//go:build ignore

// Refresh the embedded Public Good root through verified Sigstore TUF metadata.
// Run from the repository root: go run scripts/update-trusted-root.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := fetcher.NewDefaultFetcher()
	f.SetHTTPClient(&http.Client{Timeout: 10 * time.Second})
	opts := tuf.DefaultOptions()
	opts.DisableLocalCache = true
	opts.Fetcher = f
	opts.Context = ctx
	tr, err := root.FetchTrustedRootWithOptions(opts)
	if err != nil {
		return err
	}
	b, err := tr.MarshalJSON()
	if err != nil {
		return err
	}
	return os.WriteFile("internal/update/trusted-root.json", append(b, '\n'), 0600)
}
