package cache

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-memory cache implementation with FIFO eviction.
// Thread-safe and suitable for single-instance deployments.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]*memoryEntry
	order   []string // Tracks insertion order for FIFO eviction
	opts    Options
	stopCh  chan struct{}
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
	createdAt time.Time
}

// NewMemoryStore creates a new in-memory cache store.
func NewMemoryStore(opts ...Option) *MemoryStore {
	options := applyOptions(opts...)

	s := &MemoryStore{
		entries: make(map[string]*memoryEntry),
		order:   make([]string, 0),
		opts:    options,
		stopCh:  make(chan struct{}),
	}

	// Start background cleanup if interval is set
	if options.CleanupInterval > 0 {
		go s.startCleanup()
	}

	return s
}

// Read retrieves a value from the cache.
func (s *MemoryStore) Read(ctx context.Context, key string) ([]byte, bool) {
	s.mu.RLock()
	entry, exists := s.entries[key]
	s.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Check expiration (but don't delete - let cleanup handle it)
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.value, true
}

// Write stores a value with the default TTL.
func (s *MemoryStore) Write(ctx context.Context, key string, value []byte) error {
	return s.WriteWithTTL(ctx, key, value, s.opts.TTL)
}

// WriteWithTTL stores a value with a custom TTL.
func (s *MemoryStore) WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Check if key already exists
	_, exists := s.entries[key]

	s.entries[key] = &memoryEntry{
		value:     value,
		expiresAt: now.Add(ttl),
		createdAt: now,
	}

	// Track insertion order for FIFO (only for new keys)
	if !exists {
		s.order = append(s.order, key)
	}

	// Enforce max entries limit
	s.enforceLimitLocked()

	return nil
}

// Delete removes a key from the cache.
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, key)
	s.removeFromOrder(key)
	return nil
}

// DeleteByPrefix removes all keys matching the prefix.
func (s *MemoryStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for key := range s.entries {
		if strings.HasPrefix(key, prefix) {
			delete(s.entries, key)
			s.removeFromOrder(key)
			count++
		}
	}

	return count, nil
}

// Clear removes all entries from the cache.
func (s *MemoryStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]*memoryEntry)
	s.order = make([]string, 0)

	return nil
}

// Exist checks if a key exists and is not expired.
func (s *MemoryStore) Exist(ctx context.Context, key string) bool {
	s.mu.RLock()
	entry, exists := s.entries[key]
	s.mu.RUnlock()

	if !exists {
		return false
	}

	return time.Now().Before(entry.expiresAt)
}

// Stats returns cache statistics.
func (s *MemoryStore) Stats(ctx context.Context) Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	expired := int64(0)
	for _, entry := range s.entries {
		if now.After(entry.expiresAt) {
			expired++
		}
	}

	return Stats{
		Entries:        int64(len(s.entries)),
		ExpiredEntries: expired,
		MaxEntries:     s.opts.MaxEntries,
		TTL:            s.opts.TTL,
		Backend:        "memory",
	}
}

// Close stops the background cleanup goroutine.
func (s *MemoryStore) Close() error {
	close(s.stopCh)
	return nil
}

// enforceLimitLocked evicts oldest entries if max is exceeded. Must hold write lock.
func (s *MemoryStore) enforceLimitLocked() {
	if s.opts.MaxEntries <= 0 {
		return
	}

	for int64(len(s.entries)) > s.opts.MaxEntries && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.entries, oldest)
	}
}

// removeFromOrder removes a key from the order slice.
func (s *MemoryStore) removeFromOrder(key string) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

// startCleanup runs periodic cleanup of expired entries.
func (s *MemoryStore) startCleanup() {
	ticker := time.NewTicker(s.opts.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

// cleanup removes expired entries.
func (s *MemoryStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	// Find expired entries (up to batch size)
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			expired = append(expired, key)
			if len(expired) >= s.opts.CleanupBatchSize {
				break
			}
		}
	}

	// Sort by creation time (oldest first) for consistent FIFO behavior
	sort.Slice(expired, func(i, j int) bool {
		return s.entries[expired[i]].createdAt.Before(s.entries[expired[j]].createdAt)
	})

	// Delete expired entries
	for _, key := range expired {
		delete(s.entries, key)
		s.removeFromOrder(key)
	}
}
