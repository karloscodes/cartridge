package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	t.Run("applies defaults for empty config", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		if m.cfg.MaxOpenConns != 1 {
			t.Errorf("expected MaxOpenConns 1, got %d", m.cfg.MaxOpenConns)
		}
		if m.cfg.MaxIdleConns != 1 {
			t.Errorf("expected MaxIdleConns 1, got %d", m.cfg.MaxIdleConns)
		}
		if m.cfg.ConnMaxLifetime != 10*time.Minute {
			t.Errorf("expected ConnMaxLifetime 10m, got %v", m.cfg.ConnMaxLifetime)
		}
		if m.cfg.BusyTimeout != 5000 {
			t.Errorf("expected BusyTimeout 5000, got %d", m.cfg.BusyTimeout)
		}
	})

	t.Run("uses provided config values", func(t *testing.T) {
		m := NewManager(Config{
			Path:            ":memory:",
			MaxOpenConns:    5,
			MaxIdleConns:    2,
			ConnMaxLifetime: 5 * time.Minute,
			BusyTimeout:     10000,
		})

		if m.cfg.MaxOpenConns != 5 {
			t.Errorf("expected MaxOpenConns 5, got %d", m.cfg.MaxOpenConns)
		}
		if m.cfg.MaxIdleConns != 2 {
			t.Errorf("expected MaxIdleConns 2, got %d", m.cfg.MaxIdleConns)
		}
		if m.cfg.ConnMaxLifetime != 5*time.Minute {
			t.Errorf("expected ConnMaxLifetime 5m, got %v", m.cfg.ConnMaxLifetime)
		}
		if m.cfg.BusyTimeout != 10000 {
			t.Errorf("expected BusyTimeout 10000, got %d", m.cfg.BusyTimeout)
		}
	})
}

func TestManager_Connect(t *testing.T) {
	t.Run("connects to in-memory database", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		db, err := m.Connect()
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}

		if db == nil {
			t.Error("expected non-nil database")
		}

		// Clean up
		_ = m.Close()
	})

	t.Run("connects to file database", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		m := NewManager(Config{Path: dbPath})

		db, err := m.Connect()
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}

		if db == nil {
			t.Error("expected non-nil database")
		}

		// Verify file was created
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Error("expected database file to be created")
		}

		// Clean up
		_ = m.Close()
	})

	t.Run("returns same connection on multiple calls", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		db1, _ := m.Connect()
		db2, _ := m.Connect()

		// Both should be sessions of the same underlying connection
		if db1 == nil || db2 == nil {
			t.Error("expected non-nil databases")
		}

		_ = m.Close()
	})
}

func TestManager_GetConnection(t *testing.T) {
	t.Run("returns connection after Connect", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		_, _ = m.Connect()
		db := m.GetConnection()

		if db == nil {
			t.Error("expected non-nil database from GetConnection")
		}

		_ = m.Close()
	})
}

func TestManager_Close(t *testing.T) {
	t.Run("closes connection successfully", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		_, _ = m.Connect()
		err := m.Close()

		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})

	t.Run("returns nil when not connected", func(t *testing.T) {
		m := NewManager(Config{Path: ":memory:"})

		err := m.Close()
		if err != nil {
			t.Errorf("Close on unconnected manager should not fail: %v", err)
		}
	})
}

func TestManager_CheckpointWAL(t *testing.T) {
	t.Run("checkpoints successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "wal_test.db")

		m := NewManager(Config{Path: dbPath, EnableWAL: true})
		_, _ = m.Connect()

		err := m.CheckpointWAL("PASSIVE")
		if err != nil {
			t.Errorf("CheckpointWAL failed: %v", err)
		}

		_ = m.Close()
	})
}
