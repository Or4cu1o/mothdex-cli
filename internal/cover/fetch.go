// Package cover fetches cover images under the untrusted-URL policy of cache.md section 4
// (T-16 CR-9): https only, no connection to a private or special address (checked after DNS
// and again in the dialer Control hook), at most three redirects, a 5 MiB body, a Content-Type
// that agrees with the magic bytes, and a header-only dimension check. Any violation is a miss.
package cover

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Limits of the cover policy.
const (
	MaxBytes     = 5 << 20
	MaxPixelSide = 4096
	maxRedirects = 3
	fetchTimeout = 20 * time.Second
	dialTimeout  = 10 * time.Second
)

var (
	errBlocked   = errors.New("cover: address not allowed")
	errRedirects = errors.New("cover: too many redirects")
)

// Resolver is the DNS port of the guard (downloader.md section 6, proxy.Resolver).
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// Options builds a Fetcher. Resolver and Cache are required.
type Options struct {
	Resolver Resolver
	Cache    cache.Store
	Log      *slog.Logger
	// UserAgent is sent on every request; empty selects mothdex.DefaultUserAgent("").
	UserAgent string
}

// hooks are the test seams. They are unexported on purpose: production code can neither skip
// the dialer Control hook nor weaken TLS.
type hooks struct {
	dial func(ctx context.Context, network, address string) (net.Conn, error)
	tls  *tls.Config
}

// Fetcher downloads covers into the cache.
type Fetcher struct {
	client *http.Client
	cache  cache.Store
	log    *slog.Logger
	agent  string
}

// New returns a Fetcher.
func New(o Options) (*Fetcher, error) { return newWith(o, hooks{}) }

func newWith(o Options, h hooks) (*Fetcher, error) {
	if o.Resolver == nil || o.Cache == nil {
		return nil, errors.New("cover: Resolver and Cache are required")
	}
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	agent := o.UserAgent
	if agent == "" {
		agent = mothdex.DefaultUserAgent("")
	}
	dial := h.dial
	if dial == nil {
		dial = defaultDialer().DialContext
	}
	tr := newTransport(guardedDial(o.Resolver, dial), tlsConfig(h.tls))
	return &Fetcher{
		cache: o.Cache,
		log:   log,
		agent: agent,
		client: &http.Client{Transport: tr, Timeout: fetchTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errRedirects
			}
			return checkURL(req.URL)
		}},
	}, nil
}

// guardedDial resolves the host itself, refuses the connection if any answer is a blocked
// address, then dials the checked IP so the connection cannot see a second, different answer.
func guardedDial(r Resolver, dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errBlocked
		}
		ips, err := resolve(ctx, r, host)
		if err != nil {
			return nil, err
		}
		lastErr := error(errBlocked)
		for _, ip := range ips {
			c, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func resolve(ctx context.Context, r Resolver, host string) ([]netip.Addr, error) {
	var raw []net.IP
	if a, err := netip.ParseAddr(host); err == nil {
		raw = []net.IP{a.AsSlice()}
	} else if raw, err = r.LookupIP(ctx, host); err != nil {
		return nil, fmt.Errorf("cover: resolve: %w", err)
	}
	out := make([]netip.Addr, 0, len(raw))
	for _, ip := range raw {
		a, ok := netip.AddrFromSlice(ip)
		if !ok || blockedIP(a) {
			return nil, errBlocked // one bad answer rejects the host: no picking the good one
		}
		out = append(out, a.Unmap())
	}
	if len(out) == 0 {
		return nil, errBlocked
	}
	return out, nil
}

func checkURL(u *url.URL) error {
	if u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return errors.New("cover: only plain https URLs are fetched")
	}
	return nil
}

// Fetch gets rawURL and stores the image in the covers namespace. It returns false for every
// violation or failure (a cache miss, logged at debug level, never an error to the caller).
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) bool {
	body, ok := f.get(ctx, rawURL)
	if !ok {
		return false
	}
	ct := "image/" + sniff(body)
	if err := f.cache.PutBlob(cache.NSCovers, cache.KeyCover(rawURL), bytes.NewReader(body), ct, int64(len(body))); err != nil {
		f.log.Debug("cover not cached", "err", err)
		return false
	}
	return true
}

func (f *Fetcher) get(ctx context.Context, rawURL string) ([]byte, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || checkURL(u) != nil {
		f.log.Debug("cover refused", "reason", "not a plain https URL")
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", f.agent)
	resp, err := f.client.Do(req)
	if err != nil {
		f.log.Debug("cover fetch failed", "err", errors.Unwrap(err))
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		f.log.Debug("cover refused", "status", resp.StatusCode)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil || len(body) > MaxBytes {
		f.log.Debug("cover refused", "reason", "unreadable or over 5 MiB")
		return nil, false
	}
	kind := sniff(body)
	if kind == "" || kind != typeOfMedia(resp.Header.Get("Content-Type")) {
		f.log.Debug("cover refused", "reason", "content type and magic bytes disagree")
		return nil, false
	}
	w, h, err := dimensions(kind, body)
	if err != nil || w < 1 || h < 1 || w > MaxPixelSide || h > MaxPixelSide {
		f.log.Debug("cover refused", "reason", "unreadable or oversized image")
		return nil, false
	}
	return body, true
}

// tlsConfig is the production TLS policy: TLS 1.2 or newer, verification always on. The
// argument exists only for the package tests, which trust a local certificate authority.
func tlsConfig(test *tls.Config) *tls.Config {
	if test != nil {
		return test
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

// SystemResolver adapts net.Resolver (LookupIP takes a network) to Resolver.
type SystemResolver struct{ R *net.Resolver }

// LookupIP resolves host over IPv4 and IPv6 ("ip"); a nil R means net.DefaultResolver.
func (s SystemResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	r := s.R
	if r == nil {
		r = net.DefaultResolver
	}
	return r.LookupIP(ctx, "ip", host)
}

// defaultDialer is the production dialer: its Control hook re-checks the address the socket is
// about to connect to.
func defaultDialer() *net.Dialer { return &net.Dialer{Timeout: dialTimeout, Control: control} }

// newTransport builds the cover transport. Covers go direct (Proxy nil): a proxy would connect
// to the target on our behalf and skip the guard.
func newTransport(dial func(context.Context, string, string) (net.Conn, error), cfg *tls.Config) *http.Transport {
	return &http.Transport{
		DialContext:         dial,
		TLSClientConfig:     cfg,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: dialTimeout,
		DisableKeepAlives:   true,
		Proxy:               nil,
	}
}
