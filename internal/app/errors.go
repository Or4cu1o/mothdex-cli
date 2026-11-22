package app

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Codes are the stable error codes of cli-output.md section 4 (also the i18n keys under
// errors.<code>).
const (
	CodeInternal            = "internal"
	CodeUsage               = "usage"
	CodeInvalidRequest      = "invalid_request"
	CodeNotFound            = "not_found"
	CodeNoSession           = "no_session"
	CodeIndexOutOfRange     = "index_out_of_range"
	CodeNetwork             = "network"
	CodeRateLimited         = "rate_limited"
	CodeUpstream            = "upstream"
	CodeAPIIncompatible     = "api_incompatible"
	CodeUntrustedURL        = "untrusted_url"
	CodeUntrustedRedirect   = "untrusted_redirect"
	CodeCanceled            = "canceled"
	CodeConfirmationNeeded  = "confirmation_required"
	CodeInvalidConfig       = "invalid_config"
	CodeUnmappedPlatform    = "unmapped_platform"
	CodePermissionDenied    = "permission_denied"
	exitInternal            = 1
	exitUsage               = 2
	exitNotFound            = 3
	exitNetwork             = 4
	exitIntegrity           = 5
	exitFilesystem          = 6
	exitPartial             = 7
	exitInterrupted         = 130
	maxSanitizedErrorLength = 2000
)

// Error is the error type that leaves the service. Message is English and stable; UIs show the
// localized text for Code. Cause keeps the underlying error for logs and -v only.
type Error struct {
	Code      string
	Kind      mothdex.Kind // zero unless the failure came from the API client
	Message   string
	Retryable bool
	Args      map[string]any // named values for the localized message (index, count, host, ...)
	Cause     error
}

// Error implements error. The whole text, including the text of the cause, is cleaned of control
// and format characters: server data reaches it through network and TLS errors (T-16 case 49).
func (e *Error) Error() string {
	msg := e.Code
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return SanitizeText(msg)
}

// Unwrap exposes the cause.
func (e *Error) Unwrap() error { return e.Cause }

// ExitCode maps the code to the process exit code of cli-output.md section 4.
func (e *Error) ExitCode() int { return ExitCodeFor(e.Code) }

// ExitCodeFor returns the exit code of an error code; unknown codes are internal errors.
func ExitCodeFor(code string) int {
	switch code {
	case CodeUsage, CodeInvalidConfig, CodeInvalidRequest, CodeConfirmationNeeded, CodeUnmappedPlatform:
		return exitUsage
	case CodeNotFound, CodeNoSession, CodeIndexOutOfRange:
		return exitNotFound
	case CodeNetwork, CodeRateLimited, CodeUpstream, CodeAPIIncompatible, CodeUntrustedURL, CodeUntrustedRedirect:
		return exitNetwork
	case string(proxy.CodeSizeMismatch), string(proxy.CodeIntegrity):
		return exitIntegrity
	case string(proxy.CodeDiskFull), string(proxy.CodePathRejected), string(proxy.CodeUnsafeName), string(proxy.CodeExists), "permission_denied":
		return exitFilesystem
	case CodeCanceled:
		return exitInterrupted
	}
	return exitInternal
}

// SanitizeText removes Cc and Cf runes (ANSI and OSC escapes, bidi controls, zero width) from
// text that may carry server data before it reaches a terminal, and bounds its length.
func SanitizeText(s string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
	if len(clean) > maxSanitizedErrorLength {
		clean = strings.ToValidUTF8(clean[:maxSanitizedErrorLength], "") + "..."
	}
	return clean
}

// wrap turns any error from a lower layer into an *Error. Errors that already are one pass.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	var me *mothdex.Error
	if errors.As(err, &me) {
		return &Error{Code: codeOfKind(me.Kind), Kind: me.Kind, Message: me.Kind.String(), Retryable: retryable(me.Kind), Cause: logSafeAPIError(me)}
	}
	var pe *proxy.Error
	if errors.As(err, &pe) {
		return &Error{Code: string(pe.Code), Message: SanitizeText(pe.Message), Cause: err}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeCanceled, Cause: err}
	}
	return &Error{Code: CodeInternal, Cause: err}
}

// maxUpstreamLogBytes bounds the upstream message that reaches the verbose cause line and the log.
const maxUpstreamLogBytes = 200

// logSafeAPIError returns a copy of the client error whose upstream message is sanitized and capped
// for the log. The message is never part of the localized text and never drives logic (T-77).
func logSafeAPIError(me *mothdex.Error) *mothdex.Error {
	cp := *me
	msg := SanitizeText(me.Message)
	if len(msg) > maxUpstreamLogBytes {
		msg = strings.ToValidUTF8(msg[:maxUpstreamLogBytes], "")
	}
	cp.Message = msg
	return &cp
}

func codeOfKind(k mothdex.Kind) string {
	switch k {
	case mothdex.KindNetwork:
		return CodeNetwork
	case mothdex.KindNotFound:
		return CodeNotFound
	case mothdex.KindInvalidRequest:
		return CodeInvalidRequest
	case mothdex.KindUpstream, mothdex.KindAPI:
		return CodeUpstream
	case mothdex.KindDecode, mothdex.KindIncompatible:
		return CodeAPIIncompatible
	case mothdex.KindRateLimited:
		return CodeRateLimited
	case mothdex.KindCanceled:
		return CodeCanceled
	case mothdex.KindUntrustedRedirect:
		return CodeUntrustedRedirect
	}
	return CodeInternal
}

func retryable(k mothdex.Kind) bool {
	return k == mothdex.KindNetwork || k == mothdex.KindRateLimited || k == mothdex.KindUpstream
}

func newError(code, msg string) *Error { return &Error{Code: code, Message: msg} }
