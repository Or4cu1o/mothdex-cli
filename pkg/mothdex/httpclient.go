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

// ValidateBaseURL reports whether raw is an acceptable API base URL: https, or http for loopback
// hosts only, without credentials, query or fragment. New applies the same rule.
func ValidateBaseURL(raw string) error {
	_, err := parseBaseURL(raw)
	return err
}

// parseBaseURL enforces https, with http allowed only for loopback hosts (tests).
func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// The text of a url.Parse failure quotes parts of the input (a bad port, a host), which
		// may be a credential, so the message is fixed and the cause is dropped.
		return nil, errors.New("mothdex: invalid base URL: not a valid URL")
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

// isLoopback accepts loopback IP literals only; names such as "localhost" can be repointed.
func isLoopback(host string) bool {
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
