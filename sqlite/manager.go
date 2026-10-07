package sqlite

import (
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-sqlite3"
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

	// WriteWait is how long Write waits for its turn before it returns
	// ErrBusy. Default: 5 seconds.
	WriteWait time.Duration

	// Pragmas run on every new connection, after the defaults. Use them for
	// app-specific settings, such as "PRAGMA mmap_size = 268435456". A
	// pragma that fails stops the connection from opening. SQLite ignores
	// an unknown pragma name without an error, so check the spelling.
	Pragmas []string
}

// Manager manages SQLite database connections with optimized settings.
type Manager struct {
	cfg        Config
	logger     *slog.Logger
	db         *gorm.DB
	dbOnce     sync.Once
	dbMutex    sync.Mutex
	writeTurn  chan struct{} // holds one token while a Write runs
	driverName string        // set on first open when Pragmas is not empty
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
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = 5 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		cfg:       cfg,
		logger:    logger,
		writeTurn: make(chan struct{}, 1),
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

	// Record what this run learned about the queries for the next start.
	m.optimize(m.db, "PRAGMA optimize")

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

	dsn := buildDSN(m.cfg.Path, m.cfg.BusyTimeout, m.cfg.EnableWAL, m.cfg.TxImmediate)

	// Create GORM logger
	gormLogger := database.NewGormLogger(m.logger.With(slog.String("component", "gorm")), nil)

	dialector := sqlite.Open(dsn)
	if len(m.cfg.Pragmas) > 0 {
		if m.driverName == "" {
			m.driverName = registerDriver(m.cfg.Pragmas)
		}
		dialector = sqlite.New(sqlite.Config{DriverName: m.driverName, DSN: dsn})
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return fmt.Errorf("sqlite: open: %w", err)
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

	// Without statistics the query planner guesses. The SQLite docs ask a
	// long-lived connection to run this at open; it analyzes only the tables
	// that need it, so it is fast after the first run.
	m.optimize(db, "PRAGMA optimize=0x10002")

	m.db = db
	return nil
}

// optimize refreshes the query planner statistics. A failure costs only
// speed, so it logs and goes on.
func (m *Manager) optimize(db *gorm.DB, pragma string) {
	if err := db.Exec(pragma).Error; err != nil {
		m.logger.Warn("sqlite: optimize failed", slog.String("pragma", pragma), slog.Any("error", err))
	}
}

var driverSeq atomic.Int64

// registerDriver registers a driver that runs pragmas on each connection it
// opens. A PRAGMA sent through the pool reaches only one connection, and the
// DSN has no form for most pragmas.
func registerDriver(pragmas []string) string {
	pragmas = slices.Clone(pragmas)
	name := fmt.Sprintf("sqlite3_cartridge_%d", driverSeq.Add(1))
	sql.Register(name, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		for _, pragma := range pragmas {
			if _, err := conn.Exec(pragma, nil); err != nil {
				return fmt.Errorf("sqlite: %s: %w", pragma, err)
			}
		}
		return nil
	}})
	return name
}

// buildDSN adds the connection settings to the path as driver parameters. A
// PRAGMA sent with Exec reaches only the connection that runs it; the driver
// applies DSN parameters to every connection the pool opens.
func buildDSN(path string, busyTimeout int, wal, txImmediate bool) string {
	params := []string{
		fmt.Sprintf("_busy_timeout=%d", busyTimeout),
		"_synchronous=NORMAL",
	}
	if wal {
		params = append(params, "_journal_mode=WAL")
	}
	if txImmediate {
		params = append(params, "_txlock=immediate")
	}

	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + strings.Join(params, "&")
}
