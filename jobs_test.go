package cartridge

import (
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockProcessor implements Processor for testing.
type mockProcessor struct {
	callCount int32
	err       error
}

func (m *mockProcessor) ProcessBatch(ctx *JobContext) error {
	atomic.AddInt32(&m.callCount, 1)
	return m.err
}

// testLogger creates a logger that discards output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestJobDispatcher_StartStop(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	manager := &mockDBManager{db: db}
	logger := testLogger()

	processor := &mockProcessor{}
	dispatcher := NewJobDispatcher(logger, manager, 50*time.Millisecond, processor)

	// Start
	if err := dispatcher.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for at least one batch
	time.Sleep(100 * time.Millisecond)

	// Stop
	dispatcher.Stop()

	calls := atomic.LoadInt32(&processor.callCount)
	if calls < 1 {
		t.Errorf("expected at least 1 call, got %d", calls)
	}
}

func TestJobDispatcher_ProcessorError(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	manager := &mockDBManager{db: db}
	logger := testLogger()

	processor := &mockProcessor{err: errors.New("test error")}
	dispatcher := NewJobDispatcher(logger, manager, 50*time.Millisecond, processor)

	if err := dispatcher.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	dispatcher.Stop()

	// Should still call processor even if it errors
	calls := atomic.LoadInt32(&processor.callCount)
	if calls < 1 {
		t.Errorf("expected at least 1 call despite error, got %d", calls)
	}
}

func TestJobDispatcher_DoubleStart(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	manager := &mockDBManager{db: db}
	logger := testLogger()

	dispatcher := NewJobDispatcher(logger, manager, time.Hour, &mockProcessor{})

	if err := dispatcher.Start(); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer dispatcher.Stop()

	// Second start should be no-op
	if err := dispatcher.Start(); err != nil {
		t.Errorf("second Start should not error: %v", err)
	}
}

func TestJobDispatcher_DoubleStop(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	manager := &mockDBManager{db: db}
	logger := testLogger()

	dispatcher := NewJobDispatcher(logger, manager, time.Hour, &mockProcessor{})

	if err := dispatcher.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	dispatcher.Stop()

	// Second stop should be safe
	dispatcher.Stop() // Should not panic
}

func TestJobDispatcher_MultipleProcessors(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	manager := &mockDBManager{db: db}
	logger := testLogger()

	p1 := &mockProcessor{}
	p2 := &mockProcessor{}
	dispatcher := NewJobDispatcher(logger, manager, 50*time.Millisecond, p1, p2)

	if err := dispatcher.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	dispatcher.Stop()

	if atomic.LoadInt32(&p1.callCount) < 1 {
		t.Error("processor 1 should be called")
	}
	if atomic.LoadInt32(&p2.callCount) < 1 {
		t.Error("processor 2 should be called")
	}
}
