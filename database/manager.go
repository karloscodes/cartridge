package database

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Manager manages database connections using a pluggable driver.
type Manager struct {
	driver  Driver
	cfg     *Config
	logger  *slog.Logger
	db      *gorm.DB
	dbOnce  sync.Once
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

	return &Manager{
		driver: driver,
		cfg:    cfg,
		logger: logger,
	}
}

// Connect returns a GORM database instance, initializing on first call.
func (m *Manager) Connect() (*gorm.DB, error) {
	var err error
	m.dbOnce.Do(func() {
		err = m.open()
	})
	if err != nil {
		return nil, err
	}
	return m.db.Session(&gorm.Session{}), nil
}

// GetConnection implements DBManager interface.
// Returns nil if connection fails.
func (m *Manager) GetConnection() *gorm.DB {
	db, err := m.Connect()
	if err != nil {
		m.logger.Error("failed to get database connection", slog.Any("error", err))
		return nil
	}
	return db
}

// Close closes the database connection.
func (m *Manager) Close() error {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()

	if m.db == nil {
		return nil
	}

	// Run driver-specific cleanup
	if err := m.driver.Close(m.db, m.logger); err != nil {
		m.logger.Warn("driver cleanup error", slog.Any("error", err))
	}

	sqlDB, err := m.db.DB()
	if err != nil {
		return fmt.Errorf("database: access sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("database: close: %w", err)
	}

	m.db = nil
	m.dbOnce = sync.Once{}
	m.logger.Info("database connection closed", slog.String("driver", m.driver.Name()))
	return nil
}

// CheckpointWAL forces a WAL checkpoint (SQLite only).
func (m *Manager) CheckpointWAL(mode string) error {
	if !m.driver.SupportsCheckpoint() {
		return nil // No-op for non-SQLite drivers
	}

	conn, err := m.Connect()
	if err != nil {
		return err
	}
	return m.driver.Checkpoint(conn, mode)
}

// Driver returns the underlying driver.
func (m *Manager) Driver() Driver {
	return m.driver
}

func (m *Manager) open() error {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()

	if m.db != nil {
		return nil
	}

	// Configure DSN with driver-specific options
	dsn := m.driver.ConfigureDSN(m.cfg.DSN, m.cfg)

	// Create GORM logger
	gormLogger := NewGormLogger(m.logger.With(slog.String("component", "gorm")), nil)

	// Open connection using driver's dialector
	db, err := gorm.Open(m.driver.Open(dsn), &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return fmt.Errorf("database: open: %w", err)
	}

	// Run driver-specific post-connection setup
	if err := m.driver.AfterConnect(db, m.cfg, m.logger); err != nil {
		return err
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database: access sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(m.cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(m.cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)

	m.logger.Info("database connection established",
		slog.String("driver", m.driver.Name()),
		slog.Int("max_open", m.cfg.MaxOpenConns),
		slog.Int("max_idle", m.cfg.MaxIdleConns),
	)

	m.db = db
	return nil
}
