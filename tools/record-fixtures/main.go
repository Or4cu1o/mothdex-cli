// Command record-fixtures is a one-shot developer tool, not shipped. It sends
// at most ten polite requests to the Mothdex API v1 and stores the raw
// responses under pkg/mothdex/testdata/api-v1/, together with a manifest of
// statuses and rate-limit related headers (including whether Retry-After shows up).
//
// Usage: go run ./tools/record-fixtures [-out pkg/mothdex/testdata/api-v1] [-pause 3s]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const (
	baseURL         = mothdex.DefaultBaseURL
	maxBody         = 16 << 20
	reqTimeout      = 30 * time.Second
	missingSlug     = "this-slug-does-not-exist"
	slugPlaceholder = "__SLUG__"
)

// step is one request to record.
type step struct {
	Name   string
	Method string
	Path   string
	Body   string // empty for GET
}

// record is the manifest row of one fixture.
type record struct {
	File        string            `json:"file"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	RequestBody string            `json:"request_body,omitempty"`
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"` // content-type and rate-limit related only
}

type manifest struct {
	RecordedAt        string   `json:"recorded_at"`
	RetryAfterSeen    bool     `json:"retry_after_seen"`
	RetryAfterNote    string   `json:"retry_after_note"`
	RateLimitHeaders  []string `json:"rate_limit_headers_seen"`
	Fixtures          []record `json:"fixtures"`
	RequestsPerformed int      `json:"requests_performed"`
}

func main() {
	out := flag.String("out", "pkg/mothdex/testdata/api-v1", "output directory")
	pause := flag.Duration("pause", 3*time.Second, "sleep between requests")
	flag.Parse()
	if err := run(context.Background(), *out, *pause); err != nil {
		fmt.Fprintln(os.Stderr, "record-fixtures:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, outDir string, pause time.Duration) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	m := manifest{RecordedAt: time.Now().UTC().Format(time.RFC3339), RateLimitHeaders: []string{}}
	slug := missingSlug
	for i, s := range baseSteps() {
		if i > 0 {
			time.Sleep(pause)
		}
		s.Body = strings.ReplaceAll(s.Body, slugPlaceholder, slug)
		rec, hdrs, body, err := perform(ctx, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, rec.File), body, 0o600); err != nil {
			return err
		}
		m = m.absorb(rec, hdrs)
		fmt.Printf("%-18s %s %s -> %d\n", s.Name, s.Method, s.Path, rec.Status)
		if s.Name == "search" {
			slug = firstSlug(body, missingSlug)
		}
	}
	m.RequestsPerformed = len(m.Fixtures)
	m.RetryAfterNote = retryAfterNote(m.RetryAfterSeen)
	return writeManifest(filepath.Join(outDir, "manifest.json"), m)
}

func baseSteps() []step {
	return []step{
		{"info", http.MethodGet, "/info", ""},
		{"platforms", http.MethodGet, "/platforms", ""},
		{"regions", http.MethodGet, "/regions", ""},
		{"search", http.MethodPost, "/search", `{"search_key":"doom","platforms":["dos"],"max_results":3}`},
		{"search-empty", http.MethodPost, "/search", `{"search_key":"zzzqqq-no-such-title-xyz","max_results":3}`},
		{"entry", http.MethodPost, "/entry", `{"slug":"` + slugPlaceholder + `"}`},
		{"entry-not-found", http.MethodPost, "/entry", `{"slug":"` + missingSlug + `"}`},
		{"random", http.MethodPost, "/entry/random", `{"platforms":["gba"]}`},
		{"entry-empty-slug", http.MethodPost, "/entry", `{"slug":""}`},
		{"search-bad-json", http.MethodPost, "/search", `{`},
	}
}

// firstSlug returns the first slug of a recorded search body, or fallback.
func firstSlug(searchBody []byte, fallback string) string {
	var env struct {
		Data struct {
			Results []struct {
				Slug string `json:"slug"`
			} `json:"results"`
		} `json:"data"`
	}
	if json.Unmarshal(searchBody, &env) != nil || len(env.Data.Results) == 0 {
		return fallback
	}
	return env.Data.Results[0].Slug
}

func perform(ctx context.Context, s step) (record, http.Header, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	var rdr io.Reader
	if s.Body != "" {
		rdr = bytes.NewReader([]byte(s.Body))
	}
	req, err := http.NewRequestWithContext(ctx, s.Method, baseURL+s.Path, rdr)
	if err != nil {
		return record{}, nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", mothdex.DefaultUserAgent("fixture-recorder"))
	if s.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return record{}, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return record{}, nil, nil, err
	}
	rec := record{
		File: s.Name + ".json", Method: s.Method, Path: s.Path, RequestBody: s.Body,
		Status: resp.StatusCode, Headers: interestingHeaders(resp.Header),
	}
	return rec, resp.Header, body, nil
}

// interestingHeaders keeps Content-Type, Retry-After and any rate-limit header.
func interestingHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k := range h {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "retry-after" || strings.Contains(lk, "ratelimit") || strings.Contains(lk, "rate-limit") {
			out[lk] = h.Get(k)
		}
	}
	return out
}

// absorb returns a copy of m extended with one recorded response.
func (m manifest) absorb(rec record, h http.Header) manifest {
	out := m
	out.Fixtures = append(slices.Clone(m.Fixtures), rec)
	out.RetryAfterSeen = m.RetryAfterSeen || h.Get("Retry-After") != ""
	seen := slices.Clone(m.RateLimitHeaders)
	for k := range rec.Headers {
		if k != "content-type" && k != "retry-after" && !slices.Contains(seen, k) {
			seen = append(seen, k)
		}
	}
	slices.Sort(seen)
	out.RateLimitHeaders = seen
	return out
}

func retryAfterNote(seen bool) string {
	if seen {
		return "Retry-After appeared on at least one recorded response."
	}
	return "Retry-After did not appear on any recorded response. No 429 was provoked on purpose, so its presence on a 429 is not verified."
}

func writeManifest(path string, m manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
