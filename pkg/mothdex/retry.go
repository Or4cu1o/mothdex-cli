package mothdex

import "time"

// Retry constants from the contract (sections 4 and 5).
const (
	maxRateLimitRetries = 1
	defaultRetryAfter   = 2 * time.Second
	maxAutoWait         = 30 * time.Second // longer Retry-After is reported, not slept
	backoffBase         = 500 * time.Millisecond
	backoffCap          = 8 * time.Second
)

// retryState counts the retries already spent on one call.
type retryState struct {
	backoff   int // upstream/network retries
	rateLimit int // 429 retries
}

// retryPlan is the outcome of the decision rule.
type retryPlan struct {
	Retry bool
	Wait  time.Duration
	Next  retryState // state after taking this retry; unchanged when Retry is false
}

// planRetry is the pure retry rule. It never sleeps and never reads a clock.
// jitter returns a duration in [0, max] and is injected so tests are exact.
func planRetry(e *Error, st retryState, maxRetries int, breakerOpen bool, jitter func(max time.Duration) time.Duration) retryPlan {
	switch {
	case e.Kind == KindRateLimited:
		return planRateLimited(e, st, breakerOpen)
	case retryableKind(e.Kind) && st.backoff < maxRetries:
		wait := jitter(backoffCeiling(st.backoff))
		return retryPlan{Retry: true, Wait: wait, Next: retryState{backoff: st.backoff + 1, rateLimit: st.rateLimit}}
	default:
		return retryPlan{Next: st}
	}
}

func planRateLimited(e *Error, st retryState, breakerOpen bool) retryPlan {
	if breakerOpen || st.rateLimit >= maxRateLimitRetries {
		return retryPlan{Next: st}
	}
	wait := e.RetryAfter
	if wait <= 0 {
		wait = defaultRetryAfter
	}
	if wait > maxAutoWait {
		return retryPlan{Next: st}
	}
	return retryPlan{Retry: true, Wait: wait, Next: retryState{backoff: st.backoff, rateLimit: st.rateLimit + 1}}
}

// backoffCeiling is min(backoffCap, 500ms * 2^n): the upper bound of the full jitter.
func backoffCeiling(n int) time.Duration {
	d := backoffBase
	for i := 0; i < n && d < backoffCap; i++ {
		d *= 2
	}
	return min(d, backoffCap)
}
