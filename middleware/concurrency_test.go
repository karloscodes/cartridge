package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockLogger implements the Logger interface for testing.
type mockLogger struct {
	debugCalls int32
	infoCalls  int32
	warnCalls  int32
	errorCalls int32
}

func (m *mockLogger) Debug(msg string, keysAndValues ...any) { atomic.AddInt32(&m.debugCalls, 1) }
func (m *mockLogger) Info(msg string, keysAndValues ...any)  { atomic.AddInt32(&m.infoCalls, 1) }
func (m *mockLogger) Warn(msg string, keysAndValues ...any)  { atomic.AddInt32(&m.warnCalls, 1) }
func (m *mockLogger) Error(msg string, keysAndValues ...any) { atomic.AddInt32(&m.errorCalls, 1) }

func TestNewConcurrencyLimiter(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewConcurrencyLimiter(10, 1, 5*time.Second, logger)

	if limiter == nil {
		t.Fatal("expected non-nil limiter")
	}
	if limiter.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", limiter.timeout)
	}
}

func TestConcurrencyLimiter_AcquireReleaseRead(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewConcurrencyLimiter(2, 1, time.Second, logger)

	ctx := context.Background()

	// Acquire first read
	if err := limiter.AcquireRead(ctx); err != nil {
		t.Fatalf("first AcquireRead failed: %v", err)
	}

	// Acquire second read
	if err := limiter.AcquireRead(ctx); err != nil {
		t.Fatalf("second AcquireRead failed: %v", err)
	}

	// Third read should block (we're at limit)
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := limiter.AcquireRead(ctx2)
	if err == nil {
		t.Error("expected third AcquireRead to timeout")
	}

	// Release one and try again
	limiter.ReleaseRead()
	if err := limiter.AcquireRead(ctx); err != nil {
		t.Fatalf("AcquireRead after release failed: %v", err)
	}

	// Clean up
	limiter.ReleaseRead()
	limiter.ReleaseRead()
}

func TestConcurrencyLimiter_AcquireReleaseWrite(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewConcurrencyLimiter(10, 1, time.Second, logger)

	ctx := context.Background()

	// Acquire write
	if err := limiter.AcquireWrite(ctx); err != nil {
		t.Fatalf("AcquireWrite failed: %v", err)
	}

	// Second write should block
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := limiter.AcquireWrite(ctx2)
	if err == nil {
		t.Error("expected second AcquireWrite to timeout")
	}

	// Release and try again
	limiter.ReleaseWrite()
	if err := limiter.AcquireWrite(ctx); err != nil {
		t.Fatalf("AcquireWrite after release failed: %v", err)
	}

	limiter.ReleaseWrite()
}

func TestConcurrencyLimiter_ConcurrentAccess(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewConcurrencyLimiter(5, 2, time.Second, logger)

	var wg sync.WaitGroup
	ctx := context.Background()

	// Spawn multiple readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.AcquireRead(ctx); err != nil {
				t.Errorf("AcquireRead failed: %v", err)
				return
			}
			time.Sleep(10 * time.Millisecond)
			limiter.ReleaseRead()
		}()
	}

	// Spawn multiple writers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.AcquireWrite(ctx); err != nil {
				t.Errorf("AcquireWrite failed: %v", err)
				return
			}
			time.Sleep(10 * time.Millisecond)
			limiter.ReleaseWrite()
		}()
	}

	wg.Wait()
}

func TestConcurrencyLimiter_ContextCancellation(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewConcurrencyLimiter(1, 1, time.Second, logger)

	ctx := context.Background()

	// Acquire the only write slot
	if err := limiter.AcquireWrite(ctx); err != nil {
		t.Fatalf("AcquireWrite failed: %v", err)
	}

	// Try to acquire with canceled context
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	err := limiter.AcquireWrite(canceledCtx)
	if err == nil {
		t.Error("expected AcquireWrite to fail with canceled context")
	}

	limiter.ReleaseWrite()
}
