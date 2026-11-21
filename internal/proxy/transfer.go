package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type failKind int

const (
	kindFatal     failKind = iota // never retried
	kindTransient                 // network, truncation, 5xx, 429: max_attempts
	kindNotFound                  // 404 may be transient (A3): its own budget and delays
	kindIntegrity                 // one re-download
)

// attemptErr is the failure of one attempt and how the retry policy treats it.
type attemptErr struct {
	kind       failKind
	err        *Error
	retryAfter time.Duration
}

func fatal(code Code, msg string, cause error) *attemptErr {
	return &attemptErr{kind: kindFatal, err: &Error{Code: code, Message: msg, Err: cause}}
}

var (
	notFoundDelays = []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}

	errHeaderTimeout = errors.New("timeout waiting for the response headers")
	errFirstByte     = errors.New("timeout waiting for the first byte")
	errIdle          = errors.New("timeout: no data received")
)

const (
	maxRetryAfter = 15 * time.Minute // a worker slot is held while waiting
	backoffBase   = 2 * time.Second
	backoffCap    = 60 * time.Second
)

// transfer runs attempts until the file is complete and verified, or the retry policy gives up.
func (d *Downloader) transfer(ctx context.Context, j *jobState) *Error {
	failures, notFound, integrityRetried := 0, 0, false
	for {
		if ctx.Err() != nil {
			return canceled(ctx)
		}
		j.attempts++
		ae := d.attempt(ctx, j)
		if ae == nil {
			return nil
		}
		var delay time.Duration
		switch ae.kind {
		case kindFatal:
			return ae.err
		case kindTransient:
			failures++
			if failures >= d.cfg.MaxAttempts {
				return ae.err
			}
			delay = max(d.backoff(failures), ae.retryAfter)
		case kindNotFound:
			notFound++
			if notFound > d.cfg.NotFoundRetries {
				return ae.err
			}
			delay = notFoundDelays[min(notFound-1, len(notFoundDelays)-1)]
		case kindIntegrity:
			if integrityRetried {
				return ae.err
			}
			integrityRetried = true
		}
		d.event(j, StateRetryWait, ae.err)
		if err := d.o.Sleeper.Sleep(ctx, delay); err != nil {
			return canceled(ctx)
		}
		if err := d.rewind(j); err != nil {
			return err
		}
	}
}

func canceled(ctx context.Context) *Error {
	return &Error{Code: CodeCanceled, Message: "canceled", Err: ctx.Err()}
}

// backoff is full jitter: rand(0, min(60 s, 2 s * 2^(failures-1))).
func (d *Downloader) backoff(failures int) time.Duration {
	limit := backoffCap
	if failures-1 < 5 {
		limit = min(backoffCap, backoffBase<<(failures-1))
	}
	return time.Duration(d.o.Rand.Int63n(int64(limit)))
}

// rewind empties the staging file on the same descriptor (never by path, TH-11).
func (d *Downloader) rewind(j *jobState) *Error {
	if err := j.file.Truncate(0); err != nil {
		return &Error{Code: CodeInternal, Message: "cannot truncate the staging file", Err: err}
	}
	if _, err := j.file.Seek(0, io.SeekStart); err != nil {
		return &Error{Code: CodeInternal, Message: "cannot rewind the staging file", Err: err}
	}
	j.bytes, j.transferred = 0, 0
	return nil
}

// attempt is one request, from Connecting to the end of Verifying.
func (d *Downloader) attempt(ctx context.Context, j *jobState) *attemptErr {
	link := j.job.Link
	if _, err := CheckURL(link.URL, d.cfg.AllowedHosts); err != nil { // validated at use, every attempt
		return &attemptErr{kind: kindFatal, err: asError(err)}
	}
	d.event(j, StateConnecting, nil)

	actx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	wd := newWatchdog(d.o.Clock, cancel)
	defer wd.disarm()

	req, err := http.NewRequestWithContext(actx, http.MethodGet, j.url, nil)
	if err != nil {
		return fatal(CodeInternal, "cannot build the request", err)
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", d.cfg.UserAgent)

	wd.arm(d.cfg.HeaderTimeout, errHeaderTimeout)
	resp, err := d.o.HTTP.Do(req)
	wd.disarm()
	if err != nil {
		return d.netFailure(ctx, actx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ae := d.checkResponse(j, req, resp); ae != nil {
		return ae
	}

	d.event(j, StateWarming, nil)
	wd.arm(d.cfg.FirstByteTimeout, errFirstByte)
	if ae := d.copyBody(ctx, actx, j, resp.Body, wd); ae != nil {
		return ae
	}
	wd.disarm()
	return d.verify(ctx, j)
}

// checkResponse applies the status, redirect, encoding and length rules of sections 2, 3.1 and 5.
func (d *Downloader) checkResponse(j *jobState, req *http.Request, resp *http.Response) *attemptErr {
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.String() != req.URL.String() {
		return fatal(CodeUntrustedURL, "the request was redirected", nil) // a client that follows redirects
	}
	switch s := resp.StatusCode; {
	case s >= 300 && s < 400:
		return fatal(CodeUntrustedURL, "unexpected redirect", nil)
	case s == http.StatusOK:
	case s == http.StatusNotFound:
		return &attemptErr{kind: kindNotFound, err: &Error{Code: CodeNotFound, Message: "not found"}}
	case s == http.StatusTooManyRequests:
		return &attemptErr{kind: kindTransient, err: &Error{Code: CodeRateLimited, Message: "rate limited"},
			retryAfter: retryAfter(resp.Header.Get("Retry-After"), d.o.Clock.Now())}
	case s >= 500:
		return &attemptErr{kind: kindTransient, err: &Error{Code: CodeUpstream, Message: "server error " + strconv.Itoa(s)},
			retryAfter: retryAfter(resp.Header.Get("Retry-After"), d.o.Clock.Now())}
	default:
		return fatal(CodeUpstream, "unexpected status "+strconv.Itoa(s), nil)
	}
	if ce := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); resp.Uncompressed || (ce != "" && ce != "identity") {
		return fatal(CodeIntegrity, "the response has a content encoding", nil)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != j.job.Link.Size {
		return fatal(CodeSizeMismatch, "Content-Length differs from the size of the link", nil)
	}
	return nil
}

// retryAfter reads a delay in seconds or an HTTP date; anything else is no delay.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return min(time.Duration(secs)*time.Second, maxRetryAfter)
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return min(t.Sub(now), maxRetryAfter)
	}
	return 0
}

// netFailure maps a transport or read error: cancellation of the whole job, or a retryable
// network failure (timeouts included). The error text of the transport may quote the URL, so
// it is kept as the cause only, never in the message.
func (d *Downloader) netFailure(ctx, actx context.Context, err error) *attemptErr {
	if ctx.Err() != nil {
		return &attemptErr{kind: kindFatal, err: canceled(ctx)}
	}
	msg := "network error"
	if cause := context.Cause(actx); errors.Is(cause, errHeaderTimeout) || errors.Is(cause, errFirstByte) || errors.Is(cause, errIdle) {
		msg, err = cause.Error(), cause
	}
	return &attemptErr{kind: kindTransient, err: &Error{Code: CodeNetwork, Message: msg, Err: err}}
}

// copyBody streams the body into the staging file through the size+1 limit reader, hashing on
// the fly. It returns nil when exactly size bytes arrived.
func (d *Downloader) copyBody(ctx, actx context.Context, j *jobState, body io.Reader, wd *watchdog) *attemptErr {
	size := j.job.Link.Size
	r := io.LimitReader(body, size+1)
	h := sha256.New()
	buf := make([]byte, copyBuffer)
	first := true
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if first {
				first = false
				wd.arm(d.cfg.IdleTimeout, errIdle)
				j.transferAt = d.o.Clock.Now()
				d.event(j, StateTransferring, nil)
			}
			wd.touch()
			if ae := d.store(j, h, buf[:n], size); ae != nil {
				return ae
			}
			d.progress(j)
		}
		switch {
		case rerr == nil:
		case errors.Is(rerr, io.EOF):
			if j.bytes < size {
				return &attemptErr{kind: kindTransient, err: &Error{Code: CodeNetwork, Message: "the stream ended early"}}
			}
			j.sum = hex.EncodeToString(h.Sum(nil))
			return nil
		default:
			return d.netFailure(ctx, actx, rerr)
		}
	}
}

// store writes one chunk, refusing more than size bytes in total.
func (d *Downloader) store(j *jobState, h hash.Hash, chunk []byte, size int64) *attemptErr {
	if j.bytes+int64(len(chunk)) > size {
		return fatal(CodeSizeMismatch, "the server sent more bytes than the size of the link", nil)
	}
	n, err := j.file.Write(chunk)
	if err == nil && n != len(chunk) {
		err = io.ErrShortWrite
	}
	if err != nil {
		if isDiskFull(err) {
			return fatal(CodeDiskFull, "the disk is full", err)
		}
		return fatal(CodeInternal, "cannot write the staging file", err)
	}
	h.Write(chunk)
	j.bytes += int64(n)
	j.transferred = j.bytes
	return nil
}

// watchdog cancels a context when nothing happened for a duration, measured on the injected
// clock. touch records activity; arm replaces the duration and the cause.
type watchdog struct {
	clock  Clock
	cancel context.CancelCauseFunc
	last   atomic.Int64 // UnixNano of the last activity

	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

func newWatchdog(c Clock, cancel context.CancelCauseFunc) *watchdog {
	return &watchdog{clock: c, cancel: cancel}
}

func (w *watchdog) touch() { w.last.Store(w.clock.Now().UnixNano()) }

func (w *watchdog) arm(d time.Duration, cause error) {
	w.disarm()
	w.touch()
	stop, done := make(chan struct{}), make(chan struct{})
	w.mu.Lock()
	w.stop, w.done = stop, done
	w.mu.Unlock()
	go func() {
		defer close(done)
		for {
			idle := w.clock.Now().Sub(time.Unix(0, w.last.Load()))
			ch, stopTimer := w.clock.NewTimer(max(d-idle, time.Nanosecond))
			select {
			case <-ch:
				if w.clock.Now().Sub(time.Unix(0, w.last.Load())) >= d {
					w.cancel(cause)
					return
				}
			case <-stop:
				stopTimer()
				return
			}
		}
	}()
}

func (w *watchdog) disarm() {
	w.mu.Lock()
	stop, done := w.stop, w.done
	w.stop, w.done = nil, nil
	w.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}
