package cartridge

import (
	"log/slog"
	"net/http"
	"os"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func TestPublicFS(t *testing.T) {
	publicFiles := fstest.MapFS{
		"favicon.svg": &fstest.MapFile{Data: []byte("<svg>test</svg>")},
		"robots.txt":  &fstest.MapFile{Data: []byte("User-agent: *\nAllow: /")},
	}

	cfg := DefaultServerConfig()
	cfg.PublicFS = publicFiles
	cfg.EnableStaticAssets = false
	cfg.EnableRequestLogger = false
	cfg.Config = &testConfig{}
	cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg.DBManager = &testDBManager{}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Register an app route to verify public files don't block it
	srv.app.Get("/dashboard", func(c *fiber.Ctx) error {
		return c.SendString("dashboard")
	})

	t.Run("serves favicon.svg at root", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/favicon.svg", nil)
		resp, err := srv.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("serves robots.txt at root", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/robots.txt", nil)
		resp, err := srv.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("falls through to app routes for non-public files", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/dashboard", nil)
		resp, err := srv.app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}

// Minimal test implementations

type testConfig struct{}

func (c *testConfig) IsDevelopment() bool         { return false }
func (c *testConfig) IsProduction() bool          { return false }
func (c *testConfig) IsTest() bool                { return true }
func (c *testConfig) GetPort() string             { return "3000" }
func (c *testConfig) GetPublicDirectory() string  { return "" }
func (c *testConfig) GetAssetsPrefix() string     { return "/assets" }

type testDBManager struct{}

func (d *testDBManager) GetConnection() *gorm.DB     { return nil }
func (d *testDBManager) Connect() (*gorm.DB, error)  { return nil, nil }
