package database

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fileDriver opens a SQLite file through database/sql, so the test needs
// no database server. The PostgreSQL and MySQL drivers have their own tests.
type fileDriver struct{}

func (fileDriver) Name() string                                { return "test" }
func (fileDriver) SQLDriver() string                           { return "sqlite3" }
func (fileDriver) ConfigureDSN(dsn string, cfg *Config) string { return dsn }

func TestManager(t *testing.T) {
	newManager := func(t *testing.T) *Manager {
		t.Helper()
		m := NewManager(fileDriver{}, DefaultConfig(filepath.Join(t.TempDir(), "test.db")), testLogger())
		t.Cleanup(func() { _ = m.Close() })
		return m
	}

	t.Run("Reader and Writer return the same pool", func(t *testing.T) {
		m := newManager(t)

		reader, readerErr := m.Reader()
		writer, writerErr := m.Writer()

		if readerErr != nil || writerErr != nil || reader == nil || reader != writer {
			t.Errorf("reader %p (%v), writer %p (%v), want one pool", reader, readerErr, writer, writerErr)
		}
	})

	t.Run("the pool runs a query", func(t *testing.T) {
		m := newManager(t)
		reader, err := m.Reader()
		if err != nil {
			t.Fatal(err)
		}

		var got int
		err = reader.QueryRow("SELECT 1").Scan(&got)

		if err != nil || got != 1 {
			t.Errorf("got %d (%v), want 1", got, err)
		}
	})

	t.Run("a wrong DSN fails at the first use, not at the first query", func(t *testing.T) {
		m := NewManager(fileDriver{}, DefaultConfig(filepath.Join(t.TempDir(), "no-such-dir", "test.db")), testLogger())

		_, err := m.Reader()

		if err == nil {
			t.Error("Reader returned nil, want an error")
		}
	})

	t.Run("after Close, the next use opens the pool again", func(t *testing.T) {
		m := newManager(t)
		first, _ := m.Reader()
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}

		second, err := m.Reader()

		if err != nil || second == nil || second == first {
			t.Errorf("got %p after %p (%v), want a new pool", second, first, err)
		}
	})

	t.Run("Driver returns the driver of the manager", func(t *testing.T) {
		m := NewManager(fileDriver{}, nil, nil)

		got := m.Driver().Name()

		if got != "test" {
			t.Errorf("got %q, want test", got)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("postgres://localhost/app")

	if cfg.DSN != "postgres://localhost/app" || cfg.MaxOpenConns != 10 || cfg.MaxIdleConns != 5 {
		t.Errorf("got %+v", cfg)
	}
	if cfg.Postgres.SSLMode != "prefer" || cfg.Postgres.Timezone != "UTC" {
		t.Errorf("got %+v", cfg.Postgres)
	}
}
