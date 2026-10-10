package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

	// ReadPool opens the file with two pools: one write connection, and
	// MaxOpenConns read-only connections. A SELECT outside a transaction
	// runs on a read connection. Every other statement and every
	// transaction runs on the write connection, one at a time. App code
	// does not change. Writes then wait for each other in Go and do not
	// fail with "database is locked" against each other, and reads never
	// wait for a write.
	//
	// Use the transaction handle for every query inside a transaction: a
	// write on the outer handle waits for the connection that the
	// transaction holds. A database in memory keeps one pool. Default:
	// false, one pool for reads and writes.
	ReadPool bool

	// ReadOnly opens an existing file read-only (mode=ro). The manager then
	// keeps the journal mode of the file, does not run PRAGMA optimize, and
	// Write returns ErrReadOnly. SQLite still needs to create the -shm file
	// of a WAL database, so the directory must be writable.
	ReadOnly bool
}

// ErrReadOnly means a write went to a database opened with ReadOnly.
var ErrReadOnly = errors.New("sqlite: database is read-only")

// Manager manages SQLite database connections with optimized settings.
type Manager struct {
	cfg        Config
	logger     *slog.Logger
	db         *gorm.DB
	dbOnce     sync.Once
	dbMutex    sync.Mutex
	writeTurn  chan struct{} // holds one token while a Write runs
	driverName string        // set on first open when Pragmas is not empty
	split      *splitPool    // set on open with ReadPool
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
	if !m.cfg.ReadOnly {
		m.optimize(m.db, "PRAGMA optimize")
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	// With two pools, sqlDB is the writer. The reader closes here, or
	// first, above, when the database is read-only and has no writer.
	if m.split != nil && m.split.reader != sqlDB {
		if err := m.split.reader.Close(); err != nil {
			return fmt.Errorf("sqlite: close: %w", err)
		}
	}
	m.split = nil

	m.db = nil
	m.dbOnce = sync.Once{}
	return nil
}

// Reader returns the pool of read-only connections of a manager with
// ReadPool. It returns nil without ReadPool, for a database in memory, and
// when the database cannot open. A statement that writes fails on it.
func (m *Manager) Reader() *sql.DB {
	if _, err := m.Connect(); err != nil {
		return nil
	}
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()
	if m.split == nil {
		return nil
	}
	return m.split.reader
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
	if m.cfg.ReadOnly {
		// A read-only connection cannot set the journal mode, and a reader
		// needs no write lock at BEGIN.
		dsn = buildDSN(readOnlyURI(m.cfg.Path), m.cfg.BusyTimeout, false, false)
	}

	// Create GORM logger
	gormLogger := database.NewGormLogger(m.logger.With(slog.String("component", "gorm")), nil)

	dialector := sqlite.Open(dsn)
	if len(m.cfg.Pragmas) > 0 {
		if m.driverName == "" {
			m.driverName = registerDriver(m.cfg.Pragmas)
		}
		dialector = sqlite.New(sqlite.Config{DriverName: m.driverName, DSN: dsn})
	}
	if m.cfg.ReadPool && !inMemory(m.cfg.Path) {
		split, err := m.openSplit()
		if err != nil {
			return err
		}
		m.split = split
		dialector = sqlite.New(sqlite.Config{Conn: split})
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

	// With two pools, openSplit set the limits of each pool.
	if m.split == nil {
		sqlDB.SetMaxOpenConns(m.cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(m.cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)
	}

	m.logger.Info("sqlite connection established",
		slog.String("path", m.cfg.Path),
		slog.Int("max_open", m.cfg.MaxOpenConns),
		slog.Int("max_idle", m.cfg.MaxIdleConns),
		slog.Bool("read_pool", m.split != nil),
	)

	// Without statistics the query planner guesses. The SQLite docs ask a
	// long-lived connection to run this at open; it analyzes only the tables
	// that need it, so it is fast after the first run. It writes the
	// statistics, so a read-only file keeps the ones it has.
	if !m.cfg.ReadOnly {
		m.optimize(db, "PRAGMA optimize=0x10002")
	}

	m.db = db
	return nil
}

// inMemory reports whether the path names a database in memory. Each
// connection to one is another database, so it cannot have two pools.
func inMemory(path string) bool {
	return strings.Contains(path, ":memory:") || strings.Contains(path, "mode=memory")
}

// openSplit opens the write connection and the read-only pool. The writer
// comes first: it creates a new file and puts it in WAL mode, which a
// read-only connection cannot do.
func (m *Manager) openSplit() (*splitPool, error) {
	driverName := "sqlite3"
	if m.driverName != "" {
		driverName = m.driverName
	}
	open := func(dsn string, conns int) (*sql.DB, error) {
		db, err := sql.Open(driverName, dsn)
		if err != nil {
			return nil, fmt.Errorf("sqlite: open: %w", err)
		}
		// sql.Open opens no connection. Ping does, so a wrong path shows now.
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite: open: %w", err)
		}
		db.SetMaxOpenConns(conns)
		db.SetMaxIdleConns(conns)
		db.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)
		return db, nil
	}

	split := &splitPool{path: m.cfg.Path}
	if !m.cfg.ReadOnly {
		writer, err := open(buildDSN(m.cfg.Path, m.cfg.BusyTimeout, m.cfg.EnableWAL, m.cfg.TxImmediate), 1)
		if err != nil {
			return nil, err
		}
		split.writer = writer
	}
	// _query_only stops a write in SQLite before it reaches the file, which
	// mode=ro guards. A reader needs no write lock at BEGIN.
	reader, err := open(buildDSN(readOnlyURI(m.cfg.Path), m.cfg.BusyTimeout, false, false)+"&_query_only=on", m.cfg.MaxOpenConns)
	if err != nil {
		if split.writer != nil {
			_ = split.writer.Close()
		}
		return nil, err
	}
	split.reader = reader
	return split, nil
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
		// Through the driver interface: without cgo, go-sqlite3 has a stub
		// SQLiteConn with no Exec, and CGO_ENABLED=0 builds must compile.
		execer, ok := any(conn).(driver.ExecerContext)
		if !ok {
			return fmt.Errorf("sqlite: the driver cannot run pragmas (built without cgo?)")
		}
		for _, pragma := range pragmas {
			if _, err := execer.ExecContext(context.Background(), pragma, nil); err != nil {
				return fmt.Errorf("sqlite: %s: %w", pragma, err)
			}
		}
		return nil
	}})
	return name
}

// readOnlyURI turns a path into a SQLite URI that opens the file read-only.
// A path that is already a "file:" URI keeps its parameters.
func readOnlyURI(path string) string {
	if !strings.HasPrefix(path, "file:") {
		// In a URI, "?" starts the parameters, "#" a fragment, and "%" an escape.
		path = "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "mode=ro"
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
