package cache

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DatabaseStore is a database-backed cache using GORM.
// Works with any GORM-supported database (SQLite, PostgreSQL, MySQL, etc.).
// Uses FIFO eviction when max entries is exceeded.
type DatabaseStore struct {
	db     *gorm.DB
	opts   Options
	stopCh chan struct{}
}

// CacheEntry is the database model for cache entries.
type CacheEntry struct {
	Key       string `gorm:"primaryKey;size:255"`
	Value     []byte
	ExpiresAt int64 `gorm:"index"` // Unix milliseconds
	CreatedAt int64 `gorm:"index"` // Unix milliseconds for FIFO ordering
}

// TableName specifies the table name.
func (CacheEntry) TableName() string {
	return "cache_entries"
}

// NewDatabaseStore creates a new database-backed cache store.
// The cache_entries table is auto-migrated if it doesn't exist.
func NewDatabaseStore(db *gorm.DB, opts ...Option) (*DatabaseStore, error) {
	options := applyOptions(opts...)

	// Auto-migrate the table
	if err := db.AutoMigrate(&CacheEntry{}); err != nil {
		return nil, err
	}

	s := &DatabaseStore{
		db:     db,
		opts:   options,
		stopCh: make(chan struct{}),
	}

	// Start background cleanup if interval is set
	if options.CleanupInterval > 0 {
		go s.startCleanup()
	}

	return s, nil
}

// Read retrieves a value from the cache.
func (s *DatabaseStore) Read(ctx context.Context, key string) ([]byte, bool) {
	var entry CacheEntry
	now := time.Now().UnixMilli()

	result := s.db.WithContext(ctx).Where(keyIs(key)).Where("expires_at > ?", now).First(&entry)
	if result.Error != nil {
		return nil, false
	}

	return entry.Value, true
}

// Write stores a value with the default TTL.
func (s *DatabaseStore) Write(ctx context.Context, key string, value []byte) error {
	return s.WriteWithTTL(ctx, key, value, s.opts.TTL)
}

// WriteWithTTL stores a value with a custom TTL.
func (s *DatabaseStore) WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	now := time.Now().UnixMilli()
	entry := CacheEntry{
		Key:       key,
		Value:     value,
		ExpiresAt: now + ttl.Milliseconds(),
		CreatedAt: now,
	}

	// Use Save to upsert
	result := s.db.WithContext(ctx).Save(&entry)
	if result.Error != nil {
		return result.Error
	}

	// Enforce max entries limit
	s.enforceLimit(ctx)

	return nil
}

// Delete removes a key from the cache.
func (s *DatabaseStore) Delete(ctx context.Context, key string) error {
	return s.db.WithContext(ctx).Where(keyIs(key)).Delete(&CacheEntry{}).Error
}

// DeleteByPrefix removes all keys matching the prefix.
func (s *DatabaseStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	result := s.db.WithContext(ctx).Where(keyHasPrefix(prefix)).Delete(&CacheEntry{})
	return int(result.RowsAffected), result.Error
}

// Clear removes all entries from the cache.
func (s *DatabaseStore) Clear(ctx context.Context) error {
	return s.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CacheEntry{}).Error
}

// Exist checks if a key exists and is not expired.
func (s *DatabaseStore) Exist(ctx context.Context, key string) bool {
	now := time.Now().UnixMilli()
	var count int64
	s.db.WithContext(ctx).Model(&CacheEntry{}).Where(keyIs(key)).Where("expires_at > ?", now).Count(&count)
	return count > 0
}

// Stats returns cache statistics.
func (s *DatabaseStore) Stats(ctx context.Context) Stats {
	var total, expired int64
	now := time.Now().UnixMilli()

	s.db.WithContext(ctx).Model(&CacheEntry{}).Count(&total)
	s.db.WithContext(ctx).Model(&CacheEntry{}).Where("expires_at <= ?", now).Count(&expired)

	return Stats{
		Entries:        total,
		ExpiredEntries: expired,
		MaxEntries:     s.opts.MaxEntries,
		TTL:            s.opts.TTL,
		Backend:        "database",
	}
}

// Close stops the background cleanup goroutine.
func (s *DatabaseStore) Close() error {
	close(s.stopCh)
	return nil
}

// enforceLimit evicts oldest entries if max is exceeded.
func (s *DatabaseStore) enforceLimit(ctx context.Context) {
	if s.opts.MaxEntries <= 0 {
		return
	}

	var count int64
	s.db.WithContext(ctx).Model(&CacheEntry{}).Count(&count)

	if count <= s.opts.MaxEntries {
		return
	}

	// Delete oldest entries (FIFO) to get back to max. Two queries, because
	// MySQL does not take a LIMIT in an IN subquery.
	excess := count - s.opts.MaxEntries
	var oldest []string
	s.db.WithContext(ctx).Model(&CacheEntry{}).
		Order("created_at ASC").
		Limit(int(excess)).
		Pluck("key", &oldest)
	if len(oldest) == 0 {
		return
	}
	s.db.WithContext(ctx).
		Where(clause.IN{Column: clause.Column{Name: "key"}, Values: toAny(oldest)}).
		Delete(&CacheEntry{})
}

// "key" is a reserved word in MySQL. These conditions let GORM quote the
// column name for the database in use.

func keyIs(key string) clause.Expression {
	return clause.Eq{Column: clause.Column{Name: "key"}, Value: key}
}

func keyHasPrefix(prefix string) clause.Expression {
	return clause.Like{Column: clause.Column{Name: "key"}, Value: prefix + "%"}
}

func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// startCleanup runs periodic cleanup of expired entries.
func (s *DatabaseStore) startCleanup() {
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
func (s *DatabaseStore) cleanup() {
	now := time.Now().UnixMilli()

	// Delete expired entries in batches
	s.db.Where("expires_at <= ?", now).
		Limit(s.opts.CleanupBatchSize).
		Delete(&CacheEntry{})
}
