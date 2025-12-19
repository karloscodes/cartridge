package cartridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// SessionConfig configures the session manager.
type SessionConfig struct {
	// CookieName is the name of the session cookie. Default: "session".
	CookieName string

	// Secret is the HMAC secret for signing session tokens. Required.
	Secret string

	// TTL is the session duration. Default: 24 hours.
	TTL time.Duration

	// Secure sets the Secure flag on cookies. Default: true in production.
	Secure bool

	// LoginPath is where to redirect unauthenticated users. Default: "/login".
	LoginPath string
}

// SessionManager handles cookie-based session authentication.
type SessionManager struct {
	cookieName string
	secret     []byte
	ttl        time.Duration
	secure     bool
	loginPath  string
}

// SessionData stores session information in the cookie.
type SessionData struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewSessionManager creates a session manager with the given configuration.
func NewSessionManager(cfg SessionConfig) *SessionManager {
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = "session"
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}

	loginPath := cfg.LoginPath
	if loginPath == "" {
		loginPath = "/login"
	}

	return &SessionManager{
		cookieName: cookieName,
		secret:     []byte(cfg.Secret),
		ttl:        ttl,
		secure:     cfg.Secure,
		loginPath:  loginPath,
	}
}

// SetSession creates a session cookie for the given user ID.
func (sm *SessionManager) SetSession(c *fiber.Ctx, userID uint) error {
	sessionData := SessionData{
		UserID:    strconv.FormatUint(uint64(userID), 10),
		ExpiresAt: time.Now().Add(sm.ttl),
	}

	jsonData, err := json.Marshal(sessionData)
	if err != nil {
		return err
	}

	token, err := sm.sign(jsonData)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     sm.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sm.ttl.Seconds()),
		Expires:  sessionData.ExpiresAt,
		Secure:   sm.secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	slog.Debug("session created",
		slog.Uint64("user_id", uint64(userID)),
		slog.Time("expires_at", sessionData.ExpiresAt))
	return nil
}

// ClearSession removes the session cookie.
func (sm *SessionManager) ClearSession(c *fiber.Ctx) {
	c.ClearCookie(sm.cookieName)
	c.Cookie(&fiber.Cookie{
		Name:     sm.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Now().Add(-24 * time.Hour),
		Secure:   sm.secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
	slog.Debug("session cleared")
}

// IsAuthenticated checks if the request has a valid session.
func (sm *SessionManager) IsAuthenticated(c *fiber.Ctx) bool {
	token := c.Cookies(sm.cookieName)
	if token == "" {
		return false
	}

	sessionData, err := sm.verify(token)
	if err != nil {
		slog.Debug("session verification failed", slog.Any("error", err))
		return false
	}

	if time.Now().After(sessionData.ExpiresAt) {
		slog.Debug("session expired", slog.Time("expires_at", sessionData.ExpiresAt))
		return false
	}

	if _, err := strconv.ParseUint(sessionData.UserID, 10, 64); err != nil {
		slog.Debug("invalid user ID in session", slog.String("user_id", sessionData.UserID))
		return false
	}

	return true
}

// GetUserID retrieves the user ID from the session cookie.
// Returns 0 and false if not authenticated.
func (sm *SessionManager) GetUserID(c *fiber.Ctx) (uint, bool) {
	token := c.Cookies(sm.cookieName)
	if token == "" {
		return 0, false
	}

	sessionData, err := sm.verify(token)
	if err != nil {
		return 0, false
	}

	if time.Now().After(sessionData.ExpiresAt) {
		return 0, false
	}

	userID, err := strconv.ParseUint(sessionData.UserID, 10, 32)
	if err != nil {
		return 0, false
	}

	return uint(userID), true
}

// Middleware returns a Fiber middleware that requires authentication.
// Unauthenticated requests are redirected to LoginPath.
// HTMX requests receive a 401 status instead.
func (sm *SessionManager) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !sm.IsAuthenticated(c) {
			// For HTMX requests, respond with 401
			if c.Get("HX-Request") == "true" {
				return c.Status(fiber.StatusUnauthorized).SendString("authentication required")
			}
			return c.Redirect(sm.loginPath)
		}
		return c.Next()
	}
}

func (sm *SessionManager) sign(payload []byte) (string, error) {
	sig := sm.computeHMAC(payload)
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return payloadEnc + "." + sigEnc, nil
}

func (sm *SessionManager) verify(token string) (*SessionData, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("invalid session token")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid session payload")
	}

	expectedSig := sm.computeHMAC(payload)
	actualSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid session signature")
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("session signature mismatch")
	}

	var sessionData SessionData
	if err := json.Unmarshal(payload, &sessionData); err != nil {
		return nil, errors.New("invalid session data")
	}

	return &sessionData, nil
}

func (sm *SessionManager) computeHMAC(payload []byte) []byte {
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write(payload)
	return mac.Sum(nil)
}
