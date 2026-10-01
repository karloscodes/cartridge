package cartridge

import (
	"context"
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

	// Server - provide a pre-built server (takes precedence over ServerConfig)
	Server *Server

	// Server configuration (used if Server is nil)
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
	var server *Server
	var err error

	// Use provided server or create one from config
	if opts.Server != nil {
		server = opts.Server
	} else {
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
		server, err = NewServer(serverCfg)
		if err != nil {
			return nil, err
		}
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

// Start launches background workers and the HTTP server. It blocks until
// the server stops. Use Run to also handle SIGINT and SIGTERM.
func (a *Application) Start() error {
	if err := a.startWorkers(); err != nil {
		return err
	}
	return a.Server.Start()
}

// StartAsync launches background workers and the HTTP server, and returns
// once the port is bound. It returns the bind error, if any.
func (a *Application) StartAsync() error {
	if err := a.startWorkers(); err != nil {
		return err
	}
	if err := a.Server.StartAsync(); err != nil {
		a.stopWorkers()
		return err
	}
	return nil
}

// Shutdown stops the server, then the workers. The server waits for open
// requests until ctx is done.
func (a *Application) Shutdown(ctx context.Context) error {
	err := a.Server.Shutdown(ctx)
	a.stopWorkers()
	return err
}

// startWorkers starts each worker. When one fails, it stops the workers
// that started and returns the error.
func (a *Application) startWorkers() error {
	for i, w := range a.workers {
		if err := w.Start(); err != nil {
			for _, started := range a.workers[:i] {
				started.Stop()
			}
			return err
		}
	}
	return nil
}

// stopWorkers stops all background workers.
func (a *Application) stopWorkers() {
	for _, w := range a.workers {
		w.Stop()
	}
}

// Run starts the application and waits for SIGINT or SIGTERM. Then it shuts
// down gracefully, with a timeout of 10 seconds.
func (a *Application) Run() error {
	return a.RunWithTimeout(10 * time.Second)
}

// RunWithTimeout starts the application and waits for SIGINT or SIGTERM.
// Then it stops the server and the workers, and waits up to timeout for
// open requests. When the server fails to start, it returns that error.
func (a *Application) RunWithTimeout(timeout time.Duration) error {
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.StartAsync(); err != nil {
		return err
	}

	<-signals.Done()
	stop() // A second signal now kills the process.
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
