package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/utils"
)

// RateLimiterConfig holds configuration for the rate limiter.
type RateLimiterConfig struct {
	Max      int
	Duration time.Duration
	Skip     func(*fiber.Ctx) bool
	Storage  fiber.Storage // Optional: persistent storage for distributed rate limiting
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
// Example: WithSkip(func(c *fiber.Ctx) bool { return c.Get("X-API-Key") == "admin" })
func WithSkip(skip func(*fiber.Ctx) bool) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Skip = skip
	}
}

// WithStorage configures persistent storage for distributed rate limiting.
// Use this with Redis or other fiber.Storage implementations for multi-instance deployments.
// Example: WithStorage(myRedisStorage)
func WithStorage(storage fiber.Storage) RateLimiterOption {
	return func(cfg *RateLimiterConfig) {
		cfg.Storage = storage
	}
}

// RateLimiter creates a rate limiting middleware with customizable options.
// By default, limits to 50 requests per second per IP address.
// Uses in-memory storage by default - use WithStorage() for distributed setups.
//
// Example usage:
//
//	RateLimiter(WithMax(100), WithDuration(time.Minute))  // 100 req/min
func RateLimiter(options ...RateLimiterOption) fiber.Handler {
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

	limiterConfig := limiter.Config{
		Max:        cfg.Max,
		Expiration: cfg.Duration,
		Storage:    cfg.Storage, // nil = in-memory (default)
		KeyGenerator: func(c *fiber.Ctx) string {
			// Use utils.CopyString to avoid memory issues with pooled contexts
			return utils.CopyString(c.IP())
		},
		LimitReached: func(c *fiber.Ctx) error {
			// Set Retry-After header for well-behaved clients
			c.Set("Retry-After", "60") // Suggest retry after 60 seconds
			c.Set("X-RateLimit-Limit", string(rune(cfg.Max)))
			c.Set("X-RateLimit-Remaining", "0")

			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":       "Too Many Requests",
				"message":     "Rate limit exceeded. Please try again later.",
				"retry_after": 60,
			})
		},
		Next: func(c *fiber.Ctx) bool {
			if cfg.Skip != nil {
				return cfg.Skip(c)
			}
			return false
		},
	}

	return limiter.New(limiterConfig)
}
