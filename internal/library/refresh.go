package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Refresh limits (T-16 CR-10).
const (
	RefreshTimeout = 3 * time.Second
	refreshPath    = "/reloadgames"
	maxRefreshBody = 64 << 10 // 64 KiB
)

// ErrManualRefresh tells the caller to print the localized hint profile.refresh_manual.
var ErrManualRefresh = errors.New("refresh is manual on this system")

// HTTPDoer is the part of *http.Client that Refresh needs.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewRefreshClient returns a client for Refresh: no proxy, no redirects, a 3 s timeout.
func NewRefreshClient() *http.Client {
	return &http.Client{
		Timeout:       RefreshTimeout,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ValidateRefreshURL accepts only http://127.0.0.1:1234/reloadgames and
// http://[::1]:1234/reloadgames: no other host (not even localhost), port, path, query,
// fragment or credentials.
func ValidateRefreshURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("refresh.url is not a valid URL")
	}
	hostOK := u.Host == "127.0.0.1:1234" || u.Host == "[::1]:1234"
	if u.Scheme != "http" || !hostOK || u.Path != refreshPath || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" {
		return errors.New("refresh.url must be http://127.0.0.1:1234/reloadgames or http://[::1]:1234/reloadgames")
	}
	return nil
}

// Refresh asks the frontend to rescan its ROMs. Run it once per batch after at least one job
// finished, never per file. The URL is re-validated here, a 3xx answer counts as a failure
// (redirects are never followed), and at most 64 KiB of the body is read. Callers treat any
// error as a warning: refresh is never fatal. "none" returns nil and "manual" returns
// ErrManualRefresh.
func (p Profile) Refresh(ctx context.Context, h HTTPDoer) error {
	switch p.Hook.Kind {
	case RefreshHTTPGet:
	case RefreshManual:
		return ErrManualRefresh
	default:
		return nil
	}
	if err := ValidateRefreshURL(p.Hook.URL); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, RefreshTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Hook.URL, nil)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	resp, err := h.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxRefreshBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("refresh answered HTTP %d", resp.StatusCode)
	}
	return nil
}
