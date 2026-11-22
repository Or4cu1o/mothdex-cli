package mothdex

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPlanRetry(t *testing.T) {
	up := &Error{Kind: KindUpstream}
	net := &Error{Kind: KindNetwork}
	tests := []struct {
		name    string
		err     *Error
		st      retryState
		max     int
		open    bool
		want    bool
		wait    time.Duration
		wantNxt retryState
	}{
		{"upstream first retry", up, retryState{}, 2, false, true, 500 * time.Millisecond, retryState{backoff: 1}},
		{"network second retry", net, retryState{backoff: 1}, 2, false, true, time.Second, retryState{backoff: 2}},
		{"budget spent", up, retryState{backoff: 2}, 2, false, false, 0, retryState{backoff: 2}},
		{"backoff capped at 8s", up, retryState{backoff: 9}, 10, false, true, 8 * time.Second, retryState{backoff: 10}},
		{"429 default wait", &Error{Kind: KindRateLimited}, retryState{}, 2, false, true, 2 * time.Second, retryState{rateLimit: 1}},
		{"429 honors Retry-After", &Error{Kind: KindRateLimited, RetryAfter: 9 * time.Second}, retryState{}, 2, false, true, 9 * time.Second, retryState{rateLimit: 1}},
		{"429 only one retry", &Error{Kind: KindRateLimited}, retryState{rateLimit: 1}, 2, false, false, 0, retryState{rateLimit: 1}},
		{"429 with open circuit", &Error{Kind: KindRateLimited}, retryState{}, 2, true, false, 0, retryState{}},
		{"429 Retry-After too long", &Error{Kind: KindRateLimited, RetryAfter: time.Hour}, retryState{}, 2, false, false, 0, retryState{}},
		{"429 keeps backoff budget", &Error{Kind: KindRateLimited}, retryState{backoff: 2}, 2, false, true, 2 * time.Second, retryState{backoff: 2, rateLimit: 1}},
		{"not found", &Error{Kind: KindNotFound}, retryState{}, 2, false, false, 0, retryState{}},
		{"invalid request", &Error{Kind: KindInvalidRequest}, retryState{}, 2, false, false, 0, retryState{}},
		{"decode", &Error{Kind: KindDecode}, retryState{}, 2, false, false, 0, retryState{}},
		{"canceled", &Error{Kind: KindCanceled}, retryState{}, 2, false, false, 0, retryState{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planRetry(tt.err, tt.st, tt.max, tt.open, maxJitter)
			if got.Retry != tt.want || got.Wait != tt.wait || got.Next != tt.wantNxt {
				t.Fatalf("got %+v, want retry=%v wait=%v next=%+v", got, tt.want, tt.wait, tt.wantNxt)
			}
		})
	}
}

func TestBackoffCeiling(t *testing.T) {
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for n, w := range want {
		if got := backoffCeiling(n); got != w {
			t.Errorf("n=%d: %v, want %v", n, got, w)
		}
	}
}

func TestBreakerRule(t *testing.T) {
	t0 := newFakeClock().Now()
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	tests := []struct {
		name   string
		hits   []int // minutes of the 429s
		opened bool  // after the last one
	}{
		{"two inside the window", []int{0, 1}, false},
		{"third inside the window opens", []int{0, 4, 9}, true},
		{"third just past the window", []int{0, 5, 10}, false},
		{"spread out hits never open", []int{0, 6, 12, 18, 24}, false},
		{"fifth after reopening count restarts", []int{0, 1, 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBreaker(time.Time{})
			var opened bool
			var until time.Time
			for _, m := range tt.hits {
				until, opened = b.record(at(m))
			}
			if opened != tt.opened {
				t.Fatalf("opened=%v, want %v", opened, tt.opened)
			}
			last := at(tt.hits[len(tt.hits)-1])
			if tt.opened && !until.Equal(last.Add(breachCooldown)) {
				t.Fatalf("until %v", until)
			}
			if got := b.remaining(last); tt.opened != (got > 0) {
				t.Fatalf("remaining %v with opened=%v", got, tt.opened)
			}
		})
	}
}

func TestBreakerRecloses(t *testing.T) {
	t0 := newFakeClock().Now()
	b := newBreaker(t0.Add(time.Minute))
	if b.remaining(t0) != time.Minute || b.remaining(t0.Add(2*time.Minute)) != 0 {
		t.Fatal("seeded cooldown not honored")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := newFakeClock().Now()
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0}, {"7", 7 * time.Second}, {" 30 ", 30 * time.Second}, {"0", 0}, {"-5", 0}, {"abc", 0},
		{"99999999999999", 24 * time.Hour},
		{now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second},
		{now.Add(-time.Hour).UTC().Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("%q: %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestClassifyResponses(t *testing.T) {
	now := newFakeClock().Now()
	info := func(msg string) string { return `{"info":{"error":"` + msg + `"},"data":{}}` }
	tests := []struct {
		name   string
		status int
		body   string
		want   Kind
	}{
		{"entry not found", 200, info("Entry not found"), KindNotFound},
		{"slug required", 200, info("Slug is required"), KindInvalidRequest},
		{"invalid input", 200, info("Invalid input provided"), KindInvalidRequest},
		{"db operation", 200, info("Database operation failed"), KindUpstream},
		{"db error", 200, info("A database error occurred"), KindUpstream},
		{"unexpected", 200, info("An unexpected error occurred"), KindUpstream},
		{"unknown message", 200, info("Something new"), KindAPI},
		{"old code field is ignored", 200, `{"info":{"code":"entry_not_found"},"data":{}}`, 0},
		{"400 any body", 400, "garbage", KindInvalidRequest},
		{"400 with info", 400, info("Malformed JSON in request body"), KindInvalidRequest},
		{"404", 404, "", KindIncompatible},
		{"429", 429, "", KindRateLimited},
		{"500", 500, "", KindUpstream},
		{"502", 502, "", KindUpstream},
		{"401 unexpected", 401, "", KindUpstream},
		{"200 invalid json", 200, "nope", KindDecode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, e := parseResponse("op", tt.status, http.Header{}, []byte(tt.body), now)
			if tt.want == 0 {
				if e != nil {
					t.Fatalf("got %#v, want success", e)
				}
				return
			}
			if e == nil || e.Kind != tt.want || e.HTTPStatus != tt.status {
				t.Fatalf("got %#v, want %v", e, tt.want)
			}
		})
	}
}

func TestParseResponseOKReturnsData(t *testing.T) {
	data, e := parseResponse("op", 200, http.Header{}, []byte(`{"info":{},"data":{"a":1}}`), time.Time{})
	if e != nil || string(data) != `{"a":1}` {
		t.Fatalf("data=%s err=%v", data, e)
	}
}

func TestNormalizeHashes(t *testing.T) {
	if normalizeHashes(nil) != nil || normalizeHashes(&Hashes{CRC32: "AB", Scope: "weird"}) != nil {
		t.Fatal("nil and unknown scope must give nil")
	}
	in := &Hashes{CRC32: "AB", MD5: "CD", SHA1: "EF", Scope: HashScopeContent}
	got := normalizeHashes(in)
	if got.CRC32 != "ab" || got.MD5 != "cd" || got.SHA1 != "ef" || in.CRC32 != "AB" {
		t.Fatalf("got %+v (input mutated: %+v)", got, in)
	}
}

func TestErrorHelpers(t *testing.T) {
	e := &Error{Kind: KindRateLimited, Op: "search", HTTPStatus: 429, Message: "slow"}
	if !IsRateLimited(e) || IsNotFound(e) || e.Error() != "mothdex search: rate_limited (HTTP 429): slow" {
		t.Fatalf("helpers wrong: %q", e.Error())
	}
	if KindCanceled.String() != "canceled" || Kind(0).String() != "unknown" {
		t.Fatal("Kind.String")
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	in := strings.Repeat("a", maxMessageSize-1) + "é" + "tail" // the rune straddles the limit
	got := truncate(in)
	if !utf8.ValidString(got) || len(got) > maxMessageSize {
		t.Fatalf("invalid or too long: %d bytes valid=%v", len(got), utf8.ValidString(got))
	}
	if short := truncate("ok"); short != "ok" {
		t.Fatalf("short string changed: %q", short)
	}
}

func TestCleanMessage(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "Entry not found", "Entry not found"},
		{"ansi escape", "bad\x1b[31m red\x1b[0m", "bad[31m red[0m"},
		{"newline and tab", "a\nb\tc\r", "abc"},
		{"nul and del", "a\x00b\x7fc", "abc"},
		{"c1 control", "a\u009bb", "ab"},
		{"bidi override", "a\u202eb\u2066c", "abc"},
		{"zero width and bom", "a\u200bb\ufeffc", "abc"},
		{"keeps accents and cjk", "Não é 日本", "Não é 日本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanMessage(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoErrorMessageIsCleaned(t *testing.T) {
	body := []byte(`{"info":{"error":"Entry not found\u001b[2J"},"data":{}}`)
	_, e := parseResponse("op", 200, http.Header{}, body, time.Time{})
	if e == nil || e.Kind != KindAPI || e.Message != "Entry not found[2J" {
		t.Fatalf("got %#v", e)
	}
}

func TestValidSlug(t *testing.T) {
	long := strings.Repeat("a", 256)
	for s, want := range map[string]bool{
		"super-mario-world": true, "1942": true, "a": true, long: true, long + "a": false,
		"": false, "-a": false, "A": false, "a_b": false, "a b": false, "../x": false, "a/b": false,
		"é": false, "a\u202eb": false, "a\n": false, "a,b": false,
	} {
		if got := ValidSlug(s); got != want {
			t.Errorf("ValidSlug(%q) = %v, want %v", s, got, want)
		}
	}
}

// api-client section 2: Entry refuses an unsafe slug before any request.
func TestEntryRefusesAnUnsafeSlugBeforeAnyRequest(t *testing.T) {
	h := newHarness(t, serveFixture(t, "entry-not-found", nil))
	for _, slug := range []string{"../x", "Caps", "a b", "-a"} {
		_, err := h.client.Entry(context.Background(), slug)
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindInvalidRequest {
			t.Errorf("Entry(%q) = %v, want KindInvalidRequest", slug, err)
		}
	}
	if h.hits.Load() != 0 {
		t.Fatalf("an unsafe slug reached the server: %d requests", h.hits.Load())
	}
}

// T-77: upstream cavv-dev/mothdex-api 7a87af9 sends info.error as a CODE and info.message as text.
func TestUpstreamErrorCodes(t *testing.T) {
	now := newFakeClock().Now()
	tests := []struct {
		code string
		want Kind
	}{
		{"ENTRY_NOT_FOUND", KindNotFound},
		{"INVALID_API_VERSION", KindIncompatible},
		{"MALFORMED_JSON", KindInvalidRequest},
		{"INVALID_JSON_OBJECT", KindInvalidRequest},
		{"MISSING_REQUIRED_FIELDS", KindInvalidRequest},
		{"INVALID_FIELD_TYPE", KindInvalidRequest},
		{"INVALID_INPUT", KindInvalidRequest},
		{"DB_OPERATION_ERROR", KindUpstream},
		{"DB_ERROR", KindUpstream},
		{"UNEXPECTED_ERROR", KindUpstream},
		{"RATE_LIMIT_ERROR", KindRateLimited},
		{"HTTP_ERROR", KindAPI},
		{"SOMETHING_NEW", KindAPI},
		{"entry_not_found", KindAPI}, // not code-shaped: the old message table, which does not know it
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			body := `{"info":{"error":"` + tt.code + `","message":"Human text"},"data":{}}`
			_, e := parseResponse("op", 200, http.Header{}, []byte(body), now)
			if e == nil || e.Kind != tt.want || e.HTTPStatus != 200 || e.Message != "Human text" {
				t.Fatalf("got %#v, want %v", e, tt.want)
			}
			if wantCode := tt.code; tt.code != "entry_not_found" && e.Code != wantCode {
				t.Fatalf("Code = %q, want %q", e.Code, wantCode)
			}
		})
	}
}

func TestOldMessageFormKeepsWorking(t *testing.T) {
	_, e := parseResponse("op", 200, http.Header{}, []byte(`{"info":{"error":"Entry not found"},"data":{}}`), time.Time{})
	if e == nil || e.Kind != KindNotFound || e.Code != "" || e.Message != "Entry not found" {
		t.Fatalf("got %#v", e)
	}
}

func TestInfoErrorThatIsNotAString(t *testing.T) {
	for _, in := range []string{`{"info":{"error":42},"data":{}}`, `{"info":{"error":{"a":1}},"data":{}}`, `{"info":{"error":""},"data":{}}`, `{"info":{"error":["X"]},"data":{}}`} {
		if _, e := parseResponse("op", 200, http.Header{}, []byte(in), time.Time{}); e == nil || e.Kind != KindDecode {
			t.Errorf("%s: got %#v, want decode", in, e)
		}
	}
	// A null or absent info.error is a success, and a non-string info.message never breaks a code.
	if _, e := parseResponse("op", 200, http.Header{}, []byte(`{"info":{"error":null},"data":{}}`), time.Time{}); e != nil {
		t.Errorf("null error: %#v", e)
	}
	_, e := parseResponse("op", 200, http.Header{}, []byte(`{"info":{"error":"DB_ERROR","message":{"x":1}},"data":{}}`), time.Time{})
	if e == nil || e.Kind != KindUpstream || e.Message != "" {
		t.Errorf("object message: %#v", e)
	}
}

func TestNonOKStatusWithACode(t *testing.T) {
	now := newFakeClock().Now()
	cases := []struct {
		status int
		body   string
		want   Kind
		code   string
	}{
		{400, `{"info":{"error":"INVALID_API_VERSION","message":"bad version"},"data":{}}`, KindIncompatible, "INVALID_API_VERSION"},
		{400, `{"info":{"error":"MALFORMED_JSON","message":"bad json"},"data":{}}`, KindInvalidRequest, "MALFORMED_JSON"},
		{400, `{"info":{"error":"ENTRY_NOT_FOUND"},"data":{}}`, KindInvalidRequest, "ENTRY_NOT_FOUND"}, // status decides, the code is only recorded
		{500, `{"info":{"error":"UNEXPECTED_ERROR","message":"boom"},"data":{}}`, KindUpstream, ""},
		{429, `{"info":{"error":"RATE_LIMIT_ERROR"},"data":{}}`, KindRateLimited, ""},
	}
	for _, c := range cases {
		_, e := parseResponse("op", c.status, http.Header{}, []byte(c.body), now)
		if e == nil || e.Kind != c.want || e.Code != c.code {
			t.Errorf("%d %s: got %#v, want %v code %q", c.status, c.body, e, c.want, c.code)
		}
	}
}

func TestRateLimitCodeAtStatus200ReadsRetryAfter(t *testing.T) {
	h := http.Header{"Retry-After": []string{"7"}}
	_, e := parseResponse("op", 200, h, []byte(`{"info":{"error":"RATE_LIMIT_ERROR","message":"slow"},"data":{}}`), newFakeClock().Now())
	if e == nil || e.Kind != KindRateLimited || e.RetryAfter != 7*time.Second {
		t.Fatalf("got %#v", e)
	}
}

func TestUpstreamMessageIsSanitizedAndCapped(t *testing.T) {
	long := strings.Repeat("é", 400) // 800 bytes
	body := `{"info":{"error":"INVALID_INPUT","message":"a\u001b[31mb\u202ec\ufffd` + long + `"},"data":{}}`
	_, e := parseResponse("op", 200, http.Header{}, []byte(body), time.Time{})
	if e == nil || len(e.Message) > maxMessageSize || !utf8.ValidString(e.Message) || strings.ContainsAny(e.Message, "\x1b\u202e") {
		t.Fatalf("got %q", e.Message)
	}
	if !strings.HasPrefix(e.Message, "a[31mbc") {
		t.Fatalf("prefix lost: %q", e.Message)
	}
	// Invalid UTF-8 in the raw body is replaced, never passed on.
	_, e = parseResponse("op", 200, http.Header{}, []byte("{\"info\":{\"error\":\"DB_ERROR\",\"message\":\"x\xffy\"},\"data\":{}}"), time.Time{})
	if e == nil || !utf8.ValidString(e.Message) {
		t.Fatalf("got %#v", e)
	}
}
