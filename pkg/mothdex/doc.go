// Package mothdex is the public client for the Mothdex API v1: typed
// requests and responses, typed errors, client-side rate limiting, bounded
// retries and a circuit breaker for repeated HTTP 429 answers.
//
// Transport security: system roots, TLS 1.2 minimum, no option to disable certificate
// verification. Binaries that must run on systems without a CA bundle import
// golang.org/x/crypto/x509roots/fallback in their main package; this library does not,
// because the fallback roots are registered process-wide.
//
// The package knows nothing about caches, configuration files, UI or
// downloads. A Client is safe for concurrent use.
package mothdex
