package mothdex

import (
	"log/slog"
	"net/http"
	"time"
)

// Defaults from the contract (section 6).
const (
	DefaultBaseURL    = "https://api.mothdex.net/v1"
	defaultTimeout    = 15 * time.Second
	defaultRateLimit  = 5.0
	defaultBurst      = 5
	defaultMaxRetries = 2
	minRateLimit      = 1.0
	maxRateLimit      = 10.0
	maxMaxRetries     = 5
	repoURL           = "https://github.com/Or4cu1o/mothdex-cli"
)

// DefaultUserAgent returns the generic User-Agent for a CLI version. It never
// carries user data.
func DefaultUserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return "mothdex-cli/" + version + " (+" + repoURL + ")"
}

type config struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	rate       float64
	burst      int
	maxRetries int
	userAgent  string
	logger     *slog.Logger
	sleeper    Sleeper
	clock      Clock
	cooldown   time.Time
	breachHook func(until time.Time)
	jitter     func(max time.Duration) time.Duration
}

// Option configures New.
type Option func(*config)

// WithBaseURL sets the API base URL. https is required; http only for loopback hosts.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// WithTimeout sets the per-attempt timeout (headers and body).
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithRateLimit sets the token bucket; rps is clamped to 1..10, burst to at least 1.
func WithRateLimit(rps float64, burst int) Option {
	return func(c *config) {
		c.rate = min(max(rps, minRateLimit), maxRateLimit)
		c.burst = max(burst, 1)
	}
}

// WithMaxRetries sets the upstream/network retries, clamped to 0..5.
func WithMaxRetries(n int) Option {
	return func(c *config) { c.maxRetries = min(max(n, 0), maxMaxRetries) }
}

// WithUserAgent sets the User-Agent. Never put user data in it.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// WithLogger sets a logger for method, path, status and latency; bodies are never logged.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithSleeper injects the backoff sleeper.
func WithSleeper(s Sleeper) Option { return func(c *config) { c.sleeper = s } }

// WithClock injects the clock.
func WithClock(cl Clock) Option { return func(c *config) { c.clock = cl } }

// WithCooldown seeds the breach circuit with a cooldown persisted by an earlier process.
func WithCooldown(until time.Time) Option { return func(c *config) { c.cooldown = until } }

// WithBreachHook reports when the breach circuit opens.
func WithBreachHook(f func(until time.Time)) Option { return func(c *config) { c.breachHook = f } }

func defaultConfig() config {
	return config{
		baseURL:    DefaultBaseURL,
		timeout:    defaultTimeout,
		rate:       defaultRateLimit,
		burst:      defaultBurst,
		maxRetries: defaultMaxRetries,
		userAgent:  DefaultUserAgent(""),
		logger:     slog.New(slog.DiscardHandler),
		sleeper:    realSleeper{},
		clock:      realClock{},
		jitter:     fullJitter,
	}
}
