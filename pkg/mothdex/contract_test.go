package mothdex

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
)

func TestSearch_Fixtures(t *testing.T) {
	var sent []byte
	h := newHarness(t, serveFixture(t, "search", &sent))
	got, err := h.client.Search(context.Background(), SearchRequest{
		Filters:    Filters{SearchKey: "doom", Platforms: []string{"dos"}},
		MaxResults: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Results) != 3 || got.CurrentResults != 3 || got.CurrentPage != 1 || got.TotalPages < 1 {
		t.Fatalf("unexpected response: results=%d %+v", len(got.Results), got)
	}
	first := got.Results[0]
	if first.Slug != "doom-1993-dos" || first.Serial != "" || first.Regions == nil || len(first.Links) == 0 {
		t.Fatalf("unexpected first entry: %+v", first)
	}
	if first.Links[0].Size <= 0 || first.Links[0].Filename == "" || first.Links[0].Hashes != nil {
		t.Fatalf("unexpected first link: %+v", first.Links[0])
	}
	var body map[string]any
	if err := json.Unmarshal(sent, &body); err != nil || body["search_key"] != "doom" || body["max_results"] != float64(3) {
		t.Fatalf("request body %s (err %v)", sent, err)
	}
}

func TestSearch_EmptyIsReturnedRaw(t *testing.T) {
	h := newHarness(t, serveFixture(t, "search-empty", nil))
	got, err := h.client.Search(context.Background(), SearchRequest{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Results == nil || len(got.Results) != 0 || got.CurrentPage != 0 || got.TotalPages != 0 {
		t.Fatalf("want empty results with raw page 0 of 0, got %+v", got)
	}
}

func TestEntry_FixtureAndRequestBody(t *testing.T) {
	var sent []byte
	h := newHarness(t, serveFixture(t, "entry", &sent))
	got, err := h.client.Entry(context.Background(), "doom-1993-dos")
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Slug != "doom-1993-dos" || got.Platform != "dos" || len(got.Links) == 0 {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if string(sent) != `{"slug":"doom-1993-dos"}` {
		t.Fatalf("request body %s", sent)
	}
}

func TestRandom_FixtureNullsBecomeEmpty(t *testing.T) {
	var sent []byte
	h := newHarness(t, serveFixture(t, "random", &sent))
	got, err := h.client.Random(context.Background(), Filters{Platforms: []string{"gba"}})
	if err != nil {
		t.Fatalf("Random: %v", err)
	}
	if got.Serial != "" || got.CoverURL != "" || got.Links[0].Extras == nil {
		t.Fatalf("nulls not normalized: %+v", got)
	}
	if string(sent) != `{"platforms":["gba"]}` {
		t.Fatalf("request body %s", sent)
	}
}

func TestRandom_EmptyFiltersSendEmptyObject(t *testing.T) {
	var sent []byte
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveFixture(t, "random", &sent).ServeHTTP(w, r)
	}))
	if _, err := h.client.Random(context.Background(), Filters{}); err != nil {
		t.Fatalf("Random: %v", err)
	}
	if string(sent) != "{}" {
		t.Fatalf("want {}, got %s", sent)
	}
}

func TestPlatforms_SortedWithIDFromKey(t *testing.T) {
	h := newHarness(t, serveFixture(t, "platforms", nil))
	got, err := h.client.Platforms(context.Background())
	if err != nil {
		t.Fatalf("Platforms: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("too few platforms: %d", len(got))
	}
	for i, p := range got {
		if p.ID == "" || p.Name == "" || (i > 0 && got[i-1].ID >= p.ID) {
			t.Fatalf("platform %d not sorted/complete: %+v", i, p)
		}
	}
}

func TestRegions_Sorted(t *testing.T) {
	h := newHarness(t, serveFixture(t, "regions", nil))
	got, err := h.client.Regions(context.Background())
	if err != nil {
		t.Fatalf("Regions: %v", err)
	}
	want := []Region{{"eu", "Europe"}, {"jp", "Japan"}, {"other", "Other"}, {"us", "USA"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestInfo_Fixture(t *testing.T) {
	h := newHarness(t, serveFixture(t, "info", nil))
	got, err := h.client.Info(context.Background())
	if err != nil || got.TotalEntries <= 0 {
		t.Fatalf("Info = %+v, %v", got, err)
	}
}

func TestEntry_NotFoundIsHTTP200WithInfoError(t *testing.T) {
	if st := loadManifest(t)["entry-not-found"].Status; st != http.StatusOK {
		t.Fatalf("fixture status %d: the API contract says 200", st)
	}
	h := newHarness(t, serveFixture(t, "entry-not-found", nil))
	_, err := h.client.Entry(context.Background(), "this-slug-does-not-exist")
	if !IsNotFound(err) {
		t.Fatalf("want KindNotFound, got %v", err)
	}
	if h.hits.Load() != 1 {
		t.Fatalf("not found must not be retried, hits=%d", h.hits.Load())
	}
}

func TestSearch_Recorded400IsInvalidRequest(t *testing.T) {
	h := newHarness(t, serveFixture(t, "search-bad-json", nil))
	_, err := h.client.Search(context.Background(), SearchRequest{})
	var e *Error
	if !asError(err, &e) || e.Kind != KindInvalidRequest || e.HTTPStatus != 400 || e.Message == "" {
		t.Fatalf("want invalid_request 400 with message, got %#v", err)
	}
	if h.hits.Load() != 1 {
		t.Fatalf("400 must not be retried, hits=%d", h.hits.Load())
	}
}

// Recorded error bodies, classified by the pure rule (parametrized over the fixtures).
func TestClassifyRecordedInfoErrors(t *testing.T) {
	tests := []struct {
		fixture string
		want    Kind
	}{
		{"entry-not-found", KindNotFound},
		{"entry-empty-slug", KindInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			_, e := parseResponse("op", 200, http.Header{}, fixtureBody(t, tt.fixture), newFakeClock().Now())
			if e == nil || e.Kind != tt.want {
				t.Fatalf("got %#v, want kind %v", e, tt.want)
			}
		})
	}
}

func TestRequestHeaders(t *testing.T) {
	var gotUA, gotCT, gotAccept string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotCT, gotAccept = r.Header.Get("User-Agent"), r.Header.Get("Content-Type"), r.Header.Get("Accept")
		serveFixture(t, "info", nil).ServeHTTP(w, r)
	}))
	if _, err := h.client.Info(context.Background()); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !regexp.MustCompile(`^mothdex-cli/dev \(\+https://github\.com/Or4cu1o/mothdex-cli\)$`).MatchString(gotUA) {
		t.Fatalf("default User-Agent %q", gotUA)
	}
	if gotCT != "" || gotAccept != "application/json" {
		t.Fatalf("GET headers: Content-Type=%q Accept=%q", gotCT, gotAccept)
	}
}

func TestUserAgentOption(t *testing.T) {
	var gotUA string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		serveFixture(t, "info", nil).ServeHTTP(w, r)
	}), WithUserAgent(DefaultUserAgent("1.2.3")))
	if _, err := h.client.Info(context.Background()); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if gotUA != "mothdex-cli/1.2.3 (+https://github.com/Or4cu1o/mothdex-cli)" {
		t.Fatalf("User-Agent %q", gotUA)
	}
}

func TestLocalValidationSendsNothing(t *testing.T) {
	h := newHarness(t, serveFixture(t, "info", nil))
	ctx := context.Background()
	long := make([]byte, 257)
	for i := range long {
		long[i] = 'a'
	}
	many := make([]string, 51)
	calls := map[string]func() error{
		"empty slug":      func() error { _, err := h.client.Entry(ctx, ""); return err },
		"long slug":       func() error { _, err := h.client.Entry(ctx, string(long)); return err },
		"max_results 101": func() error { _, err := h.client.Search(ctx, SearchRequest{MaxResults: 101}); return err },
		"max_results -1":  func() error { _, err := h.client.Search(ctx, SearchRequest{MaxResults: -1}); return err },
		"page -1":         func() error { _, err := h.client.Search(ctx, SearchRequest{Page: -1}); return err },
		"51 platforms": func() error {
			_, err := h.client.Search(ctx, SearchRequest{Filters: Filters{Platforms: many}})
			return err
		},
		"51 regions": func() error { _, err := h.client.Random(ctx, Filters{Regions: many}); return err },
	}
	for name, call := range calls {
		if err := call(); !IsInvalidRequest(err) {
			t.Errorf("%s: want invalid_request, got %v", name, err)
		}
	}
	if h.hits.Load() != 0 || len(h.sleeper.Waits()) != 0 {
		t.Fatalf("validation must not touch the network or the limiter (hits=%d)", h.hits.Load())
	}
}

func TestDecodeErrors(t *testing.T) {
	calls := map[string]func(*Client) error{
		"search":    func(c *Client) error { _, err := c.Search(context.Background(), SearchRequest{}); return err },
		"entry":     func(c *Client) error { _, err := c.Entry(context.Background(), "x"); return err },
		"random":    func(c *Client) error { _, err := c.Random(context.Background(), Filters{}); return err },
		"platforms": func(c *Client) error { _, err := c.Platforms(context.Background()); return err },
		"regions":   func(c *Client) error { _, err := c.Regions(context.Background()); return err },
		"info":      func(c *Client) error { _, err := c.Info(context.Background()); return err },
	}
	bodies := map[string]string{
		"missing key":  `{"info":{},"data":{}}`,
		"invalid json": `<html>not json</html>`,
		"wrong type":   `{"info":{},"data":[1,2]}`,
	}
	for cname, call := range calls {
		for bname, body := range bodies {
			t.Run(cname+"/"+bname, func(t *testing.T) {
				h := newHarness(t, staticHandler(200, nil, body))
				var e *Error
				if err := call(h.client); !asError(err, &e) || e.Kind != KindDecode {
					t.Fatalf("want KindDecode, got %v", err)
				}
				if h.hits.Load() != 1 {
					t.Fatalf("decode errors are not retried, hits=%d", h.hits.Load())
				}
			})
		}
	}
}

func TestResponseBodyCap(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxBodyBytes+1))
	}))
	var e *Error
	if _, err := h.client.Info(context.Background()); !asError(err, &e) || e.Kind != KindDecode {
		t.Fatalf("want KindDecode for oversized body, got %v", err)
	}
}

func TestForwardCompatibleDecode(t *testing.T) {
	body := `{"info":{},"data":{"entry":{"slug":"s","title":"T","platform":"p","regions":null,"serial":null,` +
		`"cover_url":null,"links":[{"type":"Game","extras":null,"filename":"f.zip","url":"u","size":5,` +
		`"future":"x","hashes":{"crc32":"ABCD","scope":"file"}}],"new_field":1}}}`
	h := newHarness(t, staticHandler(200, nil, body))
	got, err := h.client.Entry(context.Background(), "s")
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	l := got.Links[0]
	if got.Regions == nil || l.Extras == nil || l.Hashes == nil || l.Hashes.CRC32 != "abcd" {
		t.Fatalf("unexpected normalization: %+v", got)
	}
}
