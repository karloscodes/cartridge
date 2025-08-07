package cartridge

import (
	"testing"
	"time"
)

func TestCronManager(t *testing.T) {
	// Create test app
	app := NewAPIOnly(WithEnvironment("test"))

	// Test adding a job  
	jobExecuted := false
	err := app.AddCronJob("test-job", "* * * * * *", "Test job", func(ctx *CronContext) error {
		jobExecuted = true
		ctx.Logger.Info("Test job executed")
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Start the cron manager
	app.StartCronJobs()

	// Wait a bit for the job to run
	time.Sleep(2 * time.Second)

	// Stop the cron manager
	app.StopCronJobs()

	// Check status
	status := app.CronStatus()
	if status["total_jobs"] != 1 {
		t.Errorf("Expected 1 job, got %v", status["total_jobs"])
	}

	if !jobExecuted {
		t.Error("Job was not executed")
	}
}