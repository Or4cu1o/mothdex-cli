package mothdex

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

const maxRedirects = 3

// parseBaseURL enforces https, with http allowed only for loopback hosts (tests).
func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mothdex: invalid base URL: %w", err)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("mothdex: base URL must be scheme://host[/path] without credentials, query or fragment")
	}
	switch u.Scheme {
	case "https":
		return u, nil
	case "http":
		if isLoopback(u.Hostname()) {
			return u, nil
		}
		return nil, errors.New("mothdex: http base URL is only allowed for loopback hosts")
	default:
		return nil, errors.New("mothdex: base URL must use https")
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// newDefaultHTTPClient returns a client with TLS >= 1.2 and at most three
// redirects within the same host and scheme. Timeouts are applied per attempt
// through the request context.
func newDefaultHTTPClient() *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{CheckRedirect: sameHostRedirects}
	}
	clone := tr.Clone()
	clone.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: clone, CheckRedirect: sameHostRedirects}
}

func sameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	first := via[0].URL
	if req.URL.Host != first.Host || req.URL.Scheme != first.Scheme {
		return errors.New("redirect to a different host or scheme refused")
	}
	return nil
}
