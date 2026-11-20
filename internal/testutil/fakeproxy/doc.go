// Package fakeproxy is a scripted TLS test double for the Mothdex download
// proxy (docs/contracts/cli/downloader.md, T-16 threat model section 8).
//
// A Server answers each request from a per-path script of Steps. Steps are
// built with the behavior constructors (OK, Truncate, StallBeforeFirstByte,
// StallMidStream, StallBeforeHeaders, NotFound, TooManyRequests, ServerError,
// LengthMismatch, NoContentLength, GzipDespiteIdentity, Redirect,
// MaliciousDisposition, Oversized). Every wait is driven by an injected
// fakeclock.Clock, so tests never sleep for real: wait for the stall with
// Clock.BlockUntil, then Advance.
//
// Transport() dials the fake server whatever the URL host is, so tests can use
// the policy host (https://proxy.mothdex.net/...) and still hit the fake.
package fakeproxy
