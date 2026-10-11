package testsupport

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/sqlite"
)

// NewTestDB opens a new SQLite file in t.TempDir() and runs the .sql files
// of migrations on it, as cartridge.WithMigrations does. Pass nil for an
// empty database. The test cleanup closes it.
//
//	m := testsupport.NewTestDB(t, db.Migrations())
//	writer, _ := m.Writer()
func NewTestDB(t *testing.T, migrations fs.FS) *sqlite.Manager {
	t.Helper()

	m := sqlite.NewManager(sqlite.Config{
		Path:   filepath.Join(t.TempDir(), "test.db"),
		Logger: NewTestLogger(),
	})
	t.Cleanup(func() { _ = m.Close() })

	writer, err := m.Writer()
	if err != nil {
		t.Fatalf("testsupport: open the test database: %v", err)
	}
	if migrations != nil {
		if err := cartridge.Migrate(writer, migrations); err != nil {
			t.Fatalf("testsupport: migrate the test database: %v", err)
		}
		m.SchemaChanged()
	}
	return m
}
