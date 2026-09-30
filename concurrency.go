package cartridge

import (
	"context"
	"time"

	"net/http"

	"golang.org/x/sync/semaphore"
)

// ConcurrencyLimiter manages concurrent read and write operations.
// This is particularly useful for SQLite with WAL mode, which allows
// one writer + multiple readers concurrently.
type ConcurrencyLimiter struct {
	readSem  *semaphore.Weighted
	writeSem *semaphore.Weighted
	timeout  time.Duration
	logger   limiterLogger
}

// limiterLogger is the logging the limiter needs. *slog.Logger satisfies it.
type limiterLogger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewConcurrencyLimiter creates a limiter with the provided thresholds.
func NewConcurrencyLimiter(readLimit, writeLimit int64, timeout time.Duration, logger limiterLogger) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		readSem:  semaphore.NewWeighted(readLimit),
		writeSem: semaphore.NewWeighted(writeLimit),
		timeout:  timeout,
		logger:   logger,
	}
}

// AcquireRead acquires a read semaphore.
func (cl *ConcurrencyLimiter) AcquireRead(ctx context.Context) error {
	return cl.readSem.Acquire(ctx, 1)
}

// AcquireWrite acquires a write semaphore.
func (cl *ConcurrencyLimiter) AcquireWrite(ctx context.Context) error {
	return cl.writeSem.Acquire(ctx, 1)
}

// ReleaseRead releases a read semaphore.
func (cl *ConcurrencyLimiter) ReleaseRead() {
	cl.readSem.Release(1)
}

// ReleaseWrite releases a write semaphore.
func (cl *ConcurrencyLimiter) ReleaseWrite() {
	cl.writeSem.Release(1)
}

// WriteConcurrencyLimitMiddleware limits concurrent write operations to protect database integrity.
// For SQLite with WAL mode, this prevents write contention while allowing reasonable concurrency.
func WriteConcurrencyLimitMiddleware(limiter *ConcurrencyLimiter) HandlerFunc {
	return func(c *Context) error {
		// Skip for OPTIONS (CORS preflight)
		if c.Method() == http.MethodOptions {
			return c.Next()
		}

		// Check if parent context is already canceled
		if err := c.Context().Err(); err != nil {
			limiter.logger.Debug("Request context already canceled",
				"path", c.Path(),
				"error", err,
			)
			return c.Status(http.StatusRequestTimeout).JSON(Map{
				"error":   "Request Timeout",
				"message": "Request was canceled",
			})
		}

		// Create timeout context from request context (not background context)
		ctx, cancel := context.WithTimeout(c.Context(), limiter.timeout)
		defer cancel()

		start := time.Now()
		if err := limiter.AcquireWrite(ctx); err != nil {
			waitTime := time.Since(start)
			limiter.logger.Warn("Write concurrency limit reached",
				"path", c.Path(),
				"ip", c.IP(),
				"method", c.Method(),
				"wait_time", waitTime,
				"error", err,
			)

			// Return appropriate error based on context
			if ctx.Err() == context.DeadlineExceeded {
				return c.Status(http.StatusServiceUnavailable).JSON(Map{
					"error":       "Service Unavailable",
					"message":     "Server is at capacity processing writes, please retry",
					"retry_after": "1",
				})
			}

			return c.Status(http.StatusServiceUnavailable).JSON(Map{
				"error":   "Service Unavailable",
				"message": "Write operation could not be queued",
			})
		}
		defer limiter.ReleaseWrite()

		acquireTime := time.Since(start)
		if acquireTime > 100*time.Millisecond {
			limiter.logger.Info("Write operation queued (high load detected)",
				"path", c.Path(),
				"ip", c.IP(),
				"queue_time", acquireTime,
			)
		}

		return c.Next()
	}
}
