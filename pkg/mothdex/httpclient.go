package mothdex

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
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

// errRedirectRefused is the sentinel CheckRedirect returns; net/http wraps it in a *url.Error, so
// the classifier uses errors.Is, before the transport-error branch (api-client 5.1).
var errRedirectRefused = errors.New("redirect refused")

// refusedRedirect carries the host of the refused target (never the full URL, which may hold
// credentials or a token) and matches errRedirectRefused.
type refusedRedirect struct{ host string }

func (e *refusedRedirect) Error() string        { return "redirect to " + e.host + " refused" }
func (e *refusedRedirect) Is(target error) bool { return target == errRedirectRefused }

// sameHostRedirects follows a redirect only within the origin of the original request (same
// scheme, host and effective port) and for at most three hops.
func sameHostRedirects(req *http.Request, via []*http.Request) error {
	refuse := &refusedRedirect{host: cleanMessage(req.URL.Hostname())}
	if len(via) > maxRedirects {
		return refuse
	}
	first := via[0].URL
	if !strings.EqualFold(req.URL.Scheme, first.Scheme) || !strings.EqualFold(req.URL.Hostname(), first.Hostname()) ||
		effectivePort(req.URL) != effectivePort(first) {
		return refuse
	}
	return nil
}

// effectivePort is the explicit port, or the default of the scheme.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}
