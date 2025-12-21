package cache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"gorm.io/gorm"
)

// CacheRecord defines the GORM model for storing cache entries.
// Works with any GORM-supported database (SQLite, PostgreSQL, MySQL, etc.).
type CacheRecord struct {
	Key         string `gorm:"primaryKey"`
	Value       []byte // Store marshalled GOB data
	LastUpdated int64  `gorm:"index"` // Unix timestamp
	TTLSeconds  int64
}

// TableName specifies the table name for GORM.
func (CacheRecord) TableName() string {
	return "generic_cache"
}

// GormCache implements database-backed caching using GORM.
// Works with any GORM-supported database (SQLite, PostgreSQL, MySQL, etc.).
// V can be any GOB-serializable type.
type GormCache[V any] struct {
	db        *gorm.DB
	mu        sync.RWMutex // Coordinates fetchFunc calls
	logger    *slog.Logger
	ttl       time.Duration
	fetchFunc func(key string) (V, error)
}

// NewGormCache creates a new database-backed cache instance.
func NewGormCache[V any](db *gorm.DB, logger *slog.Logger, ttl time.Duration, fetchFunc func(key string) (V, error)) (*GormCache[V], error) {
	return &GormCache[V]{
		db:        db,
		logger:    logger,
		ttl:       ttl,
		fetchFunc: fetchFunc,
	}, nil
}

// Get retrieves a value, checking DB and refreshing via fetchFunc if needed.
func (c *GormCache[V]) Get(key string) (V, error) {
	var record CacheRecord
	var zero V

	now := time.Now().Unix()
	tx := c.db.Where("key = ?", key).First(&record)

	if tx.Error == nil {
		c.logger.Debug("Record found. Checking expiry.", slog.String("key", key), slog.Int64("now", now), slog.Int64("lastUpdated", record.LastUpdated), slog.Int64("ttlSeconds", record.TTLSeconds))
		if now < record.LastUpdated+record.TTLSeconds {
			err := gob.NewDecoder(bytes.NewReader(record.Value)).Decode(&zero)
			if err != nil {
				c.logger.Error("Failed to gob decode cached value", slog.String("key", key), slog.Any("error", err))
			} else {
				c.logger.Debug("Cache hit", slog.String("key", key))
				return zero, nil
			}
		}
		c.logger.Debug("Cache expired", slog.String("key", key))
	} else if tx.Error != gorm.ErrRecordNotFound {
		c.logger.Error("Failed to query cache table", slog.String("key", key), slog.Any("error", tx.Error))
		return zero, tx.Error
	}
	c.logger.Debug("Cache miss", slog.String("key", key))

	// Refresh with mutex to prevent concurrent fetches for the same key
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring lock
	tx = c.db.Where("key = ?", key).First(&record)
	nowDoubleCheck := time.Now().Unix()
	if tx.Error == nil {
		c.logger.Debug("Double-check: Record found.", slog.String("key", key), slog.Int64("now", nowDoubleCheck), slog.Int64("lastUpdated", record.LastUpdated), slog.Int64("ttlSeconds", record.TTLSeconds))
		if now < record.LastUpdated+record.TTLSeconds {
			err := gob.NewDecoder(bytes.NewReader(record.Value)).Decode(&zero)
			if err == nil {
				return zero, nil
			}
			c.logger.Error("Failed to gob decode cached value during double check", slog.String("key", key), slog.Any("error", err))
		}
	}

	value, fetchErr := c.fetchFunc(key)
	if fetchErr != nil {
		c.logger.Error("Failed to refresh cache via fetchFunc", slog.String("key", key), slog.Any("error", fetchErr))
		if record.Key != "" && len(record.Value) > 0 {
			_ = gob.NewDecoder(bytes.NewReader(record.Value)).Decode(&zero)
			return zero, nil // Return stale value
		}
		return zero, fetchErr
	}

	err := c.setInternal(key, value)
	if err != nil {
		c.logger.Error("Failed to store refreshed value in cache", slog.String("key", key), slog.Any("error", err))
	}

	return value, nil
}

// Set stores a value in the cache.
func (c *GormCache[V]) Set(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.setInternal(key, value)
	if err != nil {
		c.logger.Error("Failed Set operation", slog.String("key", key), slog.Any("error", err))
	}
}

// setInternal performs the database write without locking.
func (c *GormCache[V]) setInternal(key string, value V) error {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := encoder.Encode(value)
	if err != nil {
		return fmt.Errorf("failed to gob encode value for key '%s': %w", key, err)
	}

	record := CacheRecord{
		Key:         key,
		Value:       buf.Bytes(),
		LastUpdated: time.Now().Unix(),
		TTLSeconds:  int64(c.ttl / time.Second),
	}

	tx := c.db.Save(&record)
	if tx.Error != nil {
		return fmt.Errorf("failed to save cache record for key '%s': %w", key, tx.Error)
	}

	c.logger.Debug("Set cache entry", slog.String("key", key))
	return nil
}

// Remove deletes a key from the cache.
func (c *GormCache[V]) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx := c.db.Delete(&CacheRecord{}, "key = ?", key)
	if tx.Error != nil {
		c.logger.Error("Failed to remove cache entry", slog.String("key", key), slog.Any("error", tx.Error))
	} else if tx.RowsAffected > 0 {
		c.logger.Debug("Removed cache entry", slog.String("key", key))
	}
}

// Clear removes all entries from the cache table.
func (c *GormCache[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx := c.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CacheRecord{})
	if tx.Error != nil {
		c.logger.Error("Failed to clear cache table", slog.Any("error", tx.Error))
	} else {
		c.logger.Debug("Cleared entire cache", slog.Int64("rows_affected", tx.RowsAffected))
	}
}

// InvalidateByPrefix removes entries with keys starting with the prefix.
func (c *GormCache[V]) InvalidateByPrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	tx := c.db.Where("key LIKE ?", prefix+"%").Delete(&CacheRecord{})
	if tx.Error != nil {
		c.logger.Error("Failed to invalidate cache by prefix", slog.String("prefix", prefix), slog.Any("error", tx.Error))
		return 0
	}

	if tx.RowsAffected > 0 {
		c.logger.Debug("Invalidated cache entries by prefix",
			slog.String("prefix", prefix),
			slog.Int64("count", tx.RowsAffected),
		)
	}
	return int(tx.RowsAffected)
}

// GetStats returns statistics about the cache.
func (c *GormCache[V]) GetStats() map[string]interface{} {
	var totalEntries int64
	c.db.Model(&CacheRecord{}).Count(&totalEntries)

	var expiredEntries int64
	now := time.Now().Unix()
	c.db.Model(&CacheRecord{}).Where("? > last_updated + ttl_seconds", now).Count(&expiredEntries)

	return map[string]interface{}{
		"backend":                "gorm",
		"total_entries":          totalEntries,
		"expired_entries":        expiredEntries,
		"configured_ttl_seconds": c.ttl.Seconds(),
	}
}

// PurgeAllCaches clears all cache records from the database.
func PurgeAllCaches(db *gorm.DB) (int64, error) {
	result := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CacheRecord{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
