package cartridge

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// CronJob represents a scheduled job with its metadata
type CronJob struct {
	ID          string
	Schedule    string
	Description string
	Handler     CronHandler
	EntryID     cron.EntryID
}

// CronHandler is the function signature for cron job handlers
// It receives a CronContext with access to database, logger, etc.
type CronHandler func(ctx *CronContext) error

// CronContext provides access to shared resources for cron jobs
// This is separate from HTTP Context as cron jobs don't have request/response
type CronContext struct {
	Database Database
	Logger   Logger
	ctx      context.Context
	job      *CronJob
	dbOps    *DatabaseOperations
}

// DB returns the GORM database instance, abstracting the type assertion
func (ctx *CronContext) DB() *gorm.DB {
	if ctx.Database == nil {
		return nil
	}
	return ctx.Database.GetGenericConnection().(*gorm.DB)
}

// DBExec executes database commands with clean error handling
func (ctx *CronContext) DBExec(query string, args ...interface{}) *gorm.DB {
	if ctx.dbOps == nil {
		ctx.Logger.Error("Database operations not available - database connection may be closed")
		return nil
	}
	return ctx.dbOps.Exec(query, args...)
}

// DBQuery executes database SELECT queries and scans into destination
func (ctx *CronContext) DBQuery(query string, dest interface{}, args ...interface{}) *gorm.DB {
	if ctx.dbOps == nil {
		ctx.Logger.Error("Database operations not available - database connection may be closed")
		return nil
	}
	return ctx.dbOps.Query(query, dest, args...)
}

// Context returns the context
func (ctx *CronContext) Context() context.Context {
	return ctx.ctx
}

// Job returns the current job information
func (ctx *CronContext) Job() *CronJob {
	return ctx.job
}

// CronManager manages scheduled jobs using robfig/cron
type CronManager struct {
	cron     *cron.Cron
	jobs     map[string]*CronJob
	database Database
	logger   Logger
	ctx      context.Context
}

// NewCronManager creates a new cron manager
func NewCronManager(database Database, logger Logger) *CronManager {
	// Create cron with seconds precision and UTC timezone
	c := cron.New(cron.WithSeconds(), cron.WithLocation(time.UTC))

	return &CronManager{
		cron:     c,
		jobs:     make(map[string]*CronJob),
		database: database,
		logger:   logger,
		ctx:      context.Background(),
	}
}

// AddJob registers a new cron job
// Schedule format: "0 30 * * * *" (every 30 seconds), "0 0 12 * * MON-FRI" (weekdays at noon)
// Supports seconds precision: "sec min hour day month dayOfWeek"
func (cm *CronManager) AddJob(id, schedule, description string, handler CronHandler) error {
	if _, exists := cm.jobs[id]; exists {
		return fmt.Errorf("cron job with ID '%s' already exists", id)
	}

	job := &CronJob{
		ID:          id,
		Schedule:    schedule,
		Description: description,
		Handler:     handler,
	}

	// Wrap the handler with error handling and logging
	wrappedHandler := func() {
		jobLogger := cm.logger.With("cron_job", id)

		// Create cron context
		cronCtx := &CronContext{
			Database: cm.database,
			Logger:   jobLogger,
			ctx:      cm.ctx,
			job:      job,
		}

		// Only create database operations if database is available
		if cm.database != nil && cm.database.GetGenericConnection() != nil {
			cronCtx.dbOps = NewDatabaseOperations(cm.database.GetGenericConnection().(*gorm.DB), jobLogger, "cron")
		}

		cronCtx.Logger.Debug("Starting cron job execution",
			"schedule", schedule,
			"description", description)

		start := time.Now()

		// Execute with enhanced error boundary
		func() {
			defer func() {
				if r := recover(); r != nil {
					cronCtx.Logger.Error("Cron job panicked",
						"job_id", job.ID,
						"schedule", job.Schedule,
						"description", job.Description,
						"panic", r,
						"duration", time.Since(start))
				}
			}()

			if err := handler(cronCtx); err != nil {
				cronCtx.Logger.Error("Cron job failed",
					"job_id", job.ID,
					"schedule", job.Schedule,
					"description", job.Description,
					"error", err,
					"duration", time.Since(start))
			} else {
				cronCtx.Logger.Info("Cron job completed successfully",
					"job_id", job.ID,
					"duration", time.Since(start))
			}
		}()
	}

	// Add job to cron scheduler
	entryID, err := cm.cron.AddFunc(schedule, wrappedHandler)
	if err != nil {
		return fmt.Errorf("failed to add cron job '%s': %w", id, err)
	}

	job.EntryID = entryID
	cm.jobs[id] = job

	cm.logger.Info("Cron job registered",
		"id", id,
		"schedule", schedule,
		"description", description,
		"entry_id", entryID)

	return nil
}

// RemoveJob removes a cron job
func (cm *CronManager) RemoveJob(id string) error {
	job, exists := cm.jobs[id]
	if !exists {
		return fmt.Errorf("cron job with ID '%s' not found", id)
	}

	cm.cron.Remove(job.EntryID)
	delete(cm.jobs, id)

	cm.logger.Info("Cron job removed", "id", id)
	return nil
}

// Start begins the cron scheduler
func (cm *CronManager) Start() {
	cm.logger.Info("Starting cron scheduler", "jobs_count", len(cm.jobs))
	cm.cron.Start()

	// Log next run times for all jobs
	for id, job := range cm.jobs {
		entry := cm.cron.Entry(job.EntryID)
		cm.logger.Info("Cron job scheduled",
			"id", id,
			"next_run", entry.Next.Format(time.RFC3339))
	}
}

// Stop gracefully shuts down the cron scheduler
func (cm *CronManager) Stop() {
	cm.logger.Info("Stopping cron scheduler")
	ctx := cm.cron.Stop()
	<-ctx.Done()
	cm.logger.Info("Cron scheduler stopped")
}

// Status returns information about all registered jobs
func (cm *CronManager) Status() map[string]interface{} {
	status := make(map[string]interface{})
	jobs := make([]map[string]interface{}, 0, len(cm.jobs))

	for id, job := range cm.jobs {
		entry := cm.cron.Entry(job.EntryID)

		jobInfo := map[string]interface{}{
			"id":          id,
			"schedule":    job.Schedule,
			"description": job.Description,
			"next_run":    entry.Next.Format(time.RFC3339),
			"prev_run":    entry.Prev.Format(time.RFC3339),
		}

		if entry.Prev.IsZero() {
			jobInfo["prev_run"] = "never"
		}

		jobs = append(jobs, jobInfo)
	}

	status["jobs"] = jobs
	status["total_jobs"] = len(cm.jobs)
	status["running"] = cm.cron != nil

	return status
}

// GetJob returns information about a specific job
func (cm *CronManager) GetJob(id string) (*CronJob, error) {
	job, exists := cm.jobs[id]
	if !exists {
		return nil, fmt.Errorf("cron job with ID '%s' not found", id)
	}
	return job, nil
}

// ListJobs returns all registered job IDs
func (cm *CronManager) ListJobs() []string {
	ids := make([]string, 0, len(cm.jobs))
	for id := range cm.jobs {
		ids = append(ids, id)
	}
	return ids
}

// HasJobs returns true if there are any registered cron jobs
func (cm *CronManager) HasJobs() bool {
	return len(cm.jobs) > 0
}
