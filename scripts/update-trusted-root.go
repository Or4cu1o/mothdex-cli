//go:build ignore

// Refresh the embedded Public Good root through verified Sigstore TUF metadata.
// Run from the repository root: go run scripts/update-trusted-root.go
// It prints the old and new SHA-256: after reviewing the diff, record the new one in
// trustedRootSHA256 (internal/update/native.go) and in docs/release.md.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	// One field per line, so a changed key or validity window is readable in the review.
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, b, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	const path = "internal/update/trusted-root.json"
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, pretty.Bytes(), 0600); err != nil {
		return err
	}
	fmt.Printf("old sha256 %x\nnew sha256 %x\n", sha256.Sum256(old), sha256.Sum256(pretty.Bytes()))
	return nil
}
