package mothdex

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Kind classifies a failure. Callers branch on Kind, never on message text.
type Kind int

// Error kinds, see the contract table.
const (
	KindNetwork Kind = iota + 1
	KindNotFound
	KindInvalidRequest
	KindUpstream
	KindAPI
	KindDecode
	KindIncompatible
	KindRateLimited
	KindCanceled
)

var kindNames = map[Kind]string{
	KindNetwork:        "network",
	KindNotFound:       "not_found",
	KindInvalidRequest: "invalid_request",
	KindUpstream:       "upstream",
	KindAPI:            "api",
	KindDecode:         "decode",
	KindIncompatible:   "api_incompatible",
	KindRateLimited:    "rate_limited",
	KindCanceled:       "canceled",
}

// String returns the stable snake_case name of the kind.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "unknown"
}

// Error is the single error type returned by the client.
type Error struct {
	Kind       Kind
	Op         string        // "search", "entry", ...
	HTTPStatus int           // 0 for transport errors
	Message    string        // info.error or transport text; untrusted, for logs/UI only
	RetryAfter time.Duration // KindRateLimited only; 0 if unknown
	Err        error         // wrapped cause
}

// Error implements error.
func (e *Error) Error() string {
	msg := fmt.Sprintf("mothdex %s: %s", e.Op, e.Kind)
	if e.HTTPStatus != 0 {
		msg += fmt.Sprintf(" (HTTP %d)", e.HTTPStatus)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

func hasKind(err error, k Kind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == k
}

// IsNotFound reports whether err is a KindNotFound error.
func IsNotFound(err error) bool { return hasKind(err, KindNotFound) }

// IsInvalidRequest reports whether err is a KindInvalidRequest error.
func IsInvalidRequest(err error) bool { return hasKind(err, KindInvalidRequest) }

// IsRateLimited reports whether err is a KindRateLimited error.
func IsRateLimited(err error) bool { return hasKind(err, KindRateLimited) }

// IsUpstream reports whether err is a KindUpstream error.
func IsUpstream(err error) bool { return hasKind(err, KindUpstream) }

// IsNetwork reports whether err is a KindNetwork error.
func IsNetwork(err error) bool { return hasKind(err, KindNetwork) }

// IsCanceled reports whether err is a KindCanceled error.
func IsCanceled(err error) bool { return hasKind(err, KindCanceled) }

// IsIncompatible reports whether err is a KindIncompatible error.
func IsIncompatible(err error) bool { return hasKind(err, KindIncompatible) }

// Known info.error messages. This table is the only place that matches text.
var messageKinds = map[string]Kind{
	"Entry not found":              KindNotFound,
	"Slug is required":             KindInvalidRequest,
	"Invalid input provided":       KindInvalidRequest,
	"Database operation failed":    KindUpstream,
	"A database error occurred":    KindUpstream,
	"An unexpected error occurred": KindUpstream,
}

// Known info.code values, as named in docs/upstream/U2-hashes-issue.md. rate_limited is left out on
// purpose: a 429 is classified by HTTP status. Unknown codes map to KindAPI.
var codeKinds = map[string]Kind{
	"entry_not_found": KindNotFound,
	"invalid_request": KindInvalidRequest,
	"database_error":  KindUpstream,
	"internal_error":  KindUpstream,
}

// classifyInfoError maps an HTTP 200 info.error to a Kind. A code, when
// present, takes precedence over the message.
func classifyInfoError(info apiInfo) Kind {
	if info.Code != "" {
		if k, ok := codeKinds[info.Code]; ok {
			return k
		}
		return KindAPI
	}
	if k, ok := messageKinds[info.Error]; ok {
		return k
	}
	return KindAPI
}

// classifyStatus maps a non-200 HTTP status to a Kind.
func classifyStatus(status int) Kind {
	switch status {
	case http.StatusBadRequest:
		return KindInvalidRequest
	case http.StatusNotFound:
		return KindIncompatible
	case http.StatusTooManyRequests:
		return KindRateLimited
	default:
		return KindUpstream
	}
}

// retryableKind reports whether a failure of this kind may be retried with backoff.
func retryableKind(k Kind) bool { return k == KindUpstream || k == KindNetwork }
