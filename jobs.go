package cartridge

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
)

// JobContext provides job-scoped access to application dependencies.
type JobContext struct {
	context.Context
	Logger Logger
	DB     *gorm.DB
}

// Processor defines the interface for processing a batch of work.
type Processor interface {
	ProcessBatch(ctx *JobContext) error
}

// JobDispatcher runs processors periodically in a background loop.
type JobDispatcher struct {
	logger     Logger
	dbManager  DBManager
	processors []Processor
	interval   time.Duration
	mu         sync.Mutex
	running    bool
	stop       chan struct{}
	wg         sync.WaitGroup
}

// NewJobDispatcher creates a new background job dispatcher.
func NewJobDispatcher(logger Logger, dbManager DBManager, interval time.Duration, processors ...Processor) *JobDispatcher {
	return &JobDispatcher{
		logger:     logger,
		dbManager:  dbManager,
		processors: processors,
		interval:   interval,
	}
}

// Start begins the background processing loop.
func (d *JobDispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	d.stop = make(chan struct{})
	d.running = true
	d.wg.Add(1)
	go d.loop()
	return nil
}

// Stop terminates the dispatcher and waits for completion.
func (d *JobDispatcher) Stop() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	close(d.stop)
	d.running = false
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *JobDispatcher) loop() {
	defer d.wg.Done()

	d.logger.Info("jobs dispatcher started", "processors", len(d.processors))
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// Run immediately on startup
	d.processBatch()

	for {
		select {
		case <-ticker.C:
			d.processBatch()
		case <-d.stop:
			d.logger.Info("jobs dispatcher stopped")
			return
		}
	}
}

func (d *JobDispatcher) processBatch() {
	db, err := d.dbManager.Connect()
	if err != nil {
		d.logger.Error("failed to connect to database", "error", err)
		return
	}

	ctx := &JobContext{
		Context: context.Background(),
		Logger:  d.logger,
		DB:      db,
	}

	for _, processor := range d.processors {
		if err := processor.ProcessBatch(ctx); err != nil {
			d.logger.Error("processor failed", "error", err)
		}
	}
}
