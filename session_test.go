package cartridge

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	t.Run("uses defaults for empty config", func(t *testing.T) {
		sm := NewSessionManager(SessionConfig{
			Secret: "test-secret",
		})

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
		sm := NewSessionManager(SessionConfig{
			CookieName: "my_session",
			Secret:     "test-secret",
			TTL:        1 * time.Hour,
			Secure:     true,
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
		if !sm.secure {
			t.Error("expected secure to be true")
		}
	})
}

func TestSessionSigning(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		Secret: "test-secret-key-32-characters-xx",
	})

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
	sm1 := NewSessionManager(SessionConfig{Secret: "secret-one"})
	sm2 := NewSessionManager(SessionConfig{Secret: "secret-two"})

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
