package cache

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/karloscodes/cartridge/internal/dialect"
)

// table holds the cache entries. cartridge creates it.
const table = "cartridge_cache"

// DatabaseStore keeps the cache in a table of a SQLite, PostgreSQL, or
// MySQL database, like Solid Cache in Rails. Every process that uses the
// database shares it, and it survives a restart. When MaxEntries is
// exceeded, the oldest entries go first.
type DatabaseStore struct {
	read    *sql.DB
	write   *sql.DB
	dialect dialect.Dialect
	opts    Options
	stopCh  chan struct{}
}

// NewDatabaseStore creates a cache store. It reads through read and writes
// through write: the Reader and the Writer of a cartridge.DBManager. For a
// database with one pool, pass it twice. It creates the table
// cartridge_cache when it does not exist. The database can be the app's
// main database or another one.
func NewDatabaseStore(read, write *sql.DB, opts ...Option) (*DatabaseStore, error) {
	s := &DatabaseStore{
		read:    read,
		write:   write,
		dialect: dialect.Of(write),
		opts:    applyOptions(opts...),
		stopCh:  make(chan struct{}),
	}

	create := "CREATE TABLE IF NOT EXISTS " + table + " (" +
		"cache_key VARCHAR(255) PRIMARY KEY, " +
		"value " + s.dialect.Blob() + " NOT NULL, " +
		"expires_at BIGINT NOT NULL, " + // Unix milliseconds
		"created_at BIGINT NOT NULL)" // Unix milliseconds, for the oldest-first limit
	if _, err := write.Exec(create); err != nil {
		return nil, err
	}

	if s.opts.CleanupInterval > 0 {
		go s.startCleanup()
	}
	return s, nil
}

func (s *DatabaseStore) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.write.ExecContext(ctx, s.dialect.Rebind(query), args...)
}

// Read retrieves a value from the cache.
func (s *DatabaseStore) Read(ctx context.Context, key string) ([]byte, bool) {
	var value []byte
	query := "SELECT value FROM " + table + " WHERE cache_key = ? AND expires_at > ?"
	err := s.read.QueryRowContext(ctx, s.dialect.Rebind(query), key, time.Now().UnixMilli()).Scan(&value)
	if err != nil {
		return nil, false
	}
	return value, true
}

// Write stores a value with the default TTL.
func (s *DatabaseStore) Write(ctx context.Context, key string, value []byte) error {
	return s.WriteWithTTL(ctx, key, value, s.opts.TTL)
}

// WriteWithTTL stores a value with a custom TTL.
func (s *DatabaseStore) WriteWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if value == nil {
		value = []byte{}
	}
	now := time.Now().UnixMilli()
	query := "INSERT INTO " + table + " (cache_key, value, expires_at, created_at) VALUES (?, ?, ?, ?) " +
		s.dialect.Upsert("cache_key", "value", "expires_at", "created_at")
	if _, err := s.exec(ctx, query, key, value, now+ttl.Milliseconds(), now); err != nil {
		return err
	}
	s.enforceLimit(ctx)
	return nil
}

// Delete removes a key from the cache.
func (s *DatabaseStore) Delete(ctx context.Context, key string) error {
	_, err := s.exec(ctx, "DELETE FROM "+table+" WHERE cache_key = ?", key)
	return err
}

// DeleteByPrefix removes all keys that start with prefix. A "%" or "_" in
// the prefix counts as that character, not as a wildcard.
func (s *DatabaseStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	pattern := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(prefix) + "%"
	result, err := s.exec(ctx, "DELETE FROM "+table+" WHERE cache_key LIKE ? ESCAPE '!'", pattern)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// Clear removes all entries from the cache.
func (s *DatabaseStore) Clear(ctx context.Context) error {
	_, err := s.exec(ctx, "DELETE FROM "+table)
	return err
}

// Exist checks if a key exists and is not expired.
func (s *DatabaseStore) Exist(ctx context.Context, key string) bool {
	return s.count(ctx, "WHERE cache_key = ? AND expires_at > ?", key, time.Now().UnixMilli()) > 0
}

// Stats returns cache statistics.
func (s *DatabaseStore) Stats(ctx context.Context) Stats {
	return Stats{
		Entries:        s.count(ctx, ""),
		ExpiredEntries: s.count(ctx, "WHERE expires_at <= ?", time.Now().UnixMilli()),
		MaxEntries:     s.opts.MaxEntries,
		TTL:            s.opts.TTL,
		Backend:        "database",
	}
}

func (s *DatabaseStore) count(ctx context.Context, where string, args ...any) int64 {
	var n int64
	query := "SELECT COUNT(*) FROM " + table + " " + where
	_ = s.read.QueryRowContext(ctx, s.dialect.Rebind(query), args...).Scan(&n)
	return n
}

// Close stops the background cleanup. It does not close the database.
func (s *DatabaseStore) Close() error {
	close(s.stopCh)
	return nil
}

// enforceLimit deletes the oldest entries over MaxEntries.
func (s *DatabaseStore) enforceLimit(ctx context.Context) {
	if s.opts.MaxEntries <= 0 {
		return
	}
	excess := s.count(ctx, "") - s.opts.MaxEntries
	if excess <= 0 {
		return
	}

	// Two queries, because MySQL does not take a LIMIT in an IN subquery.
	query := "SELECT cache_key FROM " + table + " ORDER BY created_at ASC, cache_key ASC LIMIT ?"
	rows, err := s.read.QueryContext(ctx, s.dialect.Rebind(query), excess)
	if err != nil {
		return
	}
	var oldest []any
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			oldest = append(oldest, key)
		}
	}
	_ = rows.Close()
	if len(oldest) == 0 {
		return
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(oldest)), ", ")
	_, _ = s.exec(ctx, "DELETE FROM "+table+" WHERE cache_key IN ("+marks+")", oldest...)
}

// startCleanup deletes the expired entries at each CleanupInterval.
func (s *DatabaseStore) startCleanup() {
	ticker := time.NewTicker(s.opts.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_, _ = s.exec(context.Background(), "DELETE FROM "+table+" WHERE expires_at <= ?", time.Now().UnixMilli())
		case <-s.stopCh:
			return
		}
	}
}

// Ensure DatabaseStore implements Store
var _ Store = (*DatabaseStore)(nil)
