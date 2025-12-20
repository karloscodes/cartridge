package sqlite

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/database"
)

// Config configures the SQLite database manager.
type Config struct {
	// Path is the database file path. Required.
	Path string

	// MaxOpenConns is the maximum number of open connections. Default: 1.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections. Default: 1.
	MaxIdleConns int

	// ConnMaxLifetime is the maximum connection lifetime. Default: 10 minutes.
	ConnMaxLifetime time.Duration

	// Logger for database operations. Optional.
	Logger *slog.Logger

	// BusyTimeout in milliseconds. Default: 5000.
	BusyTimeout int

	// EnableWAL enables Write-Ahead Logging. Default: true.
	EnableWAL bool

	// TxImmediate uses immediate transaction locking. Default: true.
	// This prevents SQLITE_BUSY errors in concurrent write scenarios.
	TxImmediate bool
}

// Manager manages SQLite database connections with optimized settings.
type Manager struct {
	cfg     Config
	logger  *slog.Logger
	db      *gorm.DB
	dbOnce  sync.Once
	dbMutex sync.Mutex
}

// NewManager creates a new SQLite database manager.
func NewManager(cfg Config) *Manager {
	// Apply defaults
	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 1
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 1
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = 10 * time.Minute
	}
	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5000
	}
	if !cfg.EnableWAL {
		cfg.EnableWAL = true // Default to WAL mode
	}
	if !cfg.TxImmediate {
		cfg.TxImmediate = true // Default to immediate transactions
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
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

	sqlDB, err := m.db.DB()
	if err != nil {
		return fmt.Errorf("sqlite: access sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}

	m.db = nil
	m.dbOnce = sync.Once{}
	return nil
}

// CheckpointWAL forces a WAL checkpoint with the given mode.
// Modes: PASSIVE, FULL, RESTART, TRUNCATE
func (m *Manager) CheckpointWAL(mode string) error {
	conn, err := m.Connect()
	if err != nil {
		return err
	}
	return conn.Exec("PRAGMA wal_checkpoint(" + mode + ");").Error
}

func (m *Manager) open() error {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()

	if m.db != nil {
		return nil
	}

	// Build DSN with options
	dsn := m.cfg.Path
	if m.cfg.TxImmediate {
		dsn += "?_txlock=immediate"
	}

	// Create GORM logger
	gormLogger := database.NewGormLogger(m.logger.With(slog.String("component", "gorm")), nil)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return fmt.Errorf("sqlite: open: %w", err)
	}

	// Apply pragmas
	if err := m.applyPragmas(db); err != nil {
		return err
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("sqlite: access sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(m.cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(m.cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)

	m.logger.Info("sqlite connection established",
		slog.String("path", m.cfg.Path),
		slog.Int("max_open", m.cfg.MaxOpenConns),
		slog.Int("max_idle", m.cfg.MaxIdleConns),
	)

	m.db = db
	return nil
}

func (m *Manager) applyPragmas(db *gorm.DB) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", m.cfg.BusyTimeout),
		"PRAGMA synchronous = NORMAL",
		"PRAGMA temp_store = MEMORY",
	}

	if m.cfg.EnableWAL {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}

	for _, pragma := range pragmas {
		if err := db.Exec(pragma).Error; err != nil {
			m.logger.Error("failed to apply pragma", slog.String("pragma", pragma), slog.Any("error", err))
			return fmt.Errorf("sqlite: apply pragma %s: %w", pragma, err)
		}
	}

	return nil
}
