package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/karloscodes/cartridge/sqlite"
)

// open returns a manager on a new file. The test cleanup closes it.
func open(t *testing.T, cfg sqlite.Config) *sqlite.Manager {
	t.Helper()
	if cfg.Path == "" {
		cfg.Path = filepath.Join(t.TempDir(), "test.db")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	}
	m := sqlite.NewManager(cfg)
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// pools returns the read pool and the write pool of m.
func pools(t *testing.T, m *sqlite.Manager) (reader, writer *sql.DB) {
	t.Helper()
	writer, err := m.Writer()
	if err != nil {
		t.Fatalf("Writer: %v", err)
	}
	reader, err = m.Reader()
	if err != nil {
		t.Fatalf("Reader: %v", err)
	}
	return reader, writer
}

// openNotes returns a manager with an empty notes table, and its pools.
func openNotes(t *testing.T, cfg sqlite.Config) (m *sqlite.Manager, reader, writer *sql.DB) {
	t.Helper()
	m = open(t, cfg)
	reader, writer = pools(t, m)
	if _, err := writer.Exec("CREATE TABLE IF NOT EXISTS notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL)"); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	return m, reader, writer
}

func countNotes(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return n
}

func TestManager(t *testing.T) {
	t.Run("the reader sees what the writer committed", func(t *testing.T) {
		_, reader, writer := openNotes(t, sqlite.Config{})

		_, err := writer.Exec("INSERT INTO notes (body) VALUES ('hello')")

		if err != nil || countNotes(t, reader) != 1 {
			t.Errorf("write: %v, the reader counts %d, want 1", err, countNotes(t, reader))
		}
	})

	t.Run("the reader refuses a statement that writes", func(t *testing.T) {
		_, reader, _ := openNotes(t, sqlite.Config{})

		_, err := reader.Exec("INSERT INTO notes (body) VALUES ('x')")

		if err == nil || countNotes(t, reader) != 0 {
			t.Errorf("err = %v with %d rows, want an error and no row", err, countNotes(t, reader))
		}
	})

	t.Run("writes at the same time do not fail with a locked database", func(t *testing.T) {
		_, reader, writer := openNotes(t, sqlite.Config{BusyTimeout: 1})
		var wg sync.WaitGroup
		failures := make(chan error, 200)

		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 10 {
					if _, err := writer.Exec("INSERT INTO notes (body) VALUES ('x')"); err != nil {
						failures <- err
					}
				}
			}()
		}
		wg.Wait()
		close(failures)

		for err := range failures {
			t.Errorf("write: %v", err)
		}
		if got := countNotes(t, reader); got != 200 {
			t.Errorf("count = %d, want 200", got)
		}
	})

	t.Run("a read does not wait for an open write transaction", func(t *testing.T) {
		_, reader, writer := openNotes(t, sqlite.Config{MaxOpenConns: 1})
		if _, err := writer.Exec("INSERT INTO notes (body) VALUES ('first')"); err != nil {
			t.Fatal(err)
		}
		tx, err := writer.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec("INSERT INTO notes (body) VALUES ('second')"); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var count int
		err = reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM notes").Scan(&count)

		if err != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want the committed row with no wait", count, err)
		}
	})

	t.Run("after Close, the next use opens both pools again", func(t *testing.T) {
		m, _, writer := openNotes(t, sqlite.Config{})
		if _, err := writer.Exec("INSERT INTO notes (body) VALUES ('kept')"); err != nil {
			t.Fatal(err)
		}

		closeErr := m.Close()
		reader, writer := pools(t, m)

		_, writeErr := writer.Exec("INSERT INTO notes (body) VALUES ('more')")
		if closeErr != nil || writeErr != nil || countNotes(t, reader) != 2 {
			t.Errorf("close: %v, write: %v, count = %d, want 2", closeErr, writeErr, countNotes(t, reader))
		}
	})

	t.Run("Close on a manager that never opened returns nil", func(t *testing.T) {
		m := open(t, sqlite.Config{})

		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	t.Run("a database in memory is refused", func(t *testing.T) {
		for _, path := range []string{":memory:", "file::memory:?cache=shared", "file:x?mode=memory"} {
			m := open(t, sqlite.Config{Path: path})

			_, readerErr := m.Reader()
			_, writerErr := m.Writer()

			if readerErr == nil || writerErr == nil {
				t.Errorf("%s: reader error %v, writer error %v, want two errors", path, readerErr, writerErr)
			}
		}
	})

	t.Run("a path in a missing folder fails at open, not at the first query", func(t *testing.T) {
		m := open(t, sqlite.Config{Path: filepath.Join(t.TempDir(), "missing", "test.db")})

		_, err := m.Writer()

		if err == nil {
			t.Error("Writer returned nil, want an error")
		}
	})
}

// hold takes n connections of db at once, so each one is another connection.
func hold(t *testing.T, db *sql.DB, n int) []*sql.Conn {
	t.Helper()
	var conns []*sql.Conn
	for i := range n {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		conns = append(conns, conn)
	}
	return conns
}

func pragma(t *testing.T, conn *sql.Conn, name string) string {
	t.Helper()
	var value string
	if err := conn.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return value
}

func TestConnectionSettings(t *testing.T) {
	t.Run("every connection has the busy timeout, WAL, and synchronous NORMAL", func(t *testing.T) {
		m := open(t, sqlite.Config{MaxOpenConns: 3, BusyTimeout: 4321})
		reader, writer := pools(t, m)

		conns := append(hold(t, reader, 3), hold(t, writer, 1)...)

		for i, conn := range conns {
			timeout, journal, synchronous := pragma(t, conn, "busy_timeout"), pragma(t, conn, "journal_mode"), pragma(t, conn, "synchronous")
			if timeout != "4321" || journal != "wal" || synchronous != "1" {
				t.Errorf("conn %d: busy_timeout %s, journal_mode %s, synchronous %s, want 4321, wal, 1", i, timeout, journal, synchronous)
			}
		}
	})

	t.Run("an app pragma runs on the write connection", func(t *testing.T) {
		m := open(t, sqlite.Config{Pragmas: []string{"PRAGMA foreign_keys = ON"}})
		_, writer := pools(t, m)
		schema := `
			CREATE TABLE parents (id INTEGER PRIMARY KEY);
			CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents (id));`
		if _, err := writer.Exec(schema); err != nil {
			t.Fatal(err)
		}

		_, err := writer.Exec("INSERT INTO children (parent_id) VALUES (99)")

		if err == nil {
			t.Error("a child without its parent was saved, want the foreign key to stop it")
		}
	})

	t.Run("the app pragmas run on every read connection", func(t *testing.T) {
		m := open(t, sqlite.Config{
			MaxOpenConns: 3,
			Pragmas:      []string{"PRAGMA mmap_size = 1048576", "PRAGMA temp_store = MEMORY"},
		})
		reader, _ := pools(t, m)

		conns := hold(t, reader, 3)

		for i, conn := range conns {
			mmap, tempStore, timeout := pragma(t, conn, "mmap_size"), pragma(t, conn, "temp_store"), pragma(t, conn, "busy_timeout")
			if mmap != "1048576" || tempStore != "2" || timeout != "5000" {
				t.Errorf("conn %d: mmap_size %s, temp_store %s, busy_timeout %s, want 1048576, 2 (MEMORY), and the default 5000", i, mmap, tempStore, timeout)
			}
		}
	})

	t.Run("a broken pragma fails the open", func(t *testing.T) {
		m := open(t, sqlite.Config{Pragmas: []string{"PRAGMA mmap_size = 'oops"}})

		_, err := m.Writer()

		if err == nil {
			t.Error("Writer returned nil, want an error for the broken pragma")
		}
	})

	t.Run("the pragmas stay after Close and a new open", func(t *testing.T) {
		m := open(t, sqlite.Config{Pragmas: []string{"PRAGMA mmap_size = 1048576"}})
		pools(t, m)
		_ = m.Close()

		reader, _ := pools(t, m)

		if got := pragma(t, hold(t, reader, 1)[0], "mmap_size"); got != "1048576" {
			t.Errorf("mmap_size = %s, want 1048576", got)
		}
	})
}

func TestPlannerStatistics(t *testing.T) {
	t.Run("Close records the statistics that the next open reads", func(t *testing.T) {
		m := open(t, sqlite.Config{})
		_, writer := pools(t, m)
		for _, stmt := range []string{
			"CREATE TABLE items (id INTEGER PRIMARY KEY, kind TEXT)",
			"CREATE INDEX idx_items_kind ON items (kind)",
			"WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 500) INSERT INTO items (kind) SELECT 'k' || (i % 5) FROM n",
		} {
			if _, err := writer.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}

		closeErr := m.Close()
		reader, _ := pools(t, m)

		var rows int
		err := reader.QueryRow("SELECT COUNT(*) FROM sqlite_stat1 WHERE idx = 'idx_items_kind'").Scan(&rows)
		if closeErr != nil || err != nil || rows != 1 {
			t.Errorf("close: %v, read: %v, sqlite_stat1 rows = %d, want 1", closeErr, err, rows)
		}
	})
}

func TestCheckpointWAL(t *testing.T) {
	t.Run("a PASSIVE checkpoint works", func(t *testing.T) {
		m, _, _ := openNotes(t, sqlite.Config{})

		if err := m.CheckpointWAL("PASSIVE"); err != nil {
			t.Errorf("CheckpointWAL: %v", err)
		}
	})

	t.Run("a PASSIVE checkpoint does not wait for a replica that reads", func(t *testing.T) {
		// A replica such as Litestream keeps a read transaction open on purpose.
		path := filepath.Join(t.TempDir(), "replica.db")
		m, _, writer := openNotes(t, sqlite.Config{Path: path, BusyTimeout: 2000})
		replica, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = replica.Close() }()
		reading, err := replica.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = reading.Rollback() }()
		var n int
		if err := reading.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Exec("INSERT INTO notes (body) VALUES ('x')"); err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		err = m.CheckpointWAL("PASSIVE")
		took := time.Since(start)

		if err != nil || took > 500*time.Millisecond {
			t.Errorf("CheckpointWAL: %v after %v, want no wait for the reader", err, took)
		}
	})
}

func TestReadOnly(t *testing.T) {
	seed := func(t *testing.T, path string, wal bool) {
		t.Helper()
		db, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		statements := []string{"CREATE TABLE notes (body TEXT)", "INSERT INTO notes VALUES ('hello')"}
		if wal {
			statements = append([]string{"PRAGMA journal_mode=WAL"}, statements...)
		}
		for _, stmt := range statements {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	readOnly := func(t *testing.T, path string) (*sqlite.Manager, *bytes.Buffer) {
		t.Helper()
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		return open(t, sqlite.Config{Path: path, ReadOnly: true, Logger: logger}), &logs
	}
	readBody := func(t *testing.T, m *sqlite.Manager) string {
		t.Helper()
		reader, err := m.Reader()
		if err != nil {
			t.Fatalf("Reader: %v", err)
		}
		var body string
		if err := reader.QueryRow("SELECT body FROM notes").Scan(&body); err != nil {
			t.Fatalf("read: %v", err)
		}
		return body
	}

	t.Run("reads a WAL file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wal.db")
		seed(t, path, true)
		m, _ := readOnly(t, path)

		if got := readBody(t, m); got != "hello" {
			t.Errorf("body = %q, want hello", got)
		}
	})

	t.Run("reads a file in rollback journal mode, which a read-only connection cannot switch to WAL", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "journal.db")
		seed(t, path, false)
		m, _ := readOnly(t, path)

		if got := readBody(t, m); got != "hello" {
			t.Errorf("body = %q, want hello", got)
		}
	})

	t.Run("reads a file whose path has a space, a hash, and a percent sign", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "my data #1 100%")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "app.db")
		seed(t, path, true)
		m, _ := readOnly(t, path)

		if got := readBody(t, m); got != "hello" {
			t.Errorf("body = %q, want hello", got)
		}
	})

	t.Run("opens and closes without a warning or an error in the log", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "quiet.db")
		seed(t, path, true)
		m, logs := readOnly(t, path)

		readBody(t, m)
		err := m.Close()

		if err != nil || strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
			t.Errorf("Close: %v, log:\n%s", err, logs)
		}
	})

	t.Run("Writer returns ErrReadOnly", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "write.db")
		seed(t, path, true)
		m, _ := readOnly(t, path)

		_, err := m.Writer()

		if !errors.Is(err, sqlite.ErrReadOnly) {
			t.Errorf("err = %v, want ErrReadOnly", err)
		}
	})

	t.Run("SQLite refuses a write through the reader", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "raw.db")
		seed(t, path, true)
		m, _ := readOnly(t, path)
		reader, _ := m.Reader()

		_, err := reader.Exec("INSERT INTO notes VALUES ('nope')")

		if err == nil || readBody(t, m) != "hello" {
			t.Errorf("err = %v, body = %q, want an error and the old row", err, readBody(t, m))
		}
	})

	t.Run("a missing file is an error, not a new file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.db")
		m, _ := readOnly(t, path)

		_, err := m.Reader()

		if _, statErr := os.Stat(path); err == nil || statErr == nil {
			t.Errorf("Reader error = %v, file exists = %v, want an error and no file", err, statErr == nil)
		}
	})
}

func TestSchemaChanged(t *testing.T) {
	t.Run("after SchemaChanged, the next read has the new column", func(t *testing.T) {
		m, reader, writer := openNotes(t, sqlite.Config{MaxOpenConns: 1})
		columns := func() []string {
			rows, err := reader.Query("SELECT * FROM notes ORDER BY id")
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			defer func() { _ = rows.Close() }()
			names, err := rows.Columns()
			if err != nil {
				t.Fatalf("columns: %v", err)
			}
			return names
		}
		if before := columns(); len(before) != 2 {
			t.Fatalf("before: columns %v", before)
		}

		_, alterErr := writer.Exec("ALTER TABLE notes ADD COLUMN title TEXT DEFAULT 'none'")
		m.SchemaChanged()
		after := columns()

		if alterErr != nil || len(after) != 3 || after[2] != "title" {
			t.Errorf("alter: %v, columns after = %v, want the new column", alterErr, after)
		}
	})

	t.Run("many different read statements all work", func(t *testing.T) {
		_, reader, writer := openNotes(t, sqlite.Config{})
		if _, err := writer.Exec("INSERT INTO notes (body) VALUES ('x')"); err != nil {
			t.Fatal(err)
		}

		for i := range 300 {
			// Each length of the IN list is another SQL text.
			ids := make([]any, i+1)
			for j := range ids {
				ids[j] = j
			}
			in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
			var count int
			if err := reader.QueryRow("SELECT COUNT(*) FROM notes WHERE id IN ("+in+")", ids...).Scan(&count); err != nil {
				t.Fatalf("query %d: %v", i, err)
			}
			if i >= 1 && count != 1 {
				t.Fatalf("query %d: count = %d, want 1", i, count)
			}
		}
	})
}

func TestDataVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("stays the same without a write, and changes with one", func(t *testing.T) {
		m, _, writer := openNotes(t, sqlite.Config{})
		first, err1 := m.DataVersion(ctx)
		same, err2 := m.DataVersion(ctx)

		_, writeErr := writer.Exec("INSERT INTO notes (body) VALUES ('x')")
		changed, err3 := m.DataVersion(ctx)

		if err := errors.Join(err1, err2, err3, writeErr); err != nil {
			t.Fatal(err)
		}
		if first == "" || first != same || changed == first {
			t.Errorf("tokens %q, %q, then %q: want the same twice, then another", first, same, changed)
		}
	})

	t.Run("changes when another process writes the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		m, _, _ := openNotes(t, sqlite.Config{Path: path})
		_, _, otherWriter := openNotes(t, sqlite.Config{Path: path})
		before, err := m.DataVersion(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := otherWriter.Exec("INSERT INTO notes (body) VALUES ('from elsewhere')"); err != nil {
			t.Fatal(err)
		}
		after, err := m.DataVersion(ctx)

		if err != nil || after == before {
			t.Errorf("token %q then %q (%v), want a change", before, after, err)
		}
	})

	t.Run("a rolled back write does not change it", func(t *testing.T) {
		m, _, writer := openNotes(t, sqlite.Config{})
		before, _ := m.DataVersion(ctx)

		tx, err := writer.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = tx.Exec("INSERT INTO notes (body) VALUES ('lost')")
		_ = tx.Rollback()
		after, err := m.DataVersion(ctx)

		if err != nil || after != before {
			t.Errorf("token %q then %q (%v), want no change", before, after, err)
		}
	})

	t.Run("is new after a reopen", func(t *testing.T) {
		m := open(t, sqlite.Config{})
		before, err1 := m.DataVersion(ctx)

		_ = m.Close()
		after, err2 := m.DataVersion(ctx)

		if err1 != nil || err2 != nil || before == "" || after == before {
			t.Errorf("tokens %q and %q, errors %v %v: want two different tokens", before, after, err1, err2)
		}
	})
}

func TestIsBusyError(t *testing.T) {
	cases := map[string]bool{
		"database is locked":             true,
		"database table is locked":       true,
		"database schema is locked: x":   true,
		"no such table: busy":            false,
		"UNIQUE constraint: busybee.com": false,
	}
	for message, want := range cases {
		if got := sqlite.IsBusyError(fmt.Errorf("%s", message)); got != want {
			t.Errorf("IsBusyError(%q) = %v, want %v", message, got, want)
		}
	}
	if sqlite.IsBusyError(nil) {
		t.Error("IsBusyError(nil) = true")
	}
}
