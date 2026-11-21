package proxy

import (
	"net/netip"
	"net/url"
	"strings"
)

// DefaultAllowedHosts is the v0.1 allowlist: the Mothdex proxy only (D16).
var DefaultAllowedHosts = []string{"proxy.mothdex.net"}

// CheckURL applies the URL policy of downloader.md section 2 to a link URL that came from the
// network or the cache. It returns the URL to request, rebuilt from the checked parts, or an
// untrusted_url error that never echoes the input. Scheme exactly https, no user info, port
// empty or 443, host an exact lower-case ASCII match (one trailing dot stripped) of allowed;
// IP literals and non-ASCII hosts are refused.
func CheckURL(raw string, allowed []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" {
		return nil, untrusted("not a plain https URL")
	}
	if p := u.Port(); p != "" && p != "443" {
		return nil, untrusted("port is not allowed")
	}
	host, ok := normalizeHost(u.Hostname())
	if !ok {
		return nil, untrusted("host is not a plain DNS name")
	}
	if !hostAllowed(host, allowed) {
		return nil, untrusted("host is not allowed")
	}
	return &url.URL{Scheme: "https", Host: host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}, nil
}

func untrusted(msg string) *Error { return &Error{Code: CodeUntrustedURL, Message: msg} }

// normalizeHost lower-cases an ASCII DNS name and strips one trailing dot. It refuses IP
// literals (including IPv6 zones), IDN and any byte outside letters, digits, '-' and '.'.
func normalizeHost(h string) (string, bool) {
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return "", false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return "", false
		}
	}
	h = strings.ToLower(h)
	if _, err := netip.ParseAddr(h); err == nil {
		return "", false
	}
	if strings.HasPrefix(h, ".") || strings.Contains(h, "..") || strings.HasPrefix(h, "xn--") || strings.Contains(h, ".xn--") {
		return "", false
	}
	return h, true
}

func hostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		if n, ok := normalizeHost(a); ok && n == host {
			return true
		}
	}
	return false
}
