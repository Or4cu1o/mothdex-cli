package mothdex

import (
	"math/rand/v2"
	"time"
)

// fullJitter returns a uniform duration in [0, max]. Retry timing is not a
// security property, so the non-cryptographic generator is deliberate.
func fullJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max) + 1)) //nolint:gosec // G404: backoff jitter only
}
