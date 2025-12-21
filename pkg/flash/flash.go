package flash

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"log/slog"

	"github.com/karloscodes/cartridge/config"
)

const (
	// Cookie name for flash messages
	FlashCookieName = "fusionaly_flash"
)

// FlashMessage represents a temporary message to be displayed to the user
type FlashMessage struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// SetFlash stores a flash message in a cookie
func SetFlash(c *fiber.Ctx, messageType string, message string) {
	// Create flash message
	flash := FlashMessage{
		Type:    messageType,
		Message: message,
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(flash)
	if err != nil {
		slog.Default().Error("Failed to marshal flash message", slog.Any("error", err))
		return
	}

	// Encode as base64 to avoid cookie parsing issues
	encodedData := base64.StdEncoding.EncodeToString(jsonData)

	cfg := config.GetConfig()
	// Set as cookie
	cookie := &fiber.Cookie{
		Name:     FlashCookieName,
		Value:    encodedData,
		Path:     "/",
		MaxAge:   60, // Short-lived cookie, just 1 minute
		Secure:   cfg.Environment == config.Production,
		HTTPOnly: true,
		SameSite: "Lax",
	}
	c.Cookie(cookie)

	slog.Default().Debug("Flash message set",
		slog.String("type", messageType),
		slog.String("message", message))
}

// GetFlash retrieves and clears the flash message
func GetFlash(c *fiber.Ctx) *FlashMessage {
	// Get the flash cookie
	encodedData := c.Cookies(FlashCookieName)
	if encodedData == "" {
		return &FlashMessage{}
	}

	// Clear the cookie immediately by setting an expired cookie
	expiredCookie := &fiber.Cookie{
		Name:     FlashCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Now().Add(-24 * time.Hour), // Expired cookie
		HTTPOnly: true,
		SameSite: "Lax",
	}
	c.Cookie(expiredCookie)

	// Decode from base64
	jsonData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		slog.Default().Error("Failed to decode flash message", slog.Any("error", err))
		return &FlashMessage{}
	}

	// Unmarshal JSON
	var flash FlashMessage
	if err := json.Unmarshal(jsonData, &flash); err != nil {
		slog.Default().Error("Failed to unmarshal flash message", slog.Any("error", err))
		return &FlashMessage{}
	}

	slog.Default().Debug("Flash message retrieved",
		slog.String("type", flash.Type),
		slog.String("message", flash.Message))

	return &flash
}
