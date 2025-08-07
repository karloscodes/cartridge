package cartridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// TaskInfo represents the serializable information about a task
type TaskInfo struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description,omitempty"`
	Args        map[string]interface{} `json:"args,omitempty"`
	StartTime   time.Time              `json:"start_time"`
	Status      string                 `json:"status"`
	Error       string                 `json:"error,omitempty"`
	Result      interface{}            `json:"result,omitempty"`
}

// AsyncTask represents a background task with its metadata
type AsyncTask struct {
	ID          string
	Description string
	Handler     AsyncHandler
	Args        map[string]interface{}
	Context     context.Context
	Cancel      context.CancelFunc
	StartTime   time.Time
	Status      TaskStatus
	Error       error
	Result      interface{}
	mutex       sync.RWMutex
}

// AsyncHandler is the function signature for async task handlers
type AsyncHandler func(ctx *AsyncContext, args map[string]interface{}) (interface{}, error)

// TaskStatus represents the current status of an async task
type TaskStatus int

const (
	TaskStatusPending TaskStatus = iota
	TaskStatusRunning
	TaskStatusCompleted
	TaskStatusFailed
	TaskStatusCancelled
)

func (ts TaskStatus) String() string {
	switch ts {
	case TaskStatusPending:
		return "pending"
	case TaskStatusRunning:
		return "running"
	case TaskStatusCompleted:
		return "completed"
	case TaskStatusFailed:
		return "failed"
	case TaskStatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// AsyncContext provides access to shared resources for async tasks
type AsyncContext struct {
	Database Database
	Logger   Logger
	Context  context.Context
	TaskID   string
	dbOps    *DatabaseOperations
}

// DB returns the GORM database instance
func (ctx *AsyncContext) DB() interface{} {
	if ctx.Database == nil {
		return nil
	}
	return ctx.Database.GetGenericConnection()
}

// DBExec executes database commands with error return (no panic for async)
func (ctx *AsyncContext) DBExec(query string, args ...interface{}) (*gorm.DB, error) {
	if ctx.dbOps == nil {
		return nil, fmt.Errorf("database operations not available")
	}
	return ctx.dbOps.ExecSafe(query, args...)
}

// DBQuery executes database queries with error return (no panic for async)
func (ctx *AsyncContext) DBQuery(query string, dest interface{}, args ...interface{}) (*gorm.DB, error) {
	if ctx.dbOps == nil {
		return nil, fmt.Errorf("database operations not available")
	}
	return ctx.dbOps.QuerySafe(query, dest, args...)
}

// AsyncManager manages background tasks
type AsyncManager struct {
	tasks    map[string]*AsyncTask
	database Database
	logger   Logger
	mutex    sync.RWMutex
}

// NewAsyncManager creates a new async task manager
func NewAsyncManager(database Database, logger Logger) *AsyncManager {
	return &AsyncManager{
		tasks:    make(map[string]*AsyncTask),
		database: database,
		logger:   logger,
	}
}

// Run executes an async task and returns the task ID
func (am *AsyncManager) Run(id string, handler AsyncHandler, args map[string]interface{}) string {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	task := &AsyncTask{
		ID:        id,
		Handler:   handler,
		Args:      args,
		Context:   ctx,
		Cancel:    cancel,
		StartTime: time.Now(),
		Status:    TaskStatusPending,
	}

	am.tasks[id] = task

	// Start the task in a goroutine with error boundary
	go am.executeTask(task)

	am.logger.Info("Async task queued", "task_id", id, "args", args)
	return id
}

// executeTask runs the async task with comprehensive error handling
func (am *AsyncManager) executeTask(task *AsyncTask) {
	defer func() {
		if r := recover(); r != nil {
			task.mutex.Lock()
			task.Status = TaskStatusFailed
			task.Error = fmt.Errorf("task panicked: %v", r)
			task.mutex.Unlock()

			am.logger.Error("Async task panicked",
				"task_id", task.ID,
				"description", task.Description,
				"panic", r,
				"duration", time.Since(task.StartTime))
		}
	}()

	// Update status to running
	task.mutex.Lock()
	task.Status = TaskStatusRunning
	task.mutex.Unlock()

	taskLogger := am.logger.With("async_task", task.ID)

	// Create async context
	var dbOps *DatabaseOperations
	if am.database != nil && am.database.GetGenericConnection() != nil {
		dbOps = NewDatabaseOperations(am.database.GetGenericConnection().(*gorm.DB), taskLogger, "async")
	}

	asyncCtx := &AsyncContext{
		Database: am.database,
		Logger:   taskLogger,
		Context:  task.Context,
		TaskID:   task.ID,
		dbOps:    dbOps,
	}

	taskLogger.Info("Starting async task execution",
		"args", task.Args)

	start := time.Now()

	// Execute the handler
	result, err := task.Handler(asyncCtx, task.Args)

	duration := time.Since(start)

	task.mutex.Lock()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		taskLogger.Error("Async task failed",
			"error", err,
			"duration", duration)
	} else if task.Context.Err() != nil {
		task.Status = TaskStatusCancelled
		task.Error = task.Context.Err()
		taskLogger.Info("Async task cancelled",
			"duration", duration)
	} else {
		task.Status = TaskStatusCompleted
		task.Result = result
		taskLogger.Info("Async task completed successfully",
			"duration", duration)
	}
	task.mutex.Unlock()
}

// Status returns the status of a specific task
func (am *AsyncManager) Status(id string) (*TaskInfo, error) {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	task, exists := am.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task with ID '%s' not found", id)
	}

	// Return a serializable copy to avoid race conditions
	task.mutex.RLock()
	defer task.mutex.RUnlock()

	var errorMsg string
	if task.Error != nil {
		errorMsg = task.Error.Error()
	}

	return &TaskInfo{
		ID:          task.ID,
		Description: task.Description,
		Args:        task.Args,
		StartTime:   task.StartTime,
		Status:      task.Status.String(),
		Error:       errorMsg,
		Result:      task.Result,
	}, nil
}

// Cancel cancels a running task
func (am *AsyncManager) Cancel(id string) error {
	am.mutex.RLock()
	task, exists := am.tasks[id]
	am.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("task with ID '%s' not found", id)
	}

	task.Cancel()
	am.logger.Info("Async task cancelled", "task_id", id)
	return nil
}

// List returns all tasks with their current status
func (am *AsyncManager) List() map[string]interface{} {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	tasks := make([]map[string]interface{}, 0, len(am.tasks))

	for id, task := range am.tasks {
		task.mutex.RLock()
		taskInfo := map[string]interface{}{
			"id":          id,
			"description": task.Description,
			"status":      task.Status.String(),
			"start_time":  task.StartTime.Format(time.RFC3339),
			"duration":    time.Since(task.StartTime).String(),
		}

		if task.Error != nil {
			taskInfo["error"] = task.Error.Error()
		}

		if task.Result != nil {
			taskInfo["has_result"] = true
		}

		task.mutex.RUnlock()
		tasks = append(tasks, taskInfo)
	}

	return map[string]interface{}{
		"tasks":       tasks,
		"total_tasks": len(am.tasks),
	}
}

// Cleanup removes completed or failed tasks older than the specified duration
func (am *AsyncManager) Cleanup(olderThan time.Duration) int {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	removed := 0
	cutoff := time.Now().Add(-olderThan)

	for id, task := range am.tasks {
		task.mutex.RLock()
		shouldRemove := task.StartTime.Before(cutoff) &&
			(task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed)
		task.mutex.RUnlock()

		if shouldRemove {
			delete(am.tasks, id)
			removed++
		}
	}

	if removed > 0 {
		am.logger.Info("Cleaned up old async tasks", "removed_count", removed)
	}

	return removed
}
