package mothdex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Client talks to the Mothdex API v1. It is safe for concurrent use: every
// field except the breaker and the limiter is immutable after New.
type Client struct {
	cfg     config
	base    *url.URL
	http    *http.Client
	limiter *rate.Limiter
	breaker *breaker
}

// New builds a Client from the defaults and the given options.
func New(opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	base, err := parseBaseURL(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	if cfg.timeout <= 0 {
		return nil, fmt.Errorf("mothdex: timeout must be positive, got %s", cfg.timeout)
	}
	hc := cfg.httpClient
	if hc == nil {
		hc = newDefaultHTTPClient()
	}
	return &Client{
		cfg:     cfg,
		base:    base,
		http:    hc,
		limiter: rate.NewLimiter(rate.Limit(cfg.rate), cfg.burst),
		breaker: newBreaker(cfg.cooldown),
	}, nil
}

// call runs one logical request: attempts, retries, breaker, rate limit.
// It returns the raw "data" object or an *Error.
func (c *Client) call(ctx context.Context, op, method, path string, body any) (json.RawMessage, error) {
	payload, err := encodeBody(op, method, body)
	if err != nil {
		return nil, err
	}
	var st retryState
	for {
		data, e := c.attempt(ctx, op, method, path, payload)
		if e == nil {
			return data, nil
		}
		plan := planRetry(e, st, c.cfg.maxRetries, c.breaker.remaining(c.cfg.clock.Now()) > 0, c.cfg.jitter)
		if !plan.Retry {
			return nil, e
		}
		if serr := c.cfg.sleeper.Sleep(ctx, plan.Wait); serr != nil {
			return nil, canceledError(op, serr)
		}
		st = plan.Next
	}
}

func encodeBody(op, method string, body any) ([]byte, *Error) {
	if method != http.MethodPost {
		return nil, nil
	}
	if body == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, &Error{Kind: KindInvalidRequest, Op: op, Message: "cannot encode request", Err: err}
	}
	return b, nil
}

// attempt performs one HTTP exchange after the breaker check and a rate token.
func (c *Client) attempt(ctx context.Context, op, method, path string, payload []byte) (json.RawMessage, *Error) {
	if rem := c.breaker.remaining(c.cfg.clock.Now()); rem > 0 {
		return nil, &Error{Kind: KindRateLimited, Op: op, Message: "rate limit cooldown active", RetryAfter: rem}
	}
	if err := c.waitToken(ctx); err != nil {
		return nil, canceledError(op, err)
	}
	data, e := c.send(ctx, op, method, path, payload)
	if e != nil && e.Kind == KindRateLimited {
		c.noteRateLimit(e)
	}
	return data, e
}

// noteRateLimit feeds the breach circuit; when it opens, e carries the cooldown.
func (c *Client) noteRateLimit(e *Error) {
	now := c.cfg.clock.Now()
	until, opened := c.breaker.record(now)
	if !opened {
		return
	}
	e.RetryAfter = until.Sub(now)
	if c.cfg.breachHook != nil {
		c.cfg.breachHook(until)
	}
}

// waitToken takes one token from the bucket, sleeping through the injected Sleeper.
func (c *Client) waitToken(ctx context.Context) error {
	now := c.cfg.clock.Now()
	r := c.limiter.ReserveN(now, 1)
	delay := r.DelayFrom(now)
	if delay <= 0 {
		return ctx.Err()
	}
	if err := c.cfg.sleeper.Sleep(ctx, delay); err != nil {
		r.CancelAt(c.cfg.clock.Now())
		return err
	}
	return nil
}

func (c *Client) send(ctx context.Context, op, method, path string, payload []byte) (json.RawMessage, *Error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.cfg.timeout)
	defer cancel()
	req, err := c.newRequest(attemptCtx, method, path, payload)
	if err != nil {
		return nil, &Error{Kind: KindInvalidRequest, Op: op, Message: "cannot build request", Err: err}
	}
	start := c.cfg.clock.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.logAttempt(op, method, path, 0, start)
		return nil, transportError(ctx, op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != http.StatusNotModified {
		c.logAttempt(op, method, path, resp.StatusCode, start) // a redirect that was not followed: no Location, or refused
		return nil, &Error{Kind: KindUntrustedRedirect, Op: op, HTTPStatus: resp.StatusCode, Message: "unfollowed redirect"}
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	c.logAttempt(op, method, path, resp.StatusCode, start)
	if rerr != nil {
		return nil, transportError(ctx, op, rerr)
	}
	if len(body) > maxBodyBytes {
		return nil, &Error{Kind: KindDecode, Op: op, HTTPStatus: resp.StatusCode, Message: "response body over 16 MiB"}
	}
	return parseResponse(op, resp.StatusCode, resp.Header, body, c.cfg.clock.Now())
}

func (c *Client) newRequest(ctx context.Context, method, path string, payload []byte) (*http.Request, error) {
	u := c.base.JoinPath(strings.TrimPrefix(path, "/"))
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.cfg.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) logAttempt(op, method, path string, status int, start time.Time) {
	c.cfg.logger.Debug("mothdex request",
		"op", op, "method", method, "path", path, "status", status,
		"latency", c.cfg.clock.Now().Sub(start))
}

// transportError maps a failure below HTTP: cancelation of the caller's
// context is KindCanceled, anything else (including the per-attempt timeout)
// is KindNetwork.
func transportError(ctx context.Context, op string, err error) *Error {
	if cerr := ctx.Err(); cerr != nil {
		return canceledError(op, cerr)
	}
	var refused *refusedRedirect
	if errors.Is(err, errRedirectRefused) && errors.As(err, &refused) { // before the network branch
		return &Error{Kind: KindUntrustedRedirect, Op: op, Message: "redirect to " + refused.host + " refused", Err: errRedirectRefused}
	}
	return &Error{Kind: KindNetwork, Op: op, Message: err.Error(), Err: err}
}

func canceledError(op string, cause error) *Error {
	return &Error{Kind: KindCanceled, Op: op, Message: cause.Error(), Err: cause}
}
