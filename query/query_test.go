package query_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/karloscodes/cartridge/query"
)

type timestamps struct {
	CreatedAt time.Time
}

type note struct {
	ID       int64
	Body     string
	UserID   int64
	Subtitle *string
	Label    string `db:"tag"`
	Ignored  string `db:"-"`
	timestamps
	Timestamps timestamps `db:"-"`
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT, user_id INTEGER, subtitle TEXT, tag TEXT, created_at DATETIME);
		INSERT INTO notes (body, user_id, subtitle, tag, created_at) VALUES
			('first', 7, NULL, 'work', '2026-10-10 12:30:00'),
			('second', 7, 'more', 'home', '2026-10-11 08:00:00'),
			('other', 8, NULL, 'work', '2026-10-12 09:00:00');`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAll(t *testing.T) {
	ctx := context.Background()

	t.Run("fills structs by snake case names, tags, and embedded structs", func(t *testing.T) {
		db := newDB(t)

		notes, err := query.All[note](ctx, db, "SELECT id, body, user_id, subtitle, tag, created_at FROM notes WHERE user_id = ? ORDER BY id", 7)

		if err != nil || len(notes) != 2 {
			t.Fatalf("got %d notes, %v", len(notes), err)
		}
		first, second := notes[0], notes[1]
		if first.ID != 1 || first.Body != "first" || first.UserID != 7 || first.Label != "work" {
			t.Errorf("first = %+v", first)
		}
		if first.Subtitle != nil || second.Subtitle == nil || *second.Subtitle != "more" {
			t.Errorf("subtitles = %v, %v, want nil for NULL and a value", first.Subtitle, second.Subtitle)
		}
		if want := time.Date(2026, 10, 10, 12, 30, 0, 0, time.UTC); !first.CreatedAt.Equal(want) {
			t.Errorf("created at %v, want %v", first.CreatedAt, want)
		}
	})

	t.Run("reads one column into plain values", func(t *testing.T) {
		db := newDB(t)

		bodies, err := query.All[string](ctx, db, "SELECT body FROM notes ORDER BY id")

		if err != nil || strings.Join(bodies, ",") != "first,second,other" {
			t.Errorf("got %v, %v", bodies, err)
		}
	})

	t.Run("no rows is an empty result, not an error", func(t *testing.T) {
		db := newDB(t)

		notes, err := query.All[note](ctx, db, "SELECT id FROM notes WHERE user_id = ?", 99)

		if err != nil || len(notes) != 0 {
			t.Errorf("got %d notes, %v", len(notes), err)
		}
	})

	t.Run("a column without a field is an error", func(t *testing.T) {
		db := newDB(t)

		_, err := query.All[note](ctx, db, "SELECT id, body AS text FROM notes")

		if err == nil || !strings.Contains(err.Error(), `"text"`) {
			t.Errorf("err = %v, want an error that names the column", err)
		}
	})

	t.Run("a field with the tag - is never filled", func(t *testing.T) {
		db := newDB(t)

		_, err := query.All[note](ctx, db, "SELECT body AS ignored FROM notes")

		if err == nil {
			t.Error("the query filled a field with the tag -")
		}
	})

	t.Run("a value type with two columns is an error", func(t *testing.T) {
		db := newDB(t)

		_, err := query.All[string](ctx, db, "SELECT id, body FROM notes")

		if err == nil {
			t.Error("All returned nil, want an error")
		}
	})

	t.Run("a request value in an argument stays data", func(t *testing.T) {
		db := newDB(t)

		notes, err := query.All[note](ctx, db, "SELECT id FROM notes WHERE body = ?", "x' OR '1'='1")

		if err != nil || len(notes) != 0 {
			t.Errorf("got %d notes, %v, want none", len(notes), err)
		}
	})
}

func TestOne(t *testing.T) {
	ctx := context.Background()

	t.Run("returns the first row", func(t *testing.T) {
		db := newDB(t)

		got, err := query.One[note](ctx, db, "SELECT id, body FROM notes ORDER BY id DESC")

		if err != nil || got.Body != "other" {
			t.Errorf("got %+v, %v", got, err)
		}
	})

	t.Run("reads one value", func(t *testing.T) {
		db := newDB(t)

		count, err := query.One[int](ctx, db, "SELECT COUNT(*) FROM notes WHERE user_id = ?", 7)

		if err != nil || count != 2 {
			t.Errorf("count = %d, %v", count, err)
		}
	})

	t.Run("without a row, it returns sql.ErrNoRows", func(t *testing.T) {
		db := newDB(t)

		_, err := query.One[note](ctx, db, "SELECT id FROM notes WHERE id = ?", 99)

		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("err = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("works inside a transaction", func(t *testing.T) {
		db := newDB(t)
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec("INSERT INTO notes (body, user_id) VALUES ('new', 7)"); err != nil {
			t.Fatal(err)
		}

		count, err := query.One[int](ctx, tx, "SELECT COUNT(*) FROM notes")

		if err != nil || count != 4 {
			t.Errorf("count = %d, %v", count, err)
		}
	})
}

func TestExec(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	result, err := query.Exec(ctx, db, "UPDATE notes SET tag = ? WHERE user_id = ?", "done", 7)

	if err != nil {
		t.Fatal(err)
	}
	changed, _ := result.RowsAffected()
	done, _ := query.One[int](ctx, db, "SELECT COUNT(*) FROM notes WHERE tag = 'done'")
	if changed != 2 || done != 2 {
		t.Errorf("changed %d rows, %d have the tag, want 2 and 2", changed, done)
	}
}
