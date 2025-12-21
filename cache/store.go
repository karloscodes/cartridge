// Package cache provides a unified caching interface inspired by Rails' Solid Cache.
// It supports both in-memory and database-backed stores with automatic expiration.
package cache

import (
	"context"
	"time"
)

// Store is the unified cache interface that all cache implementations must satisfy.
// Inspired by Rails' ActiveSupport::Cache::Store API.
type Store interface {
	// Read retrieves a value from the cache. Returns nil, false if not found or expired.
	Read(ctx context.Context, key string) ([]byte, bool)

	// Write stores a value in the cache with the default TTL.
	Write(ctx context.Context, key string, value []byte) error

	// WriteWithTTL stores a value with a custom TTL.
	WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// DeleteByPrefix removes all keys matching the prefix.
	DeleteByPrefix(ctx context.Context, prefix string) (int, error)

	// Clear removes all entries from the cache.
	Clear(ctx context.Context) error

	// Exist checks if a key exists and is not expired.
	Exist(ctx context.Context, key string) bool

	// Stats returns cache statistics.
	Stats(ctx context.Context) Stats
}

// Stats contains cache statistics.
type Stats struct {
	Entries        int64         `json:"entries"`
	ExpiredEntries int64         `json:"expired_entries"`
	MaxEntries     int64         `json:"max_entries,omitempty"`
	TTL            time.Duration `json:"ttl"`
	Backend        string        `json:"backend"`
}

// Options configures cache behavior.
type Options struct {
	// TTL is the default time-to-live for cache entries. Default: 2 weeks.
	TTL time.Duration

	// MaxEntries limits the number of entries. 0 means unlimited.
	// When exceeded, oldest entries are evicted (FIFO).
	MaxEntries int64

	// CleanupInterval is how often expired entries are removed. Default: 1 hour.
	// Set to 0 to disable background cleanup.
	CleanupInterval time.Duration

	// CleanupBatchSize is how many entries to delete per cleanup cycle. Default: 100.
	CleanupBatchSize int
}

// DefaultOptions returns sensible defaults inspired by Solid Cache.
func DefaultOptions() Options {
	return Options{
		TTL:              14 * 24 * time.Hour, // 2 weeks (Solid Cache default)
		MaxEntries:       0,                   // Unlimited
		CleanupInterval:  1 * time.Hour,
		CleanupBatchSize: 100,
	}
}

// Option is a functional option for configuring cache behavior.
type Option func(*Options)

// WithTTL sets the default TTL for cache entries.
func WithTTL(ttl time.Duration) Option {
	return func(o *Options) {
		o.TTL = ttl
	}
}

// WithMaxEntries sets the maximum number of cache entries.
func WithMaxEntries(max int64) Option {
	return func(o *Options) {
		o.MaxEntries = max
	}
}

// WithCleanupInterval sets how often expired entries are cleaned up.
func WithCleanupInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
	}
}

// WithCleanupBatchSize sets how many entries to clean per cycle.
func WithCleanupBatchSize(size int) Option {
	return func(o *Options) {
		o.CleanupBatchSize = size
	}
}

// applyOptions applies functional options to the default options.
func applyOptions(opts ...Option) Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
