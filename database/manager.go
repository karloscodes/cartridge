// Package database opens PostgreSQL and MySQL databases for cartridge.
package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
)

// Manager owns the connection pool of one PostgreSQL or MySQL database.
// These databases take many writers at once, so Reader and Writer return
// the same pool. It implements cartridge.DBManager.
type Manager struct {
	driver  Driver
	cfg     *Config
	logger  *slog.Logger
	db      *sql.DB
	dbMutex sync.Mutex
}

// NewManager creates a new database manager with the given driver and config.
func NewManager(driver Driver, cfg *Config, logger *slog.Logger) *Manager {
	if cfg == nil {
		cfg = DefaultConfig("")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{driver: driver, cfg: cfg, logger: logger}
}

// Reader returns the connection pool, for queries that only read.
func (m *Manager) Reader() (*sql.DB, error) { return m.connect() }

// Writer returns the same connection pool, for statements that write.
func (m *Manager) Writer() (*sql.DB, error) { return m.connect() }

// connect returns the connection pool. It opens the pool on the first
// call, and again after Close.
func (m *Manager) connect() (*sql.DB, error) {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()
	if m.db != nil {
		return m.db, nil
	}

	db, err := sql.Open(m.driver.SQLDriver(), m.driver.ConfigureDSN(m.cfg.DSN, m.cfg))
	if err != nil {
		return nil, fmt.Errorf("database: open: %w", err)
	}
	// sql.Open opens no connection. Ping does, so a wrong DSN shows now.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database: open: %w", err)
	}
	db.SetMaxOpenConns(m.cfg.MaxOpenConns)
	db.SetMaxIdleConns(m.cfg.MaxIdleConns)
	db.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)

	m.logger.Info("database connection established",
		slog.String("driver", m.driver.Name()),
		slog.Int("max_open", m.cfg.MaxOpenConns),
		slog.Int("max_idle", m.cfg.MaxIdleConns),
	)
	m.db = db
	return db, nil
}

// Close closes the connection pool.
func (m *Manager) Close() error {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()
	if m.db == nil {
		return nil
	}
	if err := m.db.Close(); err != nil {
		return fmt.Errorf("database: close: %w", err)
	}
	m.db = nil
	m.logger.Info("database connection closed", slog.String("driver", m.driver.Name()))
	return nil
}

// Driver returns the underlying driver.
func (m *Manager) Driver() Driver {
	return m.driver
}
