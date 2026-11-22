package mothdex

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxBodyBytes   = 16 << 20 // 16 MiB
	maxRetryAfter  = 24 * time.Hour
	maxMessageSize = 512
)

// apiInfo keeps info.error and info.message raw so that a value that is not a string can be told
// apart from an absent one. info.code of the old forward-compat plan is no longer read.
type apiInfo struct {
	Error   json.RawMessage `json:"error"`
	Message json.RawMessage `json:"message"`
}

// text returns the string held by a raw JSON value; ok is false for anything that is not a string.
func text(raw json.RawMessage) (s string, ok bool) {
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// present reports whether a raw JSON value was sent and is not null.
func present(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

type envelope struct {
	Info apiInfo         `json:"info"`
	Data json.RawMessage `json:"data"`
}

// parseResponse applies the A6 rule: HTTP status first, then info.error.
// It returns the raw data object on success. It is pure: no I/O, no clock.
func parseResponse(op string, status int, header http.Header, body []byte, now time.Time) (json.RawMessage, *Error) {
	if status != http.StatusOK {
		return nil, statusError(op, status, header, body, now)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, &Error{Kind: KindDecode, Op: op, HTTPStatus: status, Message: "invalid JSON response", Err: err}
	}
	if present(env.Info.Error) {
		return nil, infoError(op, status, header, env.Info, now)
	}
	return env.Data, nil
}

// infoError builds the error for an HTTP 200 whose info.error is present (api-client 3). A code
// is matched exactly; any other string is the old message form; a value that is not a non-empty
// string is a decode failure. The message is for logs and the UI only, never for logic.
func infoError(op string, status int, header http.Header, info apiInfo, now time.Time) *Error {
	raw, ok := text(info.Error)
	if !ok || raw == "" {
		return &Error{Kind: KindDecode, Op: op, HTTPStatus: status, Message: "info.error is not a non-empty string"}
	}
	e := &Error{Op: op, HTTPStatus: status}
	if codePattern.MatchString(raw) {
		e.Code = raw
		e.Kind = kindOfCode(raw)
		e.Message = infoMessage(info, "")
	} else {
		e.Kind = kindOfMessage(raw)
		e.Message = infoMessage(info, raw)
	}
	if e.Kind == KindRateLimited {
		e.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	}
	return e
}

// infoMessage is info.message when it is a string, else the fallback (the old form's info.error).
func infoMessage(info apiInfo, fallback string) string {
	if m, ok := text(info.Message); ok {
		return cleanMessage(m)
	}
	return cleanMessage(fallback)
}

func statusError(op string, status int, header http.Header, body []byte, now time.Time) *Error {
	e := &Error{Kind: classifyStatus(status), Op: op, HTTPStatus: status, Message: http.StatusText(status)}
	if status == http.StatusBadRequest {
		code, msg := bestEffortInfo(body)
		e.Code = code
		if code == codeInvalidAPIVersion {
			e.Kind = KindIncompatible
		}
		if msg != "" {
			e.Message = msg
		}
	}
	if status == http.StatusTooManyRequests {
		e.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	}
	return e
}

// bestEffortInfo extracts the code and the message from an error body; failures are ignored. The
// message is info.message, or info.error when that is the old text form.
func bestEffortInfo(body []byte) (code, msg string) {
	var env envelope
	if json.Unmarshal(body, &env) != nil {
		return "", ""
	}
	raw, ok := text(env.Info.Error)
	if ok && codePattern.MatchString(raw) {
		return raw, infoMessage(env.Info, "")
	}
	if !ok {
		return "", ""
	}
	return "", infoMessage(env.Info, raw)
}

// parseRetryAfter reads delta-seconds or an HTTP date; unusable values give 0.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(min(secs, int64(maxRetryAfter/time.Second))) * time.Second
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0
	}
	return min(max(t.Sub(now), 0), maxRetryAfter)
}

// cleanMessage is defense in depth for server text that may reach a terminal: it drops Unicode
// control (Cc) and format (Cf) characters, then bounds the length. Only info.error goes through it;
// data fields stay raw and output sanitizing remains the job of the UI layer.
func cleanMessage(s string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return truncate(clean)
}

func truncate(s string) string {
	if len(s) <= maxMessageSize {
		return s
	}
	return strings.ToValidUTF8(s[:cutAtRune(s, maxMessageSize)], "")
}

// cutAtRune returns the largest index <= limit that does not split a UTF-8 rune.
func cutAtRune(s string, limit int) int {
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return limit
}
