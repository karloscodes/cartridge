package cartridge

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// BackgroundWorker is an interface for background workers that can be started and stopped.
type BackgroundWorker interface {
	Start() error
	Stop()
}

// Application wires together configuration, logging, database, and HTTP server.
// It manages the complete lifecycle of a cartridge web application.
type Application struct {
	Config    Config
	Logger    Logger
	DBManager DBManager
	Server    *Server
	workers   []BackgroundWorker
}

// ApplicationOptions configure application bootstrapping.
type ApplicationOptions struct {
	// Core dependencies (required)
	Config    Config
	Logger    Logger
	DBManager DBManager

	// Server configuration
	ServerConfig *ServerConfig

	// Route mounting function
	RouteMountFunc func(*Server)

	// Catch-all redirect path for SPAs
	CatchAllRedirect string

	// Background workers to run alongside the server
	BackgroundWorkers []BackgroundWorker
}

// NewApplication constructs a cartridge application.
func NewApplication(opts ApplicationOptions) (*Application, error) {
	// Use default server config if not provided
	serverCfg := opts.ServerConfig
	if serverCfg == nil {
		serverCfg = DefaultServerConfig()
	}

	// Inject dependencies into server config
	serverCfg.Config = opts.Config
	serverCfg.Logger = opts.Logger
	serverCfg.DBManager = opts.DBManager

	// Create server
	server, err := NewServer(serverCfg)
	if err != nil {
		return nil, err
	}

	// Set catch-all redirect if provided
	if opts.CatchAllRedirect != "" {
		server.SetCatchAllRedirect(opts.CatchAllRedirect)
	}

	// Mount routes if function provided
	if opts.RouteMountFunc != nil {
		opts.RouteMountFunc(server)
	}

	return &Application{
		Config:    opts.Config,
		Logger:    opts.Logger,
		DBManager: opts.DBManager,
		Server:    server,
		workers:   opts.BackgroundWorkers,
	}, nil
}

// AddWorker adds a background worker to the application.
func (a *Application) AddWorker(w BackgroundWorker) {
	a.workers = append(a.workers, w)
}

// Start launches background workers and the HTTP server.
func (a *Application) Start() error {
	// Start all background workers first
	for _, w := range a.workers {
		if err := w.Start(); err != nil {
			// Stop any already started workers
			a.stopWorkers()
			return err
		}
	}
	return a.Server.Start()
}

// StartAsync launches the HTTP server asynchronously.
func (a *Application) StartAsync() error {
	// Start all background workers first
	for _, w := range a.workers {
		if err := w.Start(); err != nil {
			// Stop any already started workers
			a.stopWorkers()
			return err
		}
	}
	return a.Server.StartAsync()
}

// Shutdown gracefully stops workers and the server.
func (a *Application) Shutdown(ctx context.Context) error {
	a.stopWorkers()
	return a.Server.Shutdown(ctx)
}

// stopWorkers stops all background workers.
func (a *Application) stopWorkers() {
	for _, w := range a.workers {
		w.Stop()
	}
}

// Run starts the application and waits for termination signals.
// It handles graceful shutdown with a default timeout of 10 seconds.
func (a *Application) Run() error {
	return a.RunWithTimeout(10 * time.Second)
}

// RunWithTimeout starts the application and waits for termination signals.
// It handles graceful shutdown with the specified timeout.
func (a *Application) RunWithTimeout(timeout time.Duration) error {
	if err := a.Start(); err != nil {
		return err
	}

	// Wait for termination signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	a.Logger.Info("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := a.Shutdown(ctx); err != nil {
		a.Logger.Error("Graceful shutdown failed", "error", err)
		return err
	}

	a.Logger.Info("Shutdown complete")
	return nil
}
