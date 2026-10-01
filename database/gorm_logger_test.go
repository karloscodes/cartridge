package database

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGormLogger(t *testing.T) {
	t.Run("logs SQL without parameter values", func(t *testing.T) {
		var out bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: NewGormLogger(logger, nil)})
		if err != nil {
			t.Fatal(err)
		}
		type account struct {
			ID       uint
			Password string
		}
		if err := db.AutoMigrate(&account{}); err != nil {
			t.Fatal(err)
		}
		out.Reset()

		db.Create(&account{Password: "hunter2-secret"})

		if strings.Contains(out.String(), "hunter2-secret") {
			t.Errorf("the log holds a parameter value: %s", out.String())
		}
		if !strings.Contains(out.String(), "INSERT INTO") {
			t.Errorf("expected the SQL in the log, got %s", out.String())
		}
	})
}
