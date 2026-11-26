package log

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const (
	redacted  = "<redacted>"
	apiMarker = "<api>"
	// minSecretLen keeps a degenerate secret (an empty or one-letter host) from eating every line.
	minSecretLen = 4
	defaultAPI   = "https://api.mothdex.net/v1"
)

var (
	urlPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"<>\\` + "`" + `]+`)
	// A proxy setting written without a scheme still carries user and password.
	userinfoPattern = regexp.MustCompile(`[^\s/\\"'=@:(\[]+:[^\s/\\"'@]+@([A-Za-z0-9.\-]+)`)
	// "Name: value" and "Name=value" forms of the headers that carry credentials; the value runs to
	// the end of the line, quote or bracket.
	// A secret written as key=value outside a URL, as in a raw server message.
	pairPattern   = regexp.MustCompile(`(?i)\b([a-z0-9_\-]*(?:token|signature|password|passwd|secret|api_?key|x-amz-credential)|sig)=[^\s&"'\\<]+`)
	headerPattern = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization|set-cookie|cookie|x-[a-z0-9-]*(?:token|key|secret|auth)[a-z0-9-]*)(\s*[:=]\s*\[?)[^\r\n"\]\\]+`)
)

// redactor is the single redaction point in front of every sink (TM-16). Secrets can be added at
// any time: the API URL is only known once the configuration is read.
type redactor struct {
	mu      sync.RWMutex
	hosts   []string // lower case; a URL on one of these hosts becomes <api>
	secrets []*regexp.Regexp
}

func newRedactor(secrets ...string) *redactor {
	r := &redactor{}
	r.add(secrets...)
	return r
}

// add registers values that must never be written. A URL also registers its host, with and
// without the port.
func (r *redactor) add(secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range secrets {
		values := []string{s}
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			r.hosts = append(r.hosts, strings.ToLower(u.Hostname()))
			values = append(values, u.Host, u.Hostname())
		}
		for _, v := range values {
			if len(v) >= minSecretLen {
				r.secrets = append(r.secrets, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(v)))
			}
		}
	}
}

// text returns s with every credential, API address and URL secret removed.
func (r *redactor) text(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s = headerPattern.ReplaceAllString(s, "${1}${2}"+redacted)
	s = urlPattern.ReplaceAllStringFunc(s, r.url)
	s = userinfoPattern.ReplaceAllString(s, redacted+"@${1}")
	s = pairPattern.ReplaceAllString(s, "${1}="+redacted)
	for _, re := range r.secrets {
		s = re.ReplaceAllLiteralString(s, apiMarker)
	}
	return s
}

// url rewrites one URL found in a text; the caller holds the lock.
func (r *redactor) url(raw string) string {
	trimmed := strings.TrimRight(raw, ".,;:)]}'")
	tail := raw[len(trimmed):]
	if strings.HasPrefix(strings.ToLower(trimmed), "file://") {
		return raw // a local path: the bundle removes the home folder from it
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "<url>" + tail
	}
	if slices.Contains(r.hosts, strings.ToLower(u.Hostname())) {
		return apiMarker + tail
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String() + tail
}

// sensitiveKey reports an attribute whose value is a credential by its name.
func sensitiveKey(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"authorization", "cookie", "token", "secret", "password", "api-key", "api_key", "apikey", "auth"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func (r *redactor) attr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if sensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, r.text(a.Value.String()))
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, g := range group {
			out[i] = r.attr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case http.Header, *http.Request, *http.Response, []byte:
			return slog.String(a.Key, redacted) // never full header sets or bodies
		case error:
			return slog.String(a.Key, r.text(v.Error()))
		default:
			return slog.String(a.Key, r.text(fmt.Sprintf("%+v", v)))
		}
	default:
		return a // numbers, booleans, times and durations carry no text
	}
}

// redactHandler cleans every record before the wrapped handler formats it. Attributes and groups
// bound with With are applied per record, so a secret registered later still covers them.
type redactHandler struct {
	next slog.Handler
	r    *redactor
	with []func(slog.Handler) slog.Handler
}

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	next := h.next
	for _, w := range h.with {
		next = w(next)
	}
	out := slog.NewRecord(rec.Time, rec.Level, h.r.text(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.attr(a))
		return true
	})
	return next.Handle(ctx, out)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.with = append(slices.Clone(h.with), func(next slog.Handler) slog.Handler {
		out := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			out[i] = h.r.attr(a)
		}
		return next.WithAttrs(out)
	})
	return h
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	h.with = append(slices.Clone(h.with), func(next slog.Handler) slog.Handler { return next.WithGroup(name) })
	return h
}
