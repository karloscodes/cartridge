package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

// SessionData holds encrypted session information
type SessionData struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	SecretKey      string
	CookieName     string
	CookieMaxAge   int
	CookieSecure   bool
	CookieHTTPOnly bool
	CookieSameSite string
	CookieDomain   string
	CookiePath     string
}

// DefaultAuthConfig returns default authentication configuration
func DefaultAuthConfig(secretKey string, isProduction bool) AuthConfig {
	return AuthConfig{
		SecretKey:      secretKey,
		CookieName:     "auth_session",
		CookieMaxAge:   7 * 24 * 60 * 60, // 7 days in seconds
		CookieSecure:   isProduction,
		CookieHTTPOnly: true,
		CookieSameSite: "Lax",
		CookieDomain:   "",
		CookiePath:     "/",
	}
}

// SetAuthCookie creates and sets an encrypted authentication cookie
func SetAuthCookie(c *fiber.Ctx, userID string, config AuthConfig, expiration time.Duration) error {
	sessionData := SessionData{
		UserID:    userID,
		ExpiresAt: time.Now().Add(expiration),
	}

	// Serialize session data
	jsonData, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Encrypt session data
	encryptedData, err := encrypt(jsonData, config.SecretKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt session data: %w", err)
	}

	// Encode to base64
	encodedData := base64.URLEncoding.EncodeToString(encryptedData)

	// Set cookie
	cookie := &fiber.Cookie{
		Name:     config.CookieName,
		Value:    encodedData,
		MaxAge:   config.CookieMaxAge,
		Secure:   config.CookieSecure,
		HTTPOnly: config.CookieHTTPOnly,
		SameSite: config.CookieSameSite,
		Domain:   config.CookieDomain,
		Path:     config.CookiePath,
	}

	c.Cookie(cookie)
	return nil
}

// ClearAuthCookie removes the authentication cookie
func ClearAuthCookie(c *fiber.Ctx, config AuthConfig) {
	cookie := &fiber.Cookie{
		Name:     config.CookieName,
		Value:    "",
		MaxAge:   -1,
		Secure:   config.CookieSecure,
		HTTPOnly: config.CookieHTTPOnly,
		SameSite: config.CookieSameSite,
		Domain:   config.CookieDomain,
		Path:     config.CookiePath,
	}

	c.Cookie(cookie)
}

// GetSessionData retrieves and decrypts session data from the authentication cookie
func GetSessionData(c *fiber.Ctx, config AuthConfig) (*SessionData, error) {
	// Get cookie value
	cookieValue := c.Cookies(config.CookieName)
	if cookieValue == "" {
		return nil, errors.New("no authentication cookie found")
	}

	// Decode from base64
	encryptedData, err := base64.URLEncoding.DecodeString(cookieValue)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cookie value: %w", err)
	}

	// Decrypt session data
	jsonData, err := decrypt(encryptedData, config.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt session data: %w", err)
	}

	// Deserialize session data
	var sessionData SessionData
	if err := json.Unmarshal(jsonData, &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Check if session has expired
	if time.Now().After(sessionData.ExpiresAt) {
		return nil, errors.New("session has expired")
	}

	return &sessionData, nil
}

// IsAuthenticated checks if the current request has a valid authentication cookie
func IsAuthenticated(c *fiber.Ctx, config AuthConfig) bool {
	_, err := GetSessionData(c, config)
	return err == nil
}

// GetUserID retrieves the user ID from the authentication cookie
func GetUserID(c *fiber.Ctx, config AuthConfig) (string, bool) {
	sessionData, err := GetSessionData(c, config)
	if err != nil {
		return "", false
	}
	return sessionData.UserID, true
}

// AuthRequired is a middleware that requires authentication
func AuthRequired(config AuthConfig, redirectPath string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !IsAuthenticated(c, config) {
			// For API requests, return 401
			if c.Get("Accept") == "application/json" || c.Get("Content-Type") == "application/json" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "Authentication required",
				})
			}
			
			// For web requests, redirect to login
			return c.Redirect(redirectPath)
		}
		
		return c.Next()
	}
}

// AuthOptional is a middleware that optionally loads authentication data
func AuthOptional(config AuthConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if userID, ok := GetUserID(c, config); ok {
			c.Locals("user_id", userID)
			c.Locals("authenticated", true)
		} else {
			c.Locals("authenticated", false)
		}
		
		return c.Next()
	}
}

// GeneratePasswordHash generates a bcrypt hash of a password
func GeneratePasswordHash(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

// VerifyPassword verifies a password against a bcrypt hash
func VerifyPassword(hashedPassword string, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// encrypt encrypts data using AES-GCM
func encrypt(data []byte, secretKey string) ([]byte, error) {
	// Create cipher
	block, err := aes.NewCipher([]byte(secretKey)[:32]) // Use first 32 bytes for AES-256
	if err != nil {
		return nil, err
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Encrypt and append nonce
	encrypted := gcm.Seal(nonce, nonce, data, nil)
	return encrypted, nil
}

// decrypt decrypts data using AES-GCM
func decrypt(encryptedData []byte, secretKey string) ([]byte, error) {
	// Create cipher
	block, err := aes.NewCipher([]byte(secretKey)[:32]) // Use first 32 bytes for AES-256
	if err != nil {
		return nil, err
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Extract nonce and encrypted data
	nonceSize := gcm.NonceSize()
	if len(encryptedData) < nonceSize {
		return nil, errors.New("encrypted data too short")
	}

	nonce := encryptedData[:nonceSize]
	ciphertext := encryptedData[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
