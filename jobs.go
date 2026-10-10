package cartridge

import (
	"context"
	"database/sql"
	"fmt"
)

// JobContext gives a cron job the app's dependencies.
type JobContext struct {
	context.Context
	Logger Logger

	// DB is the pool of read connections of the main database. Pass the
	// JobContext itself as the context of each query. Write with WriteTx.
	DB *sql.DB

	dbManager DBManager            // for WriteTx
	databases map[string]DBManager // the named databases
}

// Database returns the read pool of the named database. See
// Context.Database. It returns an error when no database has the name or
// the connection fails: a panic in a job would stop the app.
func (ctx *JobContext) Database(name string) (*sql.DB, error) {
	m := ctx.databases[name]
	if m == nil {
		return nil, fmt.Errorf("cartridge: no database named %q", name)
	}
	return m.Reader()
}

// context returns the job's context, or the background context for a
// JobContext built by hand.
func (ctx *JobContext) context() context.Context {
	if ctx.Context == nil {
		return context.Background()
	}
	return ctx.Context
}
