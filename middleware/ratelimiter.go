// Package middleware holds optional middleware for cartridge routes.
package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/karloscodes/cartridge"
)

// EnvironmentChecker provides methods to check the runtime environment.
// Implement this interface to automatically skip rate limiting in dev/test modes.
type EnvironmentChecker interface {
	IsTest() bool
	IsDevelopment() bool
}

// RateLimiterConfig holds configuration for the rate limiter.
type RateLimiterConfig struct {
	Max      int
	Duration time.Duration
	Skip     func(*cartridge.Context) bool
	Env      EnvironmentChecker              // Optional: environment checker to skip rate limiting in dev/test
	Key      func(*cartridge.Context) string // Optional: the client key; default c.IP()
}

// RateLimiterOption defines a function to modify RateLimiterConfig.
type RateLimiterOption func(*RateLimiterConfig)

// WithMax sets the maximum number of requests allowed within the time window.
// Example: WithMax(100) allows 100 requests per Duration window
func WithMax(max int) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Max = max
	}
}

// WithDuration sets the duration for the rate limit window.
// Example: WithDuration(time.Minute) creates a per-minute rate limit
func WithDuration(duration time.Duration) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Duration = duration
	}
}

// WithSkip configures a predicate to skip rate limiting when it returns true.
// Example: WithSkip(func(c *cartridge.Context) bool { return c.Get("X-API-Key") == "admin" })
func WithSkip(skip func(*cartridge.Context) bool) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Skip = skip
	}
}

// WithKeyGenerator sets how requests are grouped into one budget. The default
// is c.IP(). A client cannot change c.IP(): it reads proxy headers only from
// ServerConfig.TrustedProxies. Behind a proxy, set ProxyHeader and
// TrustedProxies, or every client shares the proxy's budget.
func WithKeyGenerator(key func(*cartridge.Context) string) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Key = key
	}
}

// WithEnv configures environment checking to automatically skip rate limiting
// in development and test environments. This is the recommended way to configure
// rate limiting as it follows the convention over configuration principle.
// Example: WithEnv(cfg) where cfg implements EnvironmentChecker
func WithEnv(env EnvironmentChecker) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Env = env
	}
}

// RateLimiter creates a fixed-window rate limiting middleware.
// By default, limits to 50 requests per second per IP address.
// Counts live in memory, so each process has its own budget.
//
// Example usage:
//
//	RateLimiter(WithMax(100), WithDuration(time.Minute))  // 100 req/min
func RateLimiter(options ...RateLimiterOption) cartridge.HandlerFunc {
	cfg := RateLimiterConfig{
		Max:      50,
		Duration: time.Second,
	}

	for _, option := range options {
		option(&cfg)
	}

	// Validate and apply defaults
	if cfg.Max <= 0 {
		cfg.Max = 50
	}
	if cfg.Duration <= 0 {
		cfg.Duration = time.Second
	}

	store := &windowStore{window: cfg.Duration, entries: map[string]*window{}}
	limit := strconv.Itoa(cfg.Max)

	return func(c *cartridge.Context) error {
		// Skip rate limiting in dev/test environments (convention over configuration)
		if cfg.Env != nil && (cfg.Env.IsTest() || cfg.Env.IsDevelopment()) {
			return c.Next()
		}
		if cfg.Skip != nil && cfg.Skip(c) {
			return c.Next()
		}

		key := c.IP()
		if cfg.Key != nil {
			key = cfg.Key(c)
		}
		hits, resetIn := store.hit(key, time.Now())
		remaining := cfg.Max - hits

		c.Set("X-RateLimit-Limit", limit)
		if remaining < 0 {
			// Retry-After for well-behaved clients
			c.Set("Retry-After", "60")
			c.Set("X-RateLimit-Remaining", "0")
			return c.Status(http.StatusTooManyRequests).JSON(cartridge.Map{
				"error":       "Too Many Requests",
				"message":     "Rate limit exceeded. Please try again later.",
				"retry_after": 60,
			})
		}

		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Set("X-RateLimit-Reset", strconv.Itoa(int(resetIn.Seconds())))
		return c.Next()
	}
}

// windowStore counts hits per key in fixed time windows.
type windowStore struct {
	mu        sync.Mutex
	window    time.Duration
	entries   map[string]*window
	lastSweep time.Time
}

type window struct {
	hits    int
	resetAt time.Time
}

// hit records one request for key. It returns the hits in the current
// window and the time until the window resets.
func (s *windowStore) hit(key string, now time.Time) (int, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Drop expired windows once per window, so the map does not grow forever.
	if now.Sub(s.lastSweep) >= s.window {
		for k, e := range s.entries {
			if !now.Before(e.resetAt) {
				delete(s.entries, k)
			}
		}
		s.lastSweep = now
	}

	e, ok := s.entries[key]
	if !ok || !now.Before(e.resetAt) {
		e = &window{resetAt: now.Add(s.window)}
		s.entries[key] = e
	}
	e.hits++
	return e.hits, e.resetAt.Sub(now)
}
