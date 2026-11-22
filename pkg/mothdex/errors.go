package mothdex

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
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
	KindUntrustedRedirect // appended last so the numeric values of the other kinds stay stable
)

var kindNames = map[Kind]string{
	KindNetwork:           "network",
	KindNotFound:          "not_found",
	KindInvalidRequest:    "invalid_request",
	KindUpstream:          "upstream",
	KindAPI:               "api",
	KindDecode:            "decode",
	KindIncompatible:      "api_incompatible",
	KindRateLimited:       "rate_limited",
	KindCanceled:          "canceled",
	KindUntrustedRedirect: "untrusted_redirect",
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
	Code       string        // upstream info.error code (ENTRY_NOT_FOUND, ...); empty for the old message form
	Message    string        // info.message, old info.error text or transport text; untrusted, capped, for logs/UI only
	RetryAfter time.Duration // KindRateLimited only; 0 if unknown
	Err        error         // wrapped cause
}

// Error implements error.
func (e *Error) Error() string {
	msg := fmt.Sprintf("mothdex %s: %s", e.Op, e.Kind)
	if e.HTTPStatus != 0 {
		msg += fmt.Sprintf(" (HTTP %d)", e.HTTPStatus)
	}
	if e.Code != "" {
		msg += " [" + e.Code + "]"
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

// IsUntrustedRedirect reports whether err is a KindUntrustedRedirect error: the server redirected
// somewhere the client refuses to follow (api-client 5.1). It is never retried.
func IsUntrustedRedirect(err error) bool { return hasKind(err, KindUntrustedRedirect) }

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

// Upstream error codes (https://mothdex.net/api-docs/#error-codes). A string of this shape in
// info.error is a code, matched exactly; anything else is the old message form.
var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

const codeInvalidAPIVersion = "INVALID_API_VERSION"

var codeKinds = map[string]Kind{
	"ENTRY_NOT_FOUND":         KindNotFound,
	codeInvalidAPIVersion:     KindIncompatible,
	"MALFORMED_JSON":          KindInvalidRequest,
	"INVALID_JSON_OBJECT":     KindInvalidRequest,
	"MISSING_REQUIRED_FIELDS": KindInvalidRequest,
	"INVALID_FIELD_TYPE":      KindInvalidRequest,
	"INVALID_INPUT":           KindInvalidRequest,
	"DB_OPERATION_ERROR":      KindUpstream,
	"DB_ERROR":                KindUpstream,
	"UNEXPECTED_ERROR":        KindUpstream,
	"RATE_LIMIT_ERROR":        KindRateLimited,
	// HTTP_ERROR at 200, and any code not listed, are KindAPI.
}

func kindOfCode(code string) Kind {
	if k, ok := codeKinds[code]; ok {
		return k
	}
	return KindAPI
}

func kindOfMessage(msg string) Kind {
	if k, ok := messageKinds[msg]; ok {
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
