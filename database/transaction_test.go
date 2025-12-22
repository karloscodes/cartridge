package database_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/karloscodes/cartridge/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type TestModel struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TestModel{}))
	return db
}

func TestPerformWrite_Success(t *testing.T) {
	db := setupTestDB(t)
	log := slog.Default()

	err := database.PerformWrite(log, db, func(tx *gorm.DB) error {
		return tx.Create(&TestModel{Name: "test"}).Error
	})
	require.NoError(t, err)

	var count int64
	db.Model(&TestModel{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestPerformWrite_Rollback(t *testing.T) {
	db := setupTestDB(t)
	log := slog.Default()

	err := database.PerformWrite(log, db, func(tx *gorm.DB) error {
		if err := tx.Create(&TestModel{Name: "test"}).Error; err != nil {
			return err
		}
		return errors.New("intentional error")
	})
	require.Error(t, err)
	assert.Equal(t, "intentional error", err.Error())

	var count int64
	db.Model(&TestModel{}).Count(&count)
	assert.Equal(t, int64(0), count, "Transaction should have been rolled back")
}

func TestPerformWriteWithConfig_MutexMode(t *testing.T) {
	db := setupTestDB(t)
	log := slog.Default()

	cfg := database.TransactionConfig{
		UseNativeSQLiteQueuing: false,
		MaxRetries:             3,
	}

	err := database.PerformWriteWithConfig(log, db, func(tx *gorm.DB) error {
		return tx.Create(&TestModel{Name: "mutex-test"}).Error
	}, cfg)
	require.NoError(t, err)

	var count int64
	db.Model(&TestModel{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestDefaultTransactionConfig(t *testing.T) {
	cfg := database.DefaultTransactionConfig()

	assert.True(t, cfg.UseNativeSQLiteQueuing, "Default should use native SQLite queuing")
	assert.Equal(t, 10, cfg.MaxRetries)
	assert.Greater(t, cfg.BaseDelay.Milliseconds(), int64(0))
	assert.Greater(t, cfg.MaxDelay.Seconds(), float64(0))
}
