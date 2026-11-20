package config

import (
	"fmt"
	"net/url"
)

// ErrorKind classifies a configuration failure; every kind maps to exit code 2.
type ErrorKind int

// Error kinds.
const (
	KindInvalidValue ErrorKind = iota + 1 // type or range error
	KindUnknownKey                        // key not in the schema
	KindUnreadable                        // file cannot be read, parsed or written
	KindNewerSchema                       // file version is higher than SchemaVersion
)

// Error is a configuration error carrying the key, the offending value and its source.
type Error struct {
	Kind   ErrorKind
	Key    string
	Value  string
	Source Source // "" when the value came from a set command
	Reason string
	Err    error
}

// Code returns the stable error code of cli-output.md.
func (e *Error) Code() string {
	if e.Kind == KindUnknownKey {
		return "usage"
	}
	return "invalid_config"
}

// ExitCode is always 2: usage or configuration error.
func (e *Error) ExitCode() int { return 2 }

// Error implements error.
func (e *Error) Error() string {
	msg := "config"
	if e.Key != "" {
		msg += " " + e.Key
	}
	if e.Value != "" {
		msg += fmt.Sprintf(" = %q", e.Value)
	}
	if e.Source != "" {
		msg += fmt.Sprintf(" (from %s)", e.Source)
	}
	return msg + ": " + e.Reason
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

// valueError builds the error for a rejected value. It is the single place that does so, for
// Load and Set alike, so that the redaction below cannot be skipped by one path.
func valueError(key string, raw any, src Source, cause error) *Error {
	return &Error{Kind: KindInvalidValue, Key: key, Value: displayValue(key, raw), Source: src, Reason: cause.Error(), Err: cause}
}

const unparseableURL = "<unparseable URL>"

// displayValue renders a rejected value for an error message. A base URL may carry a secret in
// its userinfo, query, fragment, opaque part or even its path, so it is never echoed as given:
// only scheme://host[:port] is shown, and anything without both becomes a placeholder.
func displayValue(key string, raw any) string {
	text := fmt.Sprint(raw)
	if key != "api.base_url" {
		return text
	}
	u, err := url.Parse(text)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return unparseableURL
	}
	return u.Scheme + "://" + u.Host
}
