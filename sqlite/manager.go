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
	//
	// With ReadPool they run on the write connection and on every read
	// connection. A read connection refuses a pragma that writes.
	Pragmas []string

	// ReadPool opens the file with two pools: one write connection, and
	// MaxOpenConns read-only connections. GORM's read methods (Find,
	// First, Take, Scan, Count, Pluck, Rows, Row) run on a read connection.
	// Create, Update, Delete, Exec, and every transaction run on the write
	// connection, one at a time. The method decides, not the SQL text, and
	// app code does not change. Writes then wait for each other in Go and
	// do not fail with "database is locked" against each other, and reads
	// never wait for a write.
	//
	// A statement that writes through a read method, such as
	// Raw("UPDATE ... RETURNING id").Scan(&id), is refused outside a
	// transaction: run it in one.
	//
	// Use the transaction handle for every query inside a transaction: a
	// write on the outer handle waits for the connection that the
	// transaction holds. A database in memory keeps one pool. Default:
	// false, one pool for reads and writes.
	//
	// With ReadPool, Write needs no queue of its own: it waits for the
	// write connection, at most WriteWait.
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
	reader     *sql.DB       // set on open with ReadPool
	writer     *sql.DB       // set on open with ReadPool, nil with ReadOnly

	versionMu    sync.Mutex
	versionDB    *sql.DB   // one read-only connection, for DataVersion
	versionConn  *sql.Conn // pinned: data_version is per connection
	versionNonce string    // new for each pinned connection
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
	// above, when the database is read-only and has no writer.
	if m.reader != nil {
		readPools.Delete(m.reader)
		if m.writer != nil {
			readPools.Delete(m.writer)
		}
		if m.reader != sqlDB {
			if err := m.reader.Close(); err != nil {
				return fmt.Errorf("sqlite: close: %w", err)
			}
		}
	}
	m.reader, m.writer = nil, nil
	m.closeVersion()

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
	return m.reader
}

// SchemaChanged tells the manager that the schema changed, for example
// after a migration. Call it before the app serves requests again.
//
// A connection that was open during the change still plans its next
// "SELECT *" on a changed table with the old columns: SQLite notices the
// change only when that statement runs. So SchemaChanged closes the idle
// connections of the read pool, and new ones open with the new schema.
// Without ReadPool it does nothing. MigrateDatabase of cartridge.App calls
// it.
func (m *Manager) SchemaChanged() {
	m.dbMutex.Lock()
	defer m.dbMutex.Unlock()
	if m.reader == nil {
		return
	}
	// With no idle connection allowed, database/sql closes them all.
	m.reader.SetMaxIdleConns(0)
	m.reader.SetMaxIdleConns(m.cfg.MaxOpenConns)
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

	dsn := m.dsn(m.cfg.Path, m.cfg.EnableWAL, m.cfg.TxImmediate)
	if m.cfg.ReadOnly {
		// A read-only connection cannot set the journal mode, and a reader
		// needs no write lock at BEGIN.
		dsn = m.dsn(readOnlyURI(m.cfg.Path), false, false)
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
	usePools := m.cfg.ReadPool && !inMemory(m.cfg.Path)
	if usePools {
		if err := m.openPools(); err != nil {
			return err
		}
		// GORM runs on the write connection. A read-only database has
		// none, so there GORM runs on the read pool.
		conn := m.writer
		if conn == nil {
			conn = m.reader
		}
		dialector = sqlite.New(sqlite.Config{Conn: conn})
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

	if usePools {
		readPools.Store(m.reader, m.reader)
		if m.writer != nil {
			readPools.Store(m.writer, m.reader)
			if err := routeReads(db, m.writer, m.reader); err != nil {
				return fmt.Errorf("sqlite: read pool: %w", err)
			}
		}
	}

	// With two pools, openPools set the limits of each pool.
	if !usePools {
		sqlDB.SetMaxOpenConns(m.cfg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(m.cfg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)
	}

	m.logger.Info("sqlite connection established",
		slog.String("path", m.cfg.Path),
		slog.Int("max_open", m.cfg.MaxOpenConns),
		slog.Int("max_idle", m.cfg.MaxIdleConns),
		slog.Bool("read_pool", usePools),
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

// openPools opens the write connection and the read-only pool. The writer
// comes first: it creates a new file and puts it in WAL mode, which a
// read-only connection cannot do.
func (m *Manager) openPools() error {
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

	var writer *sql.DB
	if !m.cfg.ReadOnly {
		var err error
		if writer, err = open(m.dsn(m.cfg.Path, m.cfg.EnableWAL, m.cfg.TxImmediate), 1); err != nil {
			return err
		}
	}
	reader, err := open(m.readerDSN(), m.cfg.MaxOpenConns)
	if err != nil {
		if writer != nil {
			_ = writer.Close()
		}
		return err
	}
	m.reader, m.writer = reader, writer
	return nil
}

// dsn builds the DSN of a connection.
func (m *Manager) dsn(path string, wal, txImmediate bool) string {
	return buildDSN(path, m.cfg.BusyTimeout, wal, txImmediate)
}

// readerDSN opens the file read-only. _query_only stops a write in SQLite
// before it reaches the file, which mode=ro guards. A reader needs no write
// lock at BEGIN.
func (m *Manager) readerDSN() string {
	return m.dsn(readOnlyURI(m.cfg.Path), false, false) + "&_query_only=on"
}

// DataVersion returns a token that changes after every commit to the
// database file, by this process or by another one. Use it to keep a value
// until the data changes: put the token in a cache key or in an ETag.
//
//	version, err := m.DataVersion(ctx)
//	key := "dashboard:" + userID + ":" + version
//
// Compare tokens only for equality. A token is good for the life of the
// process: do not keep it in a cache that outlives a restart. It reads
// SQLite's PRAGMA data_version on one connection that stays open, because
// that value is per connection.
func (m *Manager) DataVersion(ctx context.Context) (string, error) {
	if inMemory(m.cfg.Path) {
		return "", fmt.Errorf("sqlite: DataVersion needs a database file")
	}
	if _, err := m.Connect(); err != nil {
		return "", err
	}

	m.versionMu.Lock()
	defer m.versionMu.Unlock()
	if m.versionConn == nil {
		driverName := "sqlite3"
		if m.driverName != "" {
			driverName = m.driverName
		}
		db, err := sql.Open(driverName, m.readerDSN())
		if err != nil {
			return "", fmt.Errorf("sqlite: data version: %w", err)
		}
		db.SetMaxOpenConns(1)
		conn, err := db.Conn(ctx)
		if err != nil {
			_ = db.Close()
			return "", fmt.Errorf("sqlite: data version: %w", err)
		}
		// Values of two connections do not compare, so each pinned
		// connection gets its own prefix.
		m.versionDB, m.versionConn = db, conn
		m.versionNonce = fmt.Sprintf("%x", time.Now().UnixNano())
	}

	var version int64
	if err := m.versionConn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version); err != nil {
		// The next call opens a new connection.
		m.closeVersionLocked()
		return "", fmt.Errorf("sqlite: data version: %w", err)
	}
	return fmt.Sprintf("%s-%d", m.versionNonce, version), nil
}

func (m *Manager) closeVersion() {
	m.versionMu.Lock()
	defer m.versionMu.Unlock()
	m.closeVersionLocked()
}

func (m *Manager) closeVersionLocked() {
	if m.versionConn != nil {
		_ = m.versionConn.Close()
		_ = m.versionDB.Close()
	}
	m.versionDB, m.versionConn = nil, nil
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
