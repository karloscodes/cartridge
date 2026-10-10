package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge/sqlite"
)

type poolNote struct {
	ID     uint
	Body   string
	Status string `gorm:"default:new"`
}

// newPoolManager opens a database with a read pool and the notes table.
func newPoolManager(t *testing.T, cfg sqlite.Config) (*sqlite.Manager, *gorm.DB) {
	t.Helper()
	if cfg.Path == "" {
		cfg.Path = filepath.Join(t.TempDir(), "pool.db")
	}
	cfg.ReadPool = true
	m := sqlite.NewManager(cfg)
	t.Cleanup(func() { _ = m.Close() })
	db, err := m.Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !cfg.ReadOnly {
		if err := db.AutoMigrate(&poolNote{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return m, db
}

func TestReadPool(t *testing.T) {
	t.Run("GORM code reads and writes as before", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		note := poolNote{Body: "hello"}

		createErr := db.Create(&note).Error
		updateErr := db.Model(&note).Update("body", "changed").Error
		var found poolNote
		findErr := db.First(&found, note.ID).Error
		var count int64
		countErr := db.Model(&poolNote{}).Where("body = ?", "changed").Count(&count).Error
		deleteErr := db.Delete(&note).Error

		if err := errors.Join(createErr, updateErr, findErr, countErr, deleteErr); err != nil {
			t.Fatal(err)
		}
		// Status has a database default, so GORM reads it back with the insert.
		if note.ID == 0 || note.Status != "new" || found.Body != "changed" || count != 1 {
			t.Errorf("note = %+v, found = %+v, count = %d", note, found, count)
		}
	})

	t.Run("a transaction reads its own writes, and a rollback leaves nothing", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		stop := errors.New("stop")
		var inside int64

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&poolNote{Body: "draft"}).Error; err != nil {
				return err
			}
			tx.Model(&poolNote{}).Count(&inside)
			return stop
		})

		var after int64
		db.Model(&poolNote{}).Count(&after)
		if !errors.Is(err, stop) || inside != 1 || after != 0 {
			t.Errorf("inside = %d, after = %d, err = %v, want 1, 0, and the error", inside, after, err)
		}
	})

	t.Run("writes at the same time do not fail with a locked database", func(t *testing.T) {
		// One millisecond of busy_timeout: two connections that write at
		// once would fail. One write connection cannot collide with itself.
		_, db := newPoolManager(t, sqlite.Config{MaxOpenConns: 8, BusyTimeout: 1})
		var wg sync.WaitGroup
		errs := make(chan error, 200)

		for i := range 40 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 5 {
					if err := db.Create(&poolNote{Body: "note"}).Error; err != nil {
						errs <- err
					}
					var n int64
					if err := db.Model(&poolNote{}).Where("id > ?", i).Count(&n).Error; err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Fatalf("a statement failed: %v", err)
		}
		var count int64
		db.Model(&poolNote{}).Count(&count)
		if count != 200 {
			t.Errorf("count = %d, want 200", count)
		}
	})

	t.Run("a read does not wait for an open write transaction", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{MaxOpenConns: 1})
		if err := db.Create(&poolNote{Body: "first"}).Error; err != nil {
			t.Fatal(err)
		}
		inWrite, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- m.Write(context.Background(), func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return tx.Create(&poolNote{Body: "second"}).Error
			})
		}()
		<-inWrite

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var count int64
		readErr := db.WithContext(ctx).Model(&poolNote{}).Count(&count).Error
		close(release)

		if readErr != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want the committed row with no wait", count, readErr)
		}
		if err := <-done; err != nil {
			t.Errorf("write: %v", err)
		}
	})

	t.Run("the reader refuses a statement that writes", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{})
		reader := m.Reader()
		if reader == nil {
			t.Fatal("Reader returned nil")
		}

		_, err := reader.Exec("INSERT INTO pool_notes (body) VALUES ('x')")

		if err == nil {
			t.Error("a write on the reader worked, want an error")
		}
	})

	t.Run("a read-only database reads, and refuses a write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		writer, db := newPoolManager(t, sqlite.Config{Path: path})
		if err := db.Create(&poolNote{Body: "seed"}).Error; err != nil {
			t.Fatal(err)
		}
		_ = writer.Close()
		readOnlyManager, readOnly := newPoolManager(t, sqlite.Config{Path: path, ReadOnly: true})

		var count int64
		readErr := readOnly.Model(&poolNote{}).Count(&count).Error
		writeErr := readOnly.Create(&poolNote{Body: "x"}).Error

		managerErr := readOnlyManager.Write(context.Background(), func(tx *gorm.DB) error { return nil })
		if readErr != nil || count != 1 || writeErr == nil || !errors.Is(managerErr, sqlite.ErrReadOnly) {
			t.Errorf("count = %d (%v), write error = %v, Write error = %v, want 1, an error, and ErrReadOnly", count, readErr, writeErr, managerErr)
		}
	})

	t.Run("after Close, the next use opens both pools again", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "kept"}).Error; err != nil {
			t.Fatal(err)
		}

		closeErr := m.Close()
		again, connectErr := m.Connect()

		if closeErr != nil || connectErr != nil {
			t.Fatalf("close: %v, connect: %v", closeErr, connectErr)
		}
		var count int64
		if err := again.Model(&poolNote{}).Count(&count).Error; err != nil || count != 1 {
			t.Errorf("count = %d, err = %v", count, err)
		}
		if err := again.Create(&poolNote{Body: "more"}).Error; err != nil {
			t.Errorf("write after reopen: %v", err)
		}
	})

	t.Run("a database in memory keeps one pool", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: ":memory:", ReadPool: true})
		t.Cleanup(func() { _ = m.Close() })
		db, err := m.Connect()
		if err != nil {
			t.Fatal(err)
		}

		migrateErr := db.AutoMigrate(&poolNote{})
		createErr := db.Create(&poolNote{Body: "x"}).Error

		if migrateErr != nil || createErr != nil || m.Reader() != nil {
			t.Errorf("migrate: %v, create: %v, has a reader: %v", migrateErr, createErr, m.Reader() != nil)
		}
	})

	t.Run("without ReadPool, the manager has no reader", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "one.db")})
		t.Cleanup(func() { _ = m.Close() })

		if m.Reader() != nil {
			t.Error("Reader returned a pool, want nil")
		}
	})
}

func TestWriteWithReadPool(t *testing.T) {
	ctx := context.Background()

	t.Run("commits, and rolls back on an error", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		stop := errors.New("stop")

		okErr := m.Write(ctx, func(tx *gorm.DB) error { return tx.Create(&poolNote{Body: "kept"}).Error })
		failErr := m.Write(ctx, func(tx *gorm.DB) error {
			if err := tx.Create(&poolNote{Body: "lost"}).Error; err != nil {
				return err
			}
			return stop
		})

		var count int64
		db.Model(&poolNote{}).Count(&count)
		if okErr != nil || !errors.Is(failErr, stop) || count != 1 {
			t.Errorf("count = %d, errors %v and %v, want 1 row", count, okErr, failErr)
		}
	})

	t.Run("a write that waits longer than WriteWait returns ErrBusy", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{WriteWait: 100 * time.Millisecond})
		inWrite, release := make(chan struct{}), make(chan struct{})
		first := make(chan error, 1)
		go func() {
			first <- m.Write(ctx, func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return nil
			})
		}()
		<-inWrite

		start := time.Now()
		err := m.Write(ctx, func(tx *gorm.DB) error { return nil })
		waited := time.Since(start)
		close(release)

		if !errors.Is(err, sqlite.ErrBusy) || waited < 90*time.Millisecond || waited > 2*time.Second {
			t.Errorf("err = %v after %v, want ErrBusy after about 100ms", err, waited)
		}
		if err := <-first; err != nil {
			t.Errorf("first write: %v", err)
		}
	})

	t.Run("a caller that leaves while it waits gets its context error", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{})
		inWrite, release := make(chan struct{}), make(chan struct{})
		go func() {
			_ = m.Write(ctx, func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return nil
			})
		}()
		<-inWrite
		waiting, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		err := m.Write(waiting, func(tx *gorm.DB) error { return nil })
		close(release)

		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sqlite.ErrBusy) {
			t.Errorf("err = %v, want the context error", err)
		}
	})

	t.Run("a write commits after its caller leaves mid-transaction", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		leaving, cancel := context.WithCancel(ctx)

		err := m.Write(leaving, func(tx *gorm.DB) error {
			cancel()
			return tx.Create(&poolNote{Body: "kept"}).Error
		})

		var count int64
		db.Model(&poolNote{}).Count(&count)
		if err != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want the row", count, err)
		}
	})

	t.Run("a write outside Write waits for a Write, and then works", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		inWrite, release := make(chan struct{}), make(chan struct{})
		first := make(chan error, 1)
		go func() {
			first <- m.Write(ctx, func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return tx.Create(&poolNote{Body: "in Write"}).Error
			})
		}()
		<-inWrite
		outside := make(chan error, 1)
		go func() { outside <- db.Create(&poolNote{Body: "outside"}).Error }()

		select {
		case err := <-outside:
			t.Fatalf("the outside write did not wait: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		close(release)

		if err := <-outside; err != nil {
			t.Errorf("outside write: %v", err)
		}
		if err := <-first; err != nil {
			t.Errorf("Write: %v", err)
		}
	})

	t.Run("after a panic in fn, the next write works", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{WriteWait: 500 * time.Millisecond})
		func() {
			defer func() { _ = recover() }()
			_ = m.Write(ctx, func(tx *gorm.DB) error { panic("boom") })
		}()

		err := m.Write(ctx, func(tx *gorm.DB) error { return tx.Create(&poolNote{Body: "after"}).Error })

		var count int64
		db.Model(&poolNote{}).Count(&count)
		if err != nil || count != 1 {
			t.Errorf("count = %d, err = %v, want the write after the panic", count, err)
		}
	})
}

func TestReadsAfterSchemaChange(t *testing.T) {
	{
		t.Run("after SchemaChanged, the next read has the new column", func(t *testing.T) {
			m, db := newPoolManager(t, sqlite.Config{MaxOpenConns: 1})
			rows := func() ([]map[string]any, error) {
				var out []map[string]any
				err := db.Raw("SELECT * FROM pool_notes ORDER BY id").Scan(&out).Error
				return out, err
			}
			if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
				t.Fatal(err)
			}
			if before, err := rows(); err != nil || len(before) != 1 {
				t.Fatalf("before: %d rows, %v", len(before), err)
			}

			alterErr := db.Exec("ALTER TABLE pool_notes ADD COLUMN title TEXT DEFAULT 'none'").Error
			m.SchemaChanged()
			after, err := rows()

			if alterErr != nil || err != nil {
				t.Fatalf("alter: %v, read: %v", alterErr, err)
			}
			if len(after) != 1 || after[0]["title"] != "none" {
				t.Errorf("after = %v, want the new column", after)
			}
		})
	}

	t.Run("many different read statements all work", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "x"}).Error; err != nil {
			t.Fatal(err)
		}

		for i := range 300 {
			// Each length of the IN list is another SQL text.
			ids := make([]int, i+1)
			for j := range ids {
				ids[j] = j
			}
			var count int64
			if err := db.Model(&poolNote{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
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
		m, db := newPoolManager(t, sqlite.Config{})
		first, err1 := m.DataVersion(ctx)
		same, err2 := m.DataVersion(ctx)

		createErr := db.Create(&poolNote{Body: "x"}).Error
		changed, err3 := m.DataVersion(ctx)

		if err := errors.Join(err1, err2, err3, createErr); err != nil {
			t.Fatal(err)
		}
		if first == "" || first != same || changed == first {
			t.Errorf("tokens %q, %q, then %q: want the same twice, then another", first, same, changed)
		}
	})

	t.Run("changes when another process writes the file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.db")
		m, _ := newPoolManager(t, sqlite.Config{Path: path})
		other, _ := newPoolManager(t, sqlite.Config{Path: path})
		otherDB, _ := other.Connect()
		before, err := m.DataVersion(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if err := otherDB.Create(&poolNote{Body: "from elsewhere"}).Error; err != nil {
			t.Fatal(err)
		}
		after, err := m.DataVersion(ctx)

		if err != nil || after == before {
			t.Errorf("token %q then %q (%v), want a change", before, after, err)
		}
	})

	t.Run("a rolled back write does not change it", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{})
		before, _ := m.DataVersion(ctx)

		_ = m.Write(ctx, func(tx *gorm.DB) error {
			_ = tx.Create(&poolNote{Body: "lost"}).Error
			return errors.New("stop")
		})
		after, err := m.DataVersion(ctx)

		if err != nil || after != before {
			t.Errorf("token %q then %q (%v), want no change", before, after, err)
		}
	})

	t.Run("works without the read pool, and is new after a reopen", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "one.db")})
		t.Cleanup(func() { _ = m.Close() })
		before, err1 := m.DataVersion(ctx)

		_ = m.Close()
		after, err2 := m.DataVersion(ctx)

		if err1 != nil || err2 != nil || before == "" || after == before {
			t.Errorf("tokens %q and %q, errors %v %v: want two different tokens", before, after, err1, err2)
		}
	})

	t.Run("a database in memory has none", func(t *testing.T) {
		m := sqlite.NewManager(sqlite.Config{Path: ":memory:"})
		t.Cleanup(func() { _ = m.Close() })

		if _, err := m.DataVersion(ctx); err == nil {
			t.Error("DataVersion returned nil, want an error")
		}
	})
}

func TestPragmasWithReadPool(t *testing.T) {
	type child struct {
		ID       uint
		ParentID uint
	}
	_, db := newPoolManager(t, sqlite.Config{Pragmas: []string{"PRAGMA foreign_keys = ON"}})
	schema := `
		CREATE TABLE parents (id INTEGER PRIMARY KEY);
		CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents (id));`
	if err := db.Exec(schema).Error; err != nil {
		t.Fatal(err)
	}

	t.Run("an app pragma runs on the write connection", func(t *testing.T) {
		err := db.Create(&child{ParentID: 99}).Error

		if err == nil {
			t.Error("a child without its parent was saved, want the foreign key to stop it")
		}
	})

	t.Run("an app pragma runs on the read connections", func(t *testing.T) {
		var on int
		err := db.Raw("SELECT foreign_keys FROM pragma_foreign_keys").Scan(&on).Error

		if err != nil || on != 1 {
			t.Errorf("foreign_keys on a read connection = %d (%v), want 1", on, err)
		}
	})
}

func TestReadPoolOf(t *testing.T) {
	t.Run("gives read connections that work while a write holds the write connection", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
			t.Fatal(err)
		}
		pool, ok := sqlite.ReadPoolOf(db)
		if !ok {
			t.Fatal("ReadPoolOf returned no pool")
		}
		inWrite, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- m.Write(context.Background(), func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return nil
			})
		}()
		<-inWrite

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		conn, connErr := pool.Conn(ctx)
		var count int
		var queryErr, writeErr error
		if connErr == nil {
			queryErr = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM pool_notes").Scan(&count)
			_, writeErr = conn.ExecContext(ctx, "DELETE FROM pool_notes")
			_ = conn.Close()
		}
		close(release)
		<-done

		if connErr != nil || queryErr != nil || count != 1 {
			t.Errorf("count = %d, conn: %v, query: %v", count, connErr, queryErr)
		}
		if writeErr == nil {
			t.Error("a write on the read connection worked, want an error")
		}
	})

	t.Run("is false without the read pool and inside a transaction", func(t *testing.T) {
		single := sqlite.NewManager(sqlite.Config{Path: filepath.Join(t.TempDir(), "one.db")})
		t.Cleanup(func() { _ = single.Close() })
		singleDB, err := single.Connect()
		if err != nil {
			t.Fatal(err)
		}
		_, db := newPoolManager(t, sqlite.Config{})
		insideTx := true

		_, withoutPool := sqlite.ReadPoolOf(singleDB)
		_ = db.Transaction(func(tx *gorm.DB) error {
			_, insideTx = sqlite.ReadPoolOf(tx)
			return nil
		})

		if withoutPool || insideTx {
			t.Errorf("without pool = %v, inside a transaction = %v, want false and false", withoutPool, insideTx)
		}
	})
}

// The method that the app calls decides the pool, not the SQL text.
func TestReadRouting(t *testing.T) {
	// holdWrite keeps the write connection busy until release is closed.
	holdWrite := func(t *testing.T, m *sqlite.Manager) (release chan struct{}, done chan error) {
		t.Helper()
		inWrite := make(chan struct{})
		release, done = make(chan struct{}), make(chan error, 1)
		go func() {
			done <- m.Write(context.Background(), func(tx *gorm.DB) error {
				close(inWrite)
				<-release
				return nil
			})
		}()
		<-inWrite
		return release, done
	}

	t.Run("every kind of read runs while the write connection is busy", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
			t.Fatal(err)
		}
		release, done := holdWrite(t, m)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		db = db.WithContext(ctx)

		var note poolNote
		var notes []poolNote
		var count, viaCTE, viaRow int64
		var bodies []string
		errs := map[string]error{
			"First":       db.First(&note).Error,
			"Find":        db.Where("body = ?", "one").Find(&notes).Error,
			"Count":       db.Model(&poolNote{}).Count(&count).Error,
			"Pluck":       db.Model(&poolNote{}).Pluck("body", &bodies).Error,
			"Raw CTE":     db.Raw("WITH n AS (SELECT id FROM pool_notes) SELECT COUNT(*) FROM n").Scan(&viaCTE).Error,
			"Raw comment": db.Raw("-- a comment first\nSELECT COUNT(*) FROM pool_notes").Scan(&viaCTE).Error,
			"Row":         db.Raw("SELECT COUNT(*) FROM pool_notes").Row().Scan(&viaRow),
		}
		close(release)
		<-done

		for name, err := range errs {
			if err != nil {
				t.Errorf("%s waited for the write connection: %v", name, err)
			}
		}
		if note.Body != "one" || len(notes) != 1 || count != 1 || len(bodies) != 1 || viaCTE != 1 || viaRow != 1 {
			t.Errorf("results: %+v, %d notes, count %d, %v, cte %d, row %d", note, len(notes), count, bodies, viaCTE, viaRow)
		}
	})

	t.Run("a write through a read method is refused", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
			t.Fatal(err)
		}
		var id int64

		err := db.Raw("UPDATE pool_notes SET body = 'changed' RETURNING id").Scan(&id).Error

		var body string
		db.Raw("SELECT body FROM pool_notes").Scan(&body)
		if err == nil || body != "one" {
			t.Errorf("err = %v, body = %q, want an error and no change", err, body)
		}
	})

	t.Run("the same write in a transaction works", func(t *testing.T) {
		m, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
			t.Fatal(err)
		}
		var id int64

		err := m.Write(context.Background(), func(tx *gorm.DB) error {
			return tx.Raw("UPDATE pool_notes SET body = 'changed' RETURNING id").Scan(&id).Error
		})

		if err != nil || id == 0 {
			t.Errorf("id = %d, err = %v", id, err)
		}
	})

	t.Run("a handle that read can write next", func(t *testing.T) {
		_, db := newPoolManager(t, sqlite.Config{})
		if err := db.Create(&poolNote{Body: "one"}).Error; err != nil {
			t.Fatal(err)
		}
		var count, left int64
		handle := db.Table("pool_notes")

		readErr := handle.Count(&count).Error
		writeErr := handle.Exec("DELETE FROM pool_notes").Error

		db.Model(&poolNote{}).Count(&left)
		if readErr != nil || writeErr != nil || count != 1 || left != 0 {
			t.Errorf("read: %v (count %d), write: %v, rows left: %d", readErr, count, writeErr, left)
		}
	})

	t.Run("a read in a transaction sees the writes of the transaction", func(t *testing.T) {
		m, _ := newPoolManager(t, sqlite.Config{})
		var inside int64

		err := m.Write(context.Background(), func(tx *gorm.DB) error {
			if err := tx.Create(&poolNote{Body: "draft"}).Error; err != nil {
				return err
			}
			return tx.Raw("SELECT COUNT(*) FROM pool_notes").Scan(&inside).Error
		})

		if err != nil || inside != 1 {
			t.Errorf("inside = %d, err = %v, want 1", inside, err)
		}
	})
}
