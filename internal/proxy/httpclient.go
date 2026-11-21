package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const dialTimeout = 30 * time.Second // connect_timeout (downloader.md section 3)

// NewHTTPClient returns the client for downloads: TLS 1.2 or later, no compression (the body is
// always the raw file), no cookie jar, no overall timeout (the state machine owns the
// timeouts) and no redirects: a 3xx is returned to the downloader, which refuses it.
func NewHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   dialTimeout,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
