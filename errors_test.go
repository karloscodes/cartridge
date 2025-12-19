package cartridge

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestErrorCodeName(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{fiber.StatusBadRequest, "Bad Request"},
		{fiber.StatusUnauthorized, "Unauthorized"},
		{fiber.StatusForbidden, "Forbidden"},
		{fiber.StatusNotFound, "Not Found"},
		{fiber.StatusMethodNotAllowed, "Method Not Allowed"},
		{fiber.StatusTooManyRequests, "Too Many Requests"},
		{fiber.StatusInternalServerError, "Internal Server Error"},
		{fiber.StatusBadGateway, "Bad Gateway"},
		{fiber.StatusServiceUnavailable, "Service Unavailable"},
		{418, "Error"}, // Unknown code
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := ErrorCodeName(tt.code)
			if result != tt.expected {
				t.Errorf("ErrorCodeName(%d) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestErrorHTML(t *testing.T) {
	t.Run("generates valid HTML without message", func(t *testing.T) {
		html := errorHTML(404, "Not Found", "")

		if !strings.Contains(html, "<!DOCTYPE html>") {
			t.Error("expected DOCTYPE declaration")
		}
		if !strings.Contains(html, "<title>404 - Not Found</title>") {
			t.Error("expected title with status code")
		}
		if !strings.Contains(html, ">404<") {
			t.Error("expected status code in body")
		}
		if !strings.Contains(html, ">Not Found<") {
			t.Error("expected error name in body")
		}
		if !strings.Contains(html, "← Go back home") {
			t.Error("expected back link")
		}
	})

	t.Run("includes error message when provided", func(t *testing.T) {
		html := errorHTML(500, "Internal Server Error", "connection refused")

		if !strings.Contains(html, "connection refused") {
			t.Error("expected error message in HTML")
		}
	})
}

func TestDefaultErrorHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("returns fiber.ErrorHandler", func(t *testing.T) {
		handler := DefaultErrorHandler(logger, false)
		if handler == nil {
			t.Error("expected non-nil error handler")
		}
	})
}
