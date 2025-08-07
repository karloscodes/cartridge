package cartridge

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPErrorBoundary(t *testing.T) {
	// Create test app
	app := NewAPIOnly(WithEnvironment("test"))

	// Add a handler that panics
	app.Get("/panic", func(ctx *Context) error {
		panic("test panic in HTTP handler")
	})

	// Add a handler that returns an error
	app.Get("/error", func(ctx *Context) error {
		return fmt.Errorf("test error in HTTP handler")
	})

	// Create test server
	req := httptest.NewRequest("GET", "/panic", nil)
	resp, err := app.fiberApp.Test(req)
	if err != nil {
		t.Fatalf("Failed to test panic handler: %v", err)
	}

	// Check that panic was caught and 500 was returned
	if resp.StatusCode != 500 {
		t.Errorf("Expected status 500 for panic, got %d", resp.StatusCode)
	}

	// Read response body to check error message
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	responseBody := buf.String()
	
	if !strings.Contains(responseBody, "Internal server error") {
		t.Errorf("Expected 'Internal server error' in response, got: %s", responseBody)
	}

	t.Log("HTTP error boundary test passed")
}

func TestCronErrorBoundary(t *testing.T) {
	// Create test app
	app := NewAPIOnly(WithEnvironment("test"))

	panicJobExecuted := false
	errorJobExecuted := false

	// Add a cron job that panics
	err := app.AddCronJob("panic-job", "* * * * * *", "Test panic job", func(ctx *CronContext) error {
		panicJobExecuted = true
		panic("test panic in cron job")
	})
	if err != nil {
		t.Fatalf("Failed to add panic job: %v", err)
	}

	// Add a cron job that returns an error
	err = app.AddCronJob("error-job", "* * * * * *", "Test error job", func(ctx *CronContext) error {
		errorJobExecuted = true
		return fmt.Errorf("test error in cron job")
	})
	if err != nil {
		t.Fatalf("Failed to add error job: %v", err)
	}

	// Start cron jobs
	app.StartCronJobs()

	// Wait for jobs to execute
	time.Sleep(2 * time.Second)

	// Stop cron jobs
	app.StopCronJobs()

	// Check that both jobs executed despite panic/error
	if !panicJobExecuted {
		t.Error("Panic job never executed")
	}

	if !errorJobExecuted {
		t.Error("Error job never executed")
	}

	// Check that cron manager is still functional
	status := app.CronStatus()
	if status["total_jobs"] != 2 {
		t.Errorf("Expected 2 jobs in status, got %v", status["total_jobs"])
	}

	t.Log("Cron error boundary test passed")
}

func TestMixedErrorBoundaries(t *testing.T) {
	// Test that error boundaries don't interfere with each other
	app := NewAPIOnly(WithEnvironment("test"))

	cronExecuted := false

	// Add working HTTP handler
	app.Get("/ok", func(ctx *Context) error {
		return ctx.JSON(map[string]string{"status": "ok"})
	})

	// Add working cron job
	err := app.AddCronJob("working-job", "* * * * * *", "Working job", func(ctx *CronContext) error {
		cronExecuted = true
		ctx.Logger.Info("Working job executed successfully")
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to add working job: %v", err)
	}

	// Start cron
	app.StartCronJobs()

	// Test HTTP handler
	req := httptest.NewRequest("GET", "/ok", nil)
	resp, err := app.fiberApp.Test(req)
	if err != nil {
		t.Fatalf("Failed to test working handler: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Wait for cron job
	time.Sleep(2 * time.Second)
	app.StopCronJobs()

	if !cronExecuted {
		t.Error("Working cron job never executed")
	}

	t.Log("Mixed error boundaries test passed")
}