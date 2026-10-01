package cartridge

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-key-32-characters-xx"

func newSessionManager(t *testing.T, cfg SessionConfig) *SessionManager {
	t.Helper()
	sm, err := NewSessionManager(cfg)
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}
	return sm
}

func TestNewSessionManager(t *testing.T) {
	t.Run("uses defaults for empty config", func(t *testing.T) {
		sm := newSessionManager(t, SessionConfig{Secret: testSecret})

		if sm.cookieName != "session" {
			t.Errorf("expected cookie name 'session', got '%s'", sm.cookieName)
		}
		if sm.ttl != 24*time.Hour {
			t.Errorf("expected TTL 24h, got %v", sm.ttl)
		}
		if sm.loginPath != "/login" {
			t.Errorf("expected login path '/login', got '%s'", sm.loginPath)
		}
	})

	t.Run("uses provided config", func(t *testing.T) {
		sm := newSessionManager(t, SessionConfig{
			CookieName: "my_session",
			Secret:     testSecret,
			TTL:        1 * time.Hour,
			LoginPath:  "/auth/login",
		})

		if sm.cookieName != "my_session" {
			t.Errorf("expected cookie name 'my_session', got '%s'", sm.cookieName)
		}
		if sm.ttl != 1*time.Hour {
			t.Errorf("expected TTL 1h, got %v", sm.ttl)
		}
		if sm.loginPath != "/auth/login" {
			t.Errorf("expected login path '/auth/login', got '%s'", sm.loginPath)
		}
	})

	t.Run("rejects a secret shorter than 32 bytes", func(t *testing.T) {
		for _, secret := range []string{"", "short", testSecret[:31]} {
			_, err := NewSessionManager(SessionConfig{Secret: secret})

			if err == nil {
				t.Errorf("secret %q: expected an error", secret)
			}
		}
	})
}

func TestSessionCookie(t *testing.T) {
	login := func(t *testing.T, sm *SessionManager, userID uint) *http.Cookie {
		t.Helper()
		app := newTestApp(t)
		app.Get("/login", func(c *Context) error { return sm.SetSession(c, userID) })
		resp, _ := app.Test(httptest.NewRequest("GET", "/login", nil))
		cookies := resp.Cookies()
		if len(cookies) != 1 {
			t.Fatalf("expected one cookie, got %d", len(cookies))
		}
		return cookies[0]
	}

	t.Run("is Secure by default", func(t *testing.T) {
		sm := newSessionManager(t, SessionConfig{Secret: testSecret})

		cookie := login(t, sm, 1)

		if !cookie.Secure || !cookie.HttpOnly {
			t.Errorf("expected a Secure, HttpOnly cookie, got %+v", cookie)
		}
	})

	t.Run("drops Secure with Insecure", func(t *testing.T) {
		sm := newSessionManager(t, SessionConfig{Secret: testSecret, Insecure: true})

		cookie := login(t, sm, 1)

		if cookie.Secure {
			t.Error("expected no Secure flag")
		}
	})

	t.Run("IsAuthenticated and GetUserID agree on a large user ID", func(t *testing.T) {
		sm := newSessionManager(t, SessionConfig{Secret: testSecret})
		var bigID uint64 = 1 << 33
		cookie := login(t, sm, uint(bigID))
		app := newTestApp(t)
		app.Get("/me", func(c *Context) error {
			id, ok := sm.GetUserID(c)
			return c.SendString(fmt.Sprintf("%v %d %v", sm.IsAuthenticated(c), id, ok))
		})
		req := httptest.NewRequest("GET", "/me", nil)
		req.AddCookie(cookie)

		resp, _ := app.Test(req)

		if got, _ := io.ReadAll(resp.Body); string(got) != "true 8589934592 true" {
			t.Errorf("got %q", got)
		}
	})
}

func TestSessionSigning(t *testing.T) {
	sm := newSessionManager(t, SessionConfig{Secret: testSecret})

	t.Run("sign and verify roundtrip", func(t *testing.T) {
		sessionData := SessionData{
			UserID:    "123",
			ExpiresAt: time.Now().Add(time.Hour),
		}

		jsonData, err := json.Marshal(sessionData)
		if err != nil {
			t.Fatalf("failed to marshal session data: %v", err)
		}

		token, err := sm.sign(jsonData)
		if err != nil {
			t.Fatalf("failed to sign session: %v", err)
		}

		// Token should have two parts separated by a dot
		parts := strings.Split(token, ".")
		if len(parts) != 2 {
			t.Errorf("expected token to have 2 parts, got %d", len(parts))
		}

		// Verify the token
		verified, err := sm.verify(token)
		if err != nil {
			t.Fatalf("failed to verify token: %v", err)
		}

		if verified.UserID != "123" {
			t.Errorf("expected user ID '123', got '%s'", verified.UserID)
		}
	})

	t.Run("verify fails for tampered payload", func(t *testing.T) {
		sessionData := SessionData{
			UserID:    "123",
			ExpiresAt: time.Now().Add(time.Hour),
		}

		jsonData, _ := json.Marshal(sessionData)
		token, _ := sm.sign(jsonData)

		// Tamper with the payload
		parts := strings.Split(token, ".")
		tamperedData := SessionData{UserID: "999", ExpiresAt: time.Now().Add(time.Hour)}
		tamperedJSON, _ := json.Marshal(tamperedData)
		tamperedPayload := base64.RawURLEncoding.EncodeToString(tamperedJSON)
		tamperedToken := tamperedPayload + "." + parts[1]

		_, err := sm.verify(tamperedToken)
		if err == nil {
			t.Error("expected verification to fail for tampered token")
		}
	})

	t.Run("verify fails for invalid signature", func(t *testing.T) {
		sessionData := SessionData{
			UserID:    "123",
			ExpiresAt: time.Now().Add(time.Hour),
		}

		jsonData, _ := json.Marshal(sessionData)
		token, _ := sm.sign(jsonData)

		// Tamper with the signature
		parts := strings.Split(token, ".")
		tamperedToken := parts[0] + ".invalidSignature"

		_, err := sm.verify(tamperedToken)
		if err == nil {
			t.Error("expected verification to fail for invalid signature")
		}
	})

	t.Run("verify fails for malformed token", func(t *testing.T) {
		testCases := []struct {
			name  string
			token string
		}{
			{"no separator", "sometoken"},
			{"too many parts", "a.b.c"},
			{"empty payload", ".signature"},
			{"empty signature", "payload."},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := sm.verify(tc.token)
				if err == nil {
					t.Errorf("expected verification to fail for %s", tc.name)
				}
			})
		}
	})
}

func TestDifferentSecrets(t *testing.T) {
	sm1 := newSessionManager(t, SessionConfig{Secret: "secret-one-" + testSecret})
	sm2 := newSessionManager(t, SessionConfig{Secret: "secret-two-" + testSecret})

	sessionData := SessionData{
		UserID:    "123",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	jsonData, _ := json.Marshal(sessionData)

	token, _ := sm1.sign(jsonData)

	// Token signed by sm1 should not verify with sm2
	_, err := sm2.verify(token)
	if err == nil {
		t.Error("expected verification to fail with different secret")
	}

	// Token signed by sm1 should verify with sm1
	_, err = sm1.verify(token)
	if err != nil {
		t.Errorf("expected verification to succeed with same secret: %v", err)
	}
}

func TestSessionIssuedAt(t *testing.T) {
	sm := newSessionManager(t, SessionConfig{Secret: testSecret})
	app := newTestApp(t)
	app.Get("/login", func(c *Context) error { return sm.SetSession(c, 7) })
	app.Get("/issued", func(c *Context) error {
		issued, ok := sm.IssuedAt(c)
		if !ok {
			return c.SendStatus(http.StatusUnauthorized)
		}
		return c.SendString(issued.UTC().Format(time.RFC3339Nano))
	})

	t.Run("reports when the session was created", func(t *testing.T) {
		before := time.Now()
		loginResp, err := app.Test(httptest.NewRequest("GET", "/login", nil))
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/issued", nil)
		for _, cookie := range loginResp.Cookies() {
			req.AddCookie(cookie)
		}

		resp, err := app.Test(req)

		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		issued, err := time.Parse(time.RFC3339Nano, string(body))
		if err != nil {
			t.Fatalf("status %d, body %q: %v", resp.StatusCode, body, err)
		}
		if issued.Before(before.Add(-time.Second)) || issued.After(time.Now().Add(time.Second)) {
			t.Errorf("issued at %v, expected close to now", issued)
		}
	})

	t.Run("reports no session without a cookie", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/issued", nil))

		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}
