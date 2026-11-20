package mothdex

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyBytes   = 16 << 20 // 16 MiB
	maxRetryAfter  = 24 * time.Hour
	maxMessageSize = 200
)

type apiInfo struct {
	Error string `json:"error"`
	Code  string `json:"code"`
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
	if env.Info.Error != "" || env.Info.Code != "" {
		return nil, &Error{
			Kind:       classifyInfoError(env.Info),
			Op:         op,
			HTTPStatus: status,
			Message:    truncate(env.Info.Error),
		}
	}
	return env.Data, nil
}

func statusError(op string, status int, header http.Header, body []byte, now time.Time) *Error {
	e := &Error{Kind: classifyStatus(status), Op: op, HTTPStatus: status, Message: http.StatusText(status)}
	if status == http.StatusBadRequest {
		if msg := bestEffortMessage(body); msg != "" {
			e.Message = msg
		}
	}
	if status == http.StatusTooManyRequests {
		e.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	}
	return e
}

// bestEffortMessage extracts info.error from an error body; failures are ignored.
func bestEffortMessage(body []byte) string {
	var env envelope
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	return truncate(env.Info.Error)
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
