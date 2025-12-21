// Package cache provides generic in-memory and SQLite-backed caching with TTL support.
package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"log/slog"
)

// Cache is a generic in-memory cache with TTL-based refreshing.
type Cache[K comparable, V any] struct {
	data      map[K]cacheEntry[V]
	mu        sync.RWMutex
	logger    *slog.Logger
	ttl       time.Duration
	fetchFunc func(key K) (V, error) // Function to fetch the value from the source (e.g., database)
}

// cacheEntry stores a value and its last updated timestamp.
type cacheEntry[V any] struct {
	value       V
	lastUpdated time.Time
}

// NewCache creates a new generic Cache instance.
func NewCache[K comparable, V any](logger *slog.Logger, ttl time.Duration, fetchFunc func(key K) (V, error)) *Cache[K, V] {
	return &Cache[K, V]{
		data:      make(map[K]cacheEntry[V]),
		logger:    logger,
		ttl:       ttl,
		fetchFunc: fetchFunc,
	}
}

// Get retrieves a value from the cache, refreshing it if necessary.
func (c *Cache[K, V]) Get(key K) (V, error) {
	c.mu.RLock()
	entry, exists := c.data[key]
	if exists && time.Since(entry.lastUpdated) < c.ttl {
		c.mu.RUnlock()
		return entry.value, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired, refresh the value
	return c.refresh(key)
}

// Set manually sets a value in the cache.
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = cacheEntry[V]{
		value:       value,
		lastUpdated: time.Now(),
	}
	c.logger.Debug("Set cache entry",
		slog.Any("key", key),
		slog.Any("value", value),
	)
}

// refresh fetches the value for the given key and updates the cache.
func (c *Cache[K, V]) refresh(key K) (V, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring the lock
	if entry, exists := c.data[key]; exists && time.Since(entry.lastUpdated) < c.ttl {
		return entry.value, nil
	}

	// Fetch the value using the provided fetch function
	value, err := c.fetchFunc(key)
	if err != nil {
		c.logger.Error("Failed to refresh cache",
			slog.Any("key", key),
			slog.Any("error", err),
		)
		// Return the stale value if available, otherwise return the error
		if entry, exists := c.data[key]; exists {
			return entry.value, nil
		}
		var zero V
		return zero, err
	}

	// Update the cache
	c.data[key] = cacheEntry[V]{
		value:       value,
		lastUpdated: time.Now(),
	}
	c.logger.Debug("Refreshed cache entry",
		slog.Any("key", key),
		slog.Any("value", value),
	)
	return value, nil
}

// Clear removes all entries from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create a new map instead of ranging through and deleting
	// This is more efficient for large caches
	oldSize := len(c.data)
	c.data = make(map[K]cacheEntry[V])

	c.logger.Debug("Cleared entire cache",
		slog.Int("entries_removed", oldSize),
	)
}

// Remove deletes a specific key from the cache.
func (c *Cache[K, V]) Remove(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.data[key]; exists {
		delete(c.data, key)
		c.logger.Debug("Removed cache entry",
			slog.Any("key", key),
		)
	}
}

// InvalidateByPrefix removes all cache entries whose string keys start with the given prefix.
// This is useful for cache invalidation patterns where related items share a prefix.
// Note: This only works if K is string or has a String() method.
func (c *Cache[K, V]) InvalidateByPrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0

	// Type assertion to check if K is string
	for k := range c.data {
		var keyStr string

		// Try to convert key to string
		switch v := any(k).(type) {
		case string:
			keyStr = v
		case fmt.Stringer:
			keyStr = v.String()
		default:
			continue // Skip if not convertible to string
		}

		if strings.HasPrefix(keyStr, prefix) {
			delete(c.data, k)
			count++
		}
	}

	if count > 0 {
		c.logger.Debug("Invalidated cache entries by prefix",
			slog.String("prefix", prefix),
			slog.Int("count", count),
		)
	}

	return count
}

// GetStats returns statistics about the cache.
func (c *Cache[K, V]) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"total_entries": len(c.data),
		"ttl_seconds":   c.ttl.Seconds(),
	}

	// Count expired entries
	now := time.Now()
	expired := 0
	for _, entry := range c.data {
		if now.Sub(entry.lastUpdated) > c.ttl {
			expired++
		}
	}
	stats["expired_entries"] = expired

	return stats
}
