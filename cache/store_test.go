package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/karloscodes/cartridge/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// storeTestSuite runs the same tests against both store implementations
func runStoreTests(t *testing.T, store cache.Store, name string) {
	ctx := context.Background()

	t.Run(name+"/ReadWriteBasic", func(t *testing.T) {
		key := "test-key-" + name
		value := []byte("test-value")

		// Write
		err := store.Write(ctx, key, value)
		require.NoError(t, err)

		// Read
		got, ok := store.Read(ctx, key)
		assert.True(t, ok)
		assert.Equal(t, value, got)
	})

	t.Run(name+"/ReadMiss", func(t *testing.T) {
		got, ok := store.Read(ctx, "nonexistent-key")
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run(name+"/Exist", func(t *testing.T) {
		key := "exist-key-" + name
		value := []byte("value")

		assert.False(t, store.Exist(ctx, key))

		err := store.Write(ctx, key, value)
		require.NoError(t, err)

		assert.True(t, store.Exist(ctx, key))
	})

	t.Run(name+"/Delete", func(t *testing.T) {
		key := "delete-key-" + name
		value := []byte("value")

		err := store.Write(ctx, key, value)
		require.NoError(t, err)

		err = store.Delete(ctx, key)
		require.NoError(t, err)

		_, ok := store.Read(ctx, key)
		assert.False(t, ok)
	})

	t.Run(name+"/DeleteByPrefix", func(t *testing.T) {
		prefix := "prefix-" + name + "-"

		// Write multiple entries with prefix
		for i := 0; i < 3; i++ {
			err := store.Write(ctx, prefix+string(rune('a'+i)), []byte("value"))
			require.NoError(t, err)
		}

		// Write one without prefix
		err := store.Write(ctx, "other-"+name, []byte("value"))
		require.NoError(t, err)

		// Delete by prefix
		count, err := store.DeleteByPrefix(ctx, prefix)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// Verify prefix entries are gone
		for i := 0; i < 3; i++ {
			_, ok := store.Read(ctx, prefix+string(rune('a'+i)))
			assert.False(t, ok)
		}

		// Verify other entry still exists
		_, ok := store.Read(ctx, "other-"+name)
		assert.True(t, ok)
	})

	t.Run(name+"/Clear", func(t *testing.T) {
		// Write some entries
		for i := 0; i < 3; i++ {
			err := store.Write(ctx, "clear-"+name+"-"+string(rune('a'+i)), []byte("value"))
			require.NoError(t, err)
		}

		// Clear all
		err := store.Clear(ctx)
		require.NoError(t, err)

		// Verify stats show zero entries
		stats := store.Stats(ctx)
		assert.Equal(t, int64(0), stats.Entries)
	})

	t.Run(name+"/Stats", func(t *testing.T) {
		// Clear first
		err := store.Clear(ctx)
		require.NoError(t, err)

		// Write some entries
		for i := 0; i < 5; i++ {
			err := store.Write(ctx, "stats-"+name+"-"+string(rune('a'+i)), []byte("value"))
			require.NoError(t, err)
		}

		stats := store.Stats(ctx)
		assert.Equal(t, int64(5), stats.Entries)
		assert.NotEmpty(t, stats.Backend)
	})
}

func TestMemoryStore(t *testing.T) {
	store := cache.NewMemoryStore(
		cache.WithTTL(1*time.Hour),
		cache.WithCleanupInterval(0), // Disable background cleanup for tests
	)
	defer store.Close()

	runStoreTests(t, store, "MemoryStore")
}

func TestMemoryStoreExpiration(t *testing.T) {
	store := cache.NewMemoryStore(
		cache.WithTTL(50*time.Millisecond),
		cache.WithCleanupInterval(0),
	)
	defer store.Close()

	ctx := context.Background()

	err := store.Write(ctx, "expiring-key", []byte("value"))
	require.NoError(t, err)

	// Should exist immediately
	_, ok := store.Read(ctx, "expiring-key")
	assert.True(t, ok)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired now
	_, ok = store.Read(ctx, "expiring-key")
	assert.False(t, ok)
}

func TestMemoryStoreMaxEntries(t *testing.T) {
	store := cache.NewMemoryStore(
		cache.WithTTL(1*time.Hour),
		cache.WithMaxEntries(3),
		cache.WithCleanupInterval(0),
	)
	defer store.Close()

	ctx := context.Background()

	// Write 5 entries
	for i := 0; i < 5; i++ {
		err := store.Write(ctx, string(rune('a'+i)), []byte("value"))
		require.NoError(t, err)
	}

	stats := store.Stats(ctx)
	assert.Equal(t, int64(3), stats.Entries, "Should have max 3 entries")

	// Oldest entries should be evicted (FIFO)
	_, ok := store.Read(ctx, "a")
	assert.False(t, ok, "Entry 'a' should be evicted")

	_, ok = store.Read(ctx, "b")
	assert.False(t, ok, "Entry 'b' should be evicted")

	// Newest entries should remain
	_, ok = store.Read(ctx, "e")
	assert.True(t, ok, "Entry 'e' should remain")
}

func TestDatabaseStore(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	store, err := cache.NewDatabaseStore(db,
		cache.WithTTL(1*time.Hour),
		cache.WithCleanupInterval(0),
	)
	require.NoError(t, err)
	defer store.Close()

	runStoreTests(t, store, "DatabaseStore")
}

func TestDatabaseStoreExpiration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	store, err := cache.NewDatabaseStore(db,
		cache.WithTTL(100*time.Millisecond),
		cache.WithCleanupInterval(0),
	)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	err = store.Write(ctx, "expiring-key", []byte("value"))
	require.NoError(t, err)

	// Should exist immediately
	_, ok := store.Read(ctx, "expiring-key")
	assert.True(t, ok, "Entry should exist immediately after write")

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired now
	_, ok = store.Read(ctx, "expiring-key")
	assert.False(t, ok, "Entry should be expired after TTL")
}

func TestDatabaseStoreMaxEntries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	store, err := cache.NewDatabaseStore(db,
		cache.WithTTL(1*time.Hour),
		cache.WithMaxEntries(3),
		cache.WithCleanupInterval(0),
	)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write 5 entries with small delays to ensure ordering
	for i := 0; i < 5; i++ {
		err := store.Write(ctx, string(rune('a'+i)), []byte("value"))
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure distinct timestamps
	}

	stats := store.Stats(ctx)
	assert.Equal(t, int64(3), stats.Entries, "Should have max 3 entries")

	// Newest entries should remain
	_, ok := store.Read(ctx, "e")
	assert.True(t, ok, "Entry 'e' should remain")
}

func TestDefaultOptions(t *testing.T) {
	opts := cache.DefaultOptions()

	assert.Equal(t, 14*24*time.Hour, opts.TTL, "Default TTL should be 2 weeks")
	assert.Equal(t, int64(0), opts.MaxEntries, "Default max entries should be unlimited")
	assert.Equal(t, 1*time.Hour, opts.CleanupInterval, "Default cleanup interval should be 1 hour")
	assert.Equal(t, 100, opts.CleanupBatchSize, "Default cleanup batch size should be 100")
}
