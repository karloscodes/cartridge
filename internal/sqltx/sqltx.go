// Package sqltx runs a function in one database/sql transaction.
package sqltx

import (
	"context"
	"database/sql"
)

// Beginner starts a transaction. *sql.DB and *sql.Conn implement it.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Run begins a transaction, calls fn, and commits. When fn returns an error
// or panics, it rolls back. A panic goes on after the rollback.
func Run(ctx context.Context, b Beginner, fn func(tx *sql.Tx) error) error {
	tx, err := b.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	done = true
	return tx.Commit()
}
