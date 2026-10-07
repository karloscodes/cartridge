package cartridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SessionConfig configures the session manager.
type SessionConfig struct {
	// CookieName is the name of the session cookie. Default: "session".
	CookieName string

	// Secret is the HMAC secret for signing session tokens. Required, at
	// least 32 bytes.
	Secret string

	// TTL is the session duration. Default: 24 hours.
	TTL time.Duration

	// Insecure drops the Secure flag from the cookie, so the browser sends
	// it over plain http. Use it only for local development. Default: false.
	Insecure bool

	// LoginPath is where to redirect unauthenticated users. Default: "/login".
	LoginPath string

	// Valid ends sessions before they expire. A signed cookie stays good
	// until its expiry, also after a logout or a password change. When Valid
	// is set, a session counts only if Valid returns true: check that the
	// user still exists and that issuedAt is not before the user's last
	// password change. It runs once per request.
	Valid func(userID uint, issuedAt time.Time) bool
}

// minSessionSecretLength is the shortest secret NewSessionManager accepts.
const minSessionSecretLength = 32

// SessionManager handles cookie-based session authentication.
type SessionManager struct {
	cookieName string
	secret     []byte
	ttl        time.Duration
	secure     bool
	loginPath  string
	valid      func(userID uint, issuedAt time.Time) bool
}

// sessionKey is the Locals key for the session a request resolved to.
type sessionKey struct{ sm *SessionManager }

// SessionData stores session information in the cookie.
type SessionData struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	// IssuedAt lets an app end sessions created before a password change.
	// Cookies created before this field existed decode with the zero time.
	IssuedAt time.Time `json:"issued_at,omitempty"`
}

// NewSessionManager creates a session manager with the given configuration.
// It returns an error when the secret is shorter than 32 bytes: anyone who
// knows or guesses the secret can sign in as any user.
func NewSessionManager(cfg SessionConfig) (*SessionManager, error) {
	if len(cfg.Secret) < minSessionSecretLength {
		return nil, fmt.Errorf("cartridge: session secret must be at least %d bytes", minSessionSecretLength)
	}

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
		secure:     !cfg.Insecure,
		loginPath:  loginPath,
		valid:      cfg.Valid,
	}, nil
}

// SetSession creates a session cookie for the given user ID.
func (sm *SessionManager) SetSession(c *Context, userID uint) error {
	now := time.Now()
	sessionData := SessionData{
		UserID:    strconv.FormatUint(uint64(userID), 10),
		ExpiresAt: now.Add(sm.ttl),
		IssuedAt:  now,
	}

	jsonData, err := json.Marshal(sessionData)
	if err != nil {
		return err
	}

	token, err := sm.sign(jsonData)
	if err != nil {
		return err
	}

	delete(c.locals, sessionKey{sm})
	c.Cookie(&Cookie{
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
func (sm *SessionManager) ClearSession(c *Context) {
	delete(c.locals, sessionKey{sm})
	c.ClearCookie(sm.cookieName)
	c.Cookie(&Cookie{
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
func (sm *SessionManager) IsAuthenticated(c *Context) bool {
	_, ok := sm.GetUserID(c)
	return ok
}

// GetUserID retrieves the user ID from the session cookie.
// Returns 0 and false if not authenticated.
func (sm *SessionManager) GetUserID(c *Context) (uint, bool) {
	data, ok := sm.current(c)
	if !ok {
		return 0, false
	}
	userID, err := strconv.ParseUint(data.UserID, 10, strconv.IntSize)
	if err != nil {
		slog.Debug("invalid user ID in session", slog.String("user_id", data.UserID))
		return 0, false
	}
	return uint(userID), true
}

// IssuedAt returns when the current session was created. It returns false
// when there is no valid session. Sessions created by older versions have
// the zero time.
func (sm *SessionManager) IssuedAt(c *Context) (time.Time, bool) {
	data, ok := sm.current(c)
	if !ok {
		return time.Time{}, false
	}
	return data.IssuedAt, true
}

// current returns the session of the request. It resolves the cookie once
// per request, so SessionConfig.Valid runs once.
func (sm *SessionManager) current(c *Context) (*SessionData, bool) {
	if data, ok := c.Locals(sessionKey{sm}).(*SessionData); ok {
		return data, data != nil
	}
	data, _ := sm.resolve(c)
	c.Locals(sessionKey{sm}, data)
	return data, data != nil
}

// resolve returns the data of a session cookie that is signed, not expired,
// and accepted by SessionConfig.Valid.
func (sm *SessionManager) resolve(c *Context) (*SessionData, bool) {
	token := c.Cookies(sm.cookieName)
	if token == "" {
		return nil, false
	}

	data, err := sm.verify(token)
	if err != nil {
		slog.Debug("session verification failed", slog.Any("error", err))
		return nil, false
	}

	if time.Now().After(data.ExpiresAt) {
		slog.Debug("session expired", slog.Time("expires_at", data.ExpiresAt))
		return nil, false
	}

	if sm.valid != nil {
		userID, err := strconv.ParseUint(data.UserID, 10, strconv.IntSize)
		if err != nil || !sm.valid(uint(userID), data.IssuedAt) {
			slog.Debug("session rejected by Valid", slog.String("user_id", data.UserID))
			return nil, false
		}
	}

	return data, true
}

// Middleware returns a middleware that requires authentication.
// Unauthenticated requests are redirected to LoginPath.
// HTMX requests receive a 401 status instead.
func (sm *SessionManager) Middleware() HandlerFunc {
	return func(c *Context) error {
		if !sm.IsAuthenticated(c) {
			// For HTMX requests, respond with 401
			if c.Get("HX-Request") == "true" {
				return c.Status(http.StatusUnauthorized).SendString("authentication required")
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
