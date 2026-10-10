package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// splitPool sends each statement to one of two pools of the same SQLite
// file: a SELECT to the read-only pool, and everything else, including
// every transaction, to the one write connection. GORM uses it as its
// connection pool, so app code does not change.
//
// SQLite allows one writer. With one write connection, writes wait for each
// other in Go and never fail with "database is locked" against each other,
// and readers keep their own connections.
type splitPool struct {
	reader *sql.DB
	writer *sql.DB // nil for a read-only database
	path   string
}

// isRead reports whether the statement only reads. Only a plain SELECT
// counts: a WITH can hold an INSERT, and a PRAGMA can write.
func isRead(query string) bool {
	query = strings.TrimLeft(query, " \t\r\n(")
	return len(query) >= 6 && strings.EqualFold(query[:6], "SELECT")
}

func (p *splitPool) write() (*sql.DB, error) {
	if p.writer == nil {
		return nil, fmt.Errorf("%w: %s", ErrReadOnly, p.path)
	}
	return p.writer, nil
}

func (p *splitPool) pool(query string) (*sql.DB, error) {
	if isRead(query) {
		return p.reader, nil
	}
	return p.write()
}

func (p *splitPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db, err := p.pool(query)
	if err != nil {
		return nil, err
	}
	return db.PrepareContext(ctx, query)
}

func (p *splitPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db, err := p.write()
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, query, args...)
}

func (p *splitPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db, err := p.pool(query)
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query, args...)
}

func (p *splitPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := p.pool(query)
	if err != nil {
		// A *sql.Row cannot carry an error from here. The read pool
		// refuses the write, and Scan returns that error.
		db = p.reader
	}
	return db.QueryRowContext(ctx, query, args...)
}

// BeginTx starts every transaction on the write connection. GORM cannot
// say in advance that a transaction only reads.
func (p *splitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	db, err := p.write()
	if err != nil {
		return nil, err
	}
	return db.BeginTx(ctx, opts)
}

// GetDBConn gives GORM's db.DB() the pool for general use: the writer, or
// the reader of a read-only database.
func (p *splitPool) GetDBConn() (*sql.DB, error) {
	if p.writer != nil {
		return p.writer, nil
	}
	return p.reader, nil
}
