package proxy

import "fmt"

// Code is the stable error code of a failed job (cli-output.md section 4). Tests assert codes,
// never message text.
type Code string

// Error codes of the downloader.
const (
	CodeUntrustedURL Code = "untrusted_url"
	CodeSizeMismatch Code = "size_mismatch"
	CodeIntegrity    Code = "integrity"
	CodeDiskFull     Code = "disk_full"
	CodePathRejected Code = "path_rejected"
	CodeUnsafeName   Code = "unsafe_name"
	CodeExists       Code = "exists"
	CodeUnmapped     Code = "unmapped_platform"
	CodeNetwork      Code = "network"
	CodeNotFound     Code = "not_found"
	CodeRateLimited  Code = "rate_limited"
	CodeUpstream     Code = "upstream"
	CodeCanceled     Code = "canceled"
	CodeInternal     Code = "internal"
)

// Error is the typed error of the downloader. Use errors.As and compare Code.
type Error struct {
	Code    Code
	Message string // untrusted text may be quoted here; sanitize before printing
	Err     error
}

// Error implements error.
func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause.
func (e *Error) Unwrap() error { return e.Err }
