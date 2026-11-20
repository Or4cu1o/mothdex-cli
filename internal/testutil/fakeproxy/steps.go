package fakeproxy

import (
	"net/http"
	"time"
)

// Step describes how the server answers one request. The zero value is a
// plain 200 with an empty body; use the constructors for the named behaviors.
type Step struct {
	Status int         // 0 means 200
	Header http.Header // extra response headers
	Body   []byte

	// DeclaredLength overrides Content-Length: nil uses len(Body), a pointer
	// to a negative value sends no Content-Length (chunked).
	DeclaredLength *int64

	StallBeforeHeaders   time.Duration // fake time to wait before any header
	StallBeforeFirstByte time.Duration // headers sent, then wait (proxy "warming")
	StallAt              int           // with StallFor: pause after this many body bytes
	StallFor             time.Duration
	TruncateAt           int  // with Truncate: drop the connection after this many body bytes
	Truncate             bool // TruncateAt == 0 is a valid cut, so it needs its own flag
	Gzip                 bool // compress the body and send Content-Encoding: gzip
}

// With returns a copy of s with one more response header.
func (s Step) With(key, value string) Step {
	h := http.Header{}
	for k, v := range s.Header {
		h[k] = append([]string(nil), v...)
	}
	h.Add(key, value)
	s.Header = h
	return s
}

// Pattern returns n deterministic bytes.
func Pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}

func length(n int64) *int64 { return &n }

// OK answers 200 with the whole body and a matching Content-Length.
func OK(body []byte) Step { return Step{Body: body} }

// Truncate declares the full length but drops the connection after n bytes
// (a proxy whose piece wait timed out).
func Truncate(body []byte, n int) Step {
	if n < 0 || n >= len(body) {
		panic("fakeproxy: Truncate offset must be inside the body, or the step does nothing")
	}
	return Step{Body: body, Truncate: true, TruncateAt: n}
}

// StallBeforeFirstByte sends 200 and headers, then waits d before the body.
func StallBeforeFirstByte(body []byte, d time.Duration) Step {
	return Step{Body: body, StallBeforeFirstByte: d}
}

// StallMidStream sends the first at bytes, waits d, then sends the rest.
func StallMidStream(body []byte, at int, d time.Duration) Step {
	if at < 0 || at >= len(body) || d <= 0 {
		panic("fakeproxy: StallMidStream needs an offset inside the body and d > 0, or the step does nothing")
	}
	return Step{Body: body, StallAt: at, StallFor: d}
}

// StallBeforeHeaders waits d before answering at all (header timeout).
func StallBeforeHeaders(body []byte, d time.Duration) Step {
	return Step{Body: body, StallBeforeHeaders: d}
}

// NotFound answers 404 "not found" (possibly transient, A3).
func NotFound() Step {
	return Step{Status: http.StatusNotFound, Body: []byte("not found")}
}

// TooManyRequests answers 429; retryAfter is sent as Retry-After when set.
func TooManyRequests(retryAfter string) Step {
	s := Step{Status: http.StatusTooManyRequests, Body: []byte("slow down")}
	if retryAfter != "" {
		s = s.With("Retry-After", retryAfter)
	}
	return s
}

// ServerError answers the given 5xx status.
func ServerError(status int) Step {
	return Step{Status: status, Body: []byte("upstream error")}
}

// LengthMismatch sends body with a Content-Length of declared bytes. A
// declared value below len(body) makes the server stop at declared bytes.
func LengthMismatch(body []byte, declared int64) Step {
	if declared >= 0 && declared < int64(len(body)) {
		body = body[:declared]
	}
	return Step{Body: body, DeclaredLength: length(declared)}
}

// NoContentLength sends body chunked, with no Content-Length header.
func NoContentLength(body []byte) Step {
	return Step{Body: body, DeclaredLength: length(-1)}
}

// Oversized streams n bytes chunked with no Content-Length, for a link whose
// size is smaller (the downloader must stop at size+1 bytes).
func Oversized(n int) Step { return NoContentLength(Pattern(n)) }

// GzipDespiteIdentity answers with Content-Encoding: gzip whatever the
// request's Accept-Encoding said.
func GzipDespiteIdentity(body []byte) Step { return Step{Body: body, Gzip: true} }

// Redirect answers a 3xx with a Location header.
func Redirect(status int, location string) Step {
	return Step{Status: status}.With("Location", location)
}

// MaliciousDisposition serves body with the given raw Content-Disposition
// value (for example a traversal name). The downloader must ignore it.
func MaliciousDisposition(body []byte, value string) Step {
	return Step{Body: body}.With("Content-Disposition", value)
}
