package cartridge

import (
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// recordingWorker records its Start and Stop calls.
type recordingWorker struct {
	started chan struct{}
	stopped atomic.Bool
}

func newRecordingWorker() *recordingWorker {
	return &recordingWorker{started: make(chan struct{}, 1)}
}

func (w *recordingWorker) Start() error {
	w.started <- struct{}{}
	return nil
}

func (w *recordingWorker) Stop() { w.stopped.Store(true) }

func newLifecycleApp(t *testing.T, port string, worker BackgroundWorker) *Application {
	t.Helper()
	srv := newTestApp(t)
	srv.cfg.Config = &portConfig{port: port}
	app, err := NewApplication(ApplicationOptions{
		Config:            srv.cfg.Config,
		Logger:            srv.cfg.Logger,
		DBManager:         srv.cfg.DBManager,
		Server:            srv,
		BackgroundWorkers: []BackgroundWorker{worker},
	})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	return app
}

func busyPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	return port
}

func TestApplicationRun(t *testing.T) {
	t.Run("on SIGTERM, it shuts down and stops the workers", func(t *testing.T) {
		port := freePort(t)
		worker := newRecordingWorker()
		app := newLifecycleApp(t, port, worker)
		done := make(chan error, 1)
		go func() { done <- app.Run() }()
		<-worker.started
		waitForPort(t, port)

		syscall.Kill(os.Getpid(), syscall.SIGTERM)

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after SIGTERM")
		}
		if !worker.stopped.Load() {
			t.Error("the worker did not stop")
		}
	})

	t.Run("when the port is in use, it returns the error and stops the workers", func(t *testing.T) {
		worker := newRecordingWorker()
		app := newLifecycleApp(t, busyPort(t), worker)

		err := app.Run()

		if err == nil {
			t.Fatal("Run returned nil, want a bind error")
		}
		if !worker.stopped.Load() {
			t.Error("the worker did not stop")
		}
	})
}

func TestServerStartAsync(t *testing.T) {
	t.Run("returns the bind error", func(t *testing.T) {
		srv := newTestApp(t)
		srv.cfg.Config = &portConfig{port: busyPort(t)}

		err := srv.StartAsync()

		if err == nil {
			t.Error("StartAsync returned nil, want a bind error")
		}
	})
}
