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
)

// Config configures the SQLite database manager.
type Config struct {
	// Path is the database file path. Required. A database in memory
	// (":memory:") is not supported: the two pools would each get their own.
	Path string

	// MaxOpenConns is the number of read connections. Default: 4. There is
	// always exactly one write connection.
	MaxOpenConns int

	// MaxIdleConns is the number of read connections kept open. Default:
	// MaxOpenConns.
	MaxIdleConns int

	// ConnMaxLifetime is the maximum connection lifetime. Default: 10 minutes.
	ConnMaxLifetime time.Duration

	// Logger for database operations. Optional.
	Logger *slog.Logger

	// BusyTimeout in milliseconds: how long SQLite waits for a lock that
	// another process holds. Default: 5000.
	BusyTimeout int

	// WriteWait is how long a write waits for the write connection before
	// it fails with ErrBusy. Default: 5 seconds.
	WriteWait time.Duration

	// Pragmas run on every new connection of both pools, after the
	// defaults. Use them for app-specific settings, such as
	// "PRAGMA mmap_size = 268435456". A pragma that fails stops the
	// connection from opening, and a read connection refuses a pragma that
	// writes. SQLite ignores an unknown pragma name without an error, so
	// check the spelling.
	Pragmas []string

	// ReadOnly opens an existing file with no write connection. Writer and
	// writes then return ErrReadOnly. SQLite still needs to create the -shm
	// file of a WAL database, so the directory must be writable.
	ReadOnly bool
}

// ErrReadOnly means a write went to a database opened with ReadOnly.
var ErrReadOnly = errors.New("sqlite: database is read-only")

// Manager opens one SQLite file with two connection pools:
//
//   - one write connection. SQLite allows one writer, so every write uses
//     this connection, one at a time;
//   - a pool of read connections, opened read-only. Readers do not wait
//     for the writer (WAL mode), and a statement that writes fails on them.
//
// So a write that does not go through the write connection is an error at
// once, not a lock failure under load. It implements cartridge.DBManager.
type Manager struct {
	cfg        Config
	logger     *slog.Logger
	mu         sync.Mutex
	opened     bool
	reader     *sql.DB
	writer     *sql.DB // nil with ReadOnly
	driverName string  // set on first open when Pragmas is not empty
}

// NewManager creates a new SQLite database manager.
func NewManager(cfg Config) *Manager {
	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 4
	}
	if cfg.MaxIdleConns <= 0 || cfg.MaxIdleConns > cfg.MaxOpenConns {
		cfg.MaxIdleConns = cfg.MaxOpenConns
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = 10 * time.Minute
	}
	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5000
	}
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = 5 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, logger: logger}
}

// Reader returns the pool of read connections. It opens the database on
// the first call. A statement that writes fails on this pool.
func (m *Manager) Reader() (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.open(); err != nil {
		return nil, err
	}
	return m.reader, nil
}

// Writer returns the pool with the one write connection. It opens the
// database on the first call. For a transaction, use cartridge.Write, which
// waits for the connection only as long as WriteWait. With ReadOnly it
// returns ErrReadOnly.
func (m *Manager) Writer() (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.ReadOnly {
		return nil, fmt.Errorf("%w: %s", ErrReadOnly, m.cfg.Path)
	}
	if err := m.open(); err != nil {
		return nil, err
	}
	return m.writer, nil
}

// WriteWait returns how long a write waits for the write connection.
func (m *Manager) WriteWait() time.Duration {
	return m.cfg.WriteWait
}

// Close closes both pools. The next Reader or Writer opens them again.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.opened {
		return nil
	}

	var errs []error
	if m.writer != nil {
		// Record what this run learned about the queries for the next start.
		m.optimize(m.writer, "PRAGMA optimize")
		errs = append(errs, m.writer.Close())
	}
	errs = append(errs, m.reader.Close())
	m.reader, m.writer, m.opened = nil, nil, false
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	return nil
}

// CheckpointWAL forces a WAL checkpoint with the given mode.
// Modes: PASSIVE, FULL, RESTART, TRUNCATE
func (m *Manager) CheckpointWAL(mode string) error {
	writer, err := m.Writer()
	if err != nil {
		return err
	}
	_, err = writer.Exec("PRAGMA wal_checkpoint(" + mode + ");")
	return err
}

// open opens the pools. The caller holds m.mu.
func (m *Manager) open() error {
	if m.opened {
		return nil
	}
	if strings.Contains(m.cfg.Path, ":memory:") || strings.Contains(m.cfg.Path, "mode=memory") {
		return fmt.Errorf("sqlite: a database in memory is not supported; use a file, such as one in t.TempDir()")
	}

	driverName := "sqlite3"
	if len(m.cfg.Pragmas) > 0 {
		if m.driverName == "" {
			m.driverName = registerDriver(m.cfg.Pragmas)
		}
		driverName = m.driverName
	}

	// The writer comes first: it creates a new file and puts it in WAL
	// mode, which a read-only connection cannot do.
	var writer *sql.DB
	if !m.cfg.ReadOnly {
		var err error
		writer, err = openPool(driverName, writerDSN(m.cfg.Path, m.cfg.BusyTimeout))
		if err != nil {
			return err
		}
		writer.SetMaxOpenConns(1)
		writer.SetMaxIdleConns(1)

		// Without statistics the query planner guesses. The SQLite docs ask
		// a long-lived connection to run this at open; it analyzes only the
		// tables that need it, so it is fast after the first run.
		m.optimize(writer, "PRAGMA optimize=0x10002")
	}

	reader, err := openPool(driverName, readerDSN(m.cfg.Path, m.cfg.BusyTimeout))
	if err != nil {
		if writer != nil {
			_ = writer.Close()
		}
		return err
	}
	reader.SetMaxOpenConns(m.cfg.MaxOpenConns)
	reader.SetMaxIdleConns(m.cfg.MaxIdleConns)
	reader.SetConnMaxLifetime(m.cfg.ConnMaxLifetime)

	m.logger.Info("sqlite connection established",
		slog.String("path", m.cfg.Path),
		slog.Int("readers", m.cfg.MaxOpenConns),
		slog.Bool("read_only", m.cfg.ReadOnly),
	)
	m.reader, m.writer, m.opened = reader, writer, true
	return nil
}

// openPool opens a pool and its first connection. sql.Open alone opens no
// connection, so a wrong path or a pragma that fails would show only on the
// first query.
func openPool(driverName, dsn string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	return db, nil
}

// optimize refreshes the query planner statistics. A failure costs only
// speed, so it logs and goes on.
func (m *Manager) optimize(db *sql.DB, pragma string) {
	if _, err := db.Exec(pragma); err != nil {
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

// fileURI turns a path into a SQLite URI, which the open mode needs. A path
// that is already a "file:" URI keeps its parameters.
func fileURI(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path
	}
	// In a URI, "?" starts the parameters, "#" a fragment, and "%" an escape.
	return "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// withParams adds driver parameters to a URI. A PRAGMA sent with Exec
// reaches only the connection that runs it; the driver applies these
// parameters to every connection the pool opens.
func withParams(uri string, params ...string) string {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	return uri + sep + strings.Join(params, "&")
}

// writerDSN opens the file for writing, in WAL mode. Each transaction takes
// the write lock when it begins (_txlock=immediate), so it cannot fail
// halfway because another process took the lock first.
func writerDSN(path string, busyTimeout int) string {
	return withParams(fileURI(path),
		fmt.Sprintf("_busy_timeout=%d", busyTimeout),
		"_synchronous=NORMAL",
		"_journal_mode=WAL",
		"_txlock=immediate",
	)
}

// readerDSN opens the file read-only. mode=ro stops writes to the file, and
// _query_only stops them in SQLite before they reach it.
func readerDSN(path string, busyTimeout int) string {
	return withParams(fileURI(path),
		"mode=ro",
		"_query_only=on",
		fmt.Sprintf("_busy_timeout=%d", busyTimeout),
		"_synchronous=NORMAL",
	)
}
