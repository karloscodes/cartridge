// Package query reads rows from database/sql into Go values, without an ORM.
//
//	type Note struct {
//	    ID        int64
//	    Body      string
//	    CreatedAt time.Time
//	}
//
//	notes, err := query.All[Note](ctx, db, "SELECT id, body, created_at FROM notes WHERE user_id = ?", userID)
//	note, err := query.One[Note](ctx, db, "SELECT id, body, created_at FROM notes WHERE id = ?", id)
//	count, err := query.One[int](ctx, db, "SELECT COUNT(*) FROM notes")
//
// A column fills the field with its name: the `db` tag of the field, or
// else the field name in snake case (CreatedAt reads created_at). A column
// without a field is an error, so a wrong name shows on the first run. A
// column that can be NULL needs a pointer field or a sql.Null type.
//
// The SQL text goes to the database as it is. Always pass request values as
// arguments, never inside the text. The placeholder is the one of the
// database: "?" for SQLite and MySQL, "$1" for PostgreSQL.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Querier runs a query. *sql.DB, *sql.Tx, and *sql.Conn implement it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// All returns every row of the query as a T. T is a struct, or one value
// such as int or string for a query with one column.
func All[T any](ctx context.Context, q Querier, query string, args ...any) ([]T, error) {
	return scan[T](ctx, q, 0, query, args...)
}

// One returns the first row of the query as a T. It returns sql.ErrNoRows
// when the query has no row.
func One[T any](ctx context.Context, q Querier, query string, args ...any) (T, error) {
	rows, err := scan[T](ctx, q, 1, query, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	if len(rows) == 0 {
		var zero T
		return zero, sql.ErrNoRows
	}
	return rows[0], nil
}

// scan reads the rows of the query, at most limit of them when limit > 0.
func scan[T any](ctx context.Context, q Querier, limit int, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	paths, err := fieldPaths(reflect.TypeFor[T](), columns)
	if err != nil {
		return nil, err
	}

	var out []T
	dest := make([]any, len(columns))
	for rows.Next() {
		var value T
		v := reflect.ValueOf(&value).Elem()
		for i, path := range paths {
			if path == nil {
				dest[i] = v.Addr().Interface()
			} else {
				dest[i] = v.FieldByIndex(path).Addr().Interface()
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, value)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

// fieldPaths returns, for each column, the index path of the field of t
// that it fills. For a T that is one value, the one path is nil.
func fieldPaths(t reflect.Type, columns []string) ([][]int, error) {
	if isValue(t) {
		if len(columns) != 1 {
			return nil, fmt.Errorf("query: %s takes one column, the query has %d", t, len(columns))
		}
		return [][]int{nil}, nil
	}
	fields := fieldsOf(t)
	paths := make([][]int, len(columns))
	for i, column := range columns {
		path, ok := fields[strings.ToLower(column)]
		if !ok {
			return nil, fmt.Errorf("query: column %q has no field in %s", column, t)
		}
		paths[i] = path
	}
	return paths, nil
}

var (
	scannerType = reflect.TypeFor[sql.Scanner]()
	timeType    = reflect.TypeFor[time.Time]()
)

// isValue reports whether the database fills t from one column: every type
// but a plain struct.
func isValue(t reflect.Type) bool {
	return t.Kind() != reflect.Struct || t == timeType || reflect.PointerTo(t).Implements(scannerType)
}

var fieldCache sync.Map // reflect.Type -> map[string][]int

// fieldsOf maps the column names of t to the index paths of its fields.
func fieldsOf(t reflect.Type) map[string][]int {
	if cached, ok := fieldCache.Load(t); ok {
		return cached.(map[string][]int)
	}
	fields := map[string][]int{}
	addFields(t, nil, fields)
	fieldCache.Store(t, fields)
	return fields
}

func addFields(t reflect.Type, prefix []int, fields map[string][]int) {
	var embedded []int
	for i := range t.NumField() {
		field := t.Field(i)
		path := append(append([]int{}, prefix...), i)
		// An embedded struct gives its exported fields, also when its own
		// type name is not exported. It goes last, so an outer field wins.
		if field.Anonymous && !isValue(field.Type) {
			embedded = append(embedded, i)
			continue
		}
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("db"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = snakeCase(field.Name)
		}
		if _, taken := fields[strings.ToLower(name)]; !taken {
			fields[strings.ToLower(name)] = path
		}
	}
	for _, i := range embedded {
		addFields(t.Field(i).Type, append(append([]int{}, prefix...), i), fields)
	}
}

// snakeCase turns a Go field name into a column name: "CreatedAt" is
// "created_at", "UserID" is "user_id", and "HTMLBody" is "html_body".
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && nextIsLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
