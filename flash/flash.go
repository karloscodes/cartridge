package flash

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const (
	// Cookie name for flash messages
	FlashCookieName = "flash"
)

// FlashMessage represents a temporary message to be displayed to the user
type FlashMessage struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

// SetFlash stores a flash message in a cookie.
// The secure parameter controls whether the cookie requires HTTPS.
// Pass true in production, false in development.
func SetFlash(w http.ResponseWriter, messageType string, message string, secure ...bool) {
	// Default to false (development-friendly), callers should pass true in production
	isSecure := len(secure) > 0 && secure[0]

	jsonData, err := json.Marshal(FlashMessage{Type: messageType, Message: message})
	if err != nil {
		slog.Default().Error("Failed to marshal flash message", slog.Any("error", err))
		return
	}

	// Encode as base64 to avoid cookie parsing issues
	http.SetCookie(w, &http.Cookie{
		Name:     FlashCookieName,
		Value:    base64.StdEncoding.EncodeToString(jsonData),
		Path:     "/",
		MaxAge:   60, // Short-lived cookie, just 1 minute
		Secure:   isSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	slog.Default().Debug("Flash message set",
		slog.String("type", messageType),
		slog.String("message", message))
}

// GetFlash retrieves and clears the flash message
func GetFlash(w http.ResponseWriter, r *http.Request) *FlashMessage {
	cookie, err := r.Cookie(FlashCookieName)
	if err != nil || cookie.Value == "" {
		return &FlashMessage{}
	}

	// Clear the cookie immediately by setting an expired cookie
	http.SetCookie(w, &http.Cookie{
		Name:     FlashCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Now().Add(-24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonData, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		slog.Default().Error("Failed to decode flash message", slog.Any("error", err))
		return &FlashMessage{}
	}

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
