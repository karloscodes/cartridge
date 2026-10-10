package cartridge

import (
	"context"
	"io"
	"net"
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

// Shutdown stops the server, then the workers, then closes the databases.
// The server waits for open requests until ctx is done.
func (a *Application) Shutdown(ctx context.Context) error {
	err := a.Server.Shutdown(ctx)
	a.stopWorkers()
	a.closeDatabases()
	return err
}

// closeDatabases closes the main database and the named ones, when their
// managers have a Close method. A SQLite manager then saves its query
// statistics for the next start. A closed manager opens again on its next use.
func (a *Application) closeDatabases() {
	managers := map[string]DBManager{"main": a.DBManager}
	for name, m := range a.Server.cfg.Databases {
		managers[name] = m
	}
	for name, m := range managers {
		closer, ok := m.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil {
			a.Logger.Error("failed to close the database", "database", name, "error", err)
		}
	}
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
// open requests. When the server fails to start or stops with an error, it
// stops the workers and returns that error.
func (a *Application) RunWithTimeout(timeout time.Duration) error {
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(signals, stop) // A second signal now kills the process.

	if err := a.startWorkers(); err != nil {
		return err
	}
	ln, err := a.Server.listen()
	if err != nil {
		a.stopWorkers()
		return err
	}
	return a.serveUntil(signals, ln, timeout)
}

// serveUntil serves on ln until ctx is done or the server fails. Then it
// shuts down the server and the workers.
func (a *Application) serveUntil(ctx context.Context, ln net.Listener, timeout time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- a.Server.serve(ln) }()

	select {
	case err := <-served:
		if err != nil {
			a.Logger.Error("Server failed", "error", err)
			a.stopWorkers()
			a.closeDatabases()
		}
		return err
	case <-ctx.Done():
	}

	a.Logger.Info("Shutting down gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := a.Shutdown(shutdownCtx); err != nil {
		a.Logger.Error("Graceful shutdown failed", "error", err)
		return err
	}

	a.Logger.Info("Shutdown complete")
	return nil
}
