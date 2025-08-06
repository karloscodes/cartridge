package cartridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GenerateRandomString generates a random string of specified length
func GenerateRandomString(length int) string {
	bytes := make([]byte, length/2+1)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}

// GenerateSecretKey generates a 32-character secret key suitable for encryption
func GenerateSecretKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// SanitizeFilename removes invalid characters from a filename
func SanitizeFilename(filename string) string {
	// Remove path components
	filename = filepath.Base(filename)

	// Replace invalid characters with underscores
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	filename = reg.ReplaceAllString(filename, "_")

	// Remove control characters
	reg = regexp.MustCompile(`[\x00-\x1f\x7f]`)
	filename = reg.ReplaceAllString(filename, "")

	// Trim spaces and dots
	filename = strings.Trim(filename, " .")

	// Ensure filename is not empty
	if filename == "" {
		filename = "file"
	}

	return filename
}

// IsValidEmail validates an email address using a simple regex
func IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// IsValidURL validates a URL
func IsValidURL(url string) bool {
	urlRegex := regexp.MustCompile(`^https?://[^\s]+$`)
	return urlRegex.MatchString(url)
}

// TruncateString truncates a string to a maximum length
func TruncateString(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}
	return s[:maxLength-3] + "..."
}

// SlugifyString converts a string to a URL-friendly slug
func SlugifyString(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces and special characters with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	s = reg.ReplaceAllString(s, "-")

	// Remove leading and trailing hyphens
	s = strings.Trim(s, "-")

	return s
}

// ParseBool parses a string to boolean with more flexible input
func ParseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "yes", "on", "enable", "enabled":
		return true
	default:
		return false
	}
}

// ParseInt parses a string to int with default value
func ParseInt(s string, defaultValue int) int {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return defaultValue
}

// ParseInt64 parses a string to int64 with default value
func ParseInt64(s string, defaultValue int64) int64 {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return defaultValue
}

// ParseFloat64 parses a string to float64 with default value
func ParseFloat64(s string, defaultValue float64) float64 {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return defaultValue
}

// ContainsString checks if a slice contains a string
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// RemoveString removes a string from a slice
func RemoveString(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// UniqueStrings returns unique strings from a slice
func UniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(slice))

	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// JoinNonEmpty joins non-empty strings with a separator
func JoinNonEmpty(separator string, strs ...string) string {
	var nonEmpty []string
	for _, s := range strs {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return strings.Join(nonEmpty, separator)
}

// FormatBytes formats bytes as human-readable string
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats duration as human-readable string
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

// FormatTimeAgo formats time as "X ago" string
func FormatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(duration.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	if days < 30 {
		return fmt.Sprintf("%d days ago", days)
	}

	months := days / 30
	if months == 1 {
		return "1 month ago"
	}
	if months < 12 {
		return fmt.Sprintf("%d months ago", months)
	}

	years := months / 12
	if years == 1 {
		return "1 year ago"
	}
	return fmt.Sprintf("%d years ago", years)
}

// Retry executes a function with exponential backoff retry logic
func Retry(attempts int, sleep time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}

		if i < attempts-1 {
			time.Sleep(sleep)
			sleep *= 2 // Exponential backoff
		}
	}
	return err
}

// IsStructEmpty checks if a struct has all zero values
func IsStructEmpty(s interface{}) bool {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return reflect.DeepEqual(s, reflect.Zero(reflect.TypeOf(s)).Interface())
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.IsZero() {
			return false
		}
	}

	return true
}

// CoalesceString returns the first non-empty string
func CoalesceString(strings ...string) string {
	for _, s := range strings {
		if s != "" {
			return s
		}
	}
	return ""
}

// CoalesceInt returns the first non-zero int
func CoalesceInt(ints ...int) int {
	for _, i := range ints {
		if i != 0 {
			return i
		}
	}
	return 0
}

// MapStringKeys returns the keys of a map[string]interface{}
func MapStringKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// MapStringValues returns the values of a map[string]interface{}
func MapStringValues(m map[string]interface{}) []interface{} {
	values := make([]interface{}, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}

// PanicRecover safely recovers from panics and returns error
func PanicRecover() error {
	if r := recover(); r != nil {
		return fmt.Errorf("panic recovered: %v", r)
	}
	return nil
}

// SafeExecute executes a function and recovers from panics
func SafeExecute(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()

	return fn()
}

// TimeoutContext represents a simple timeout context
type TimeoutContext struct {
	timeout time.Duration
	done    chan struct{}
}

// NewTimeoutContext creates a new timeout context
func NewTimeoutContext(timeout time.Duration) *TimeoutContext {
	ctx := &TimeoutContext{
		timeout: timeout,
		done:    make(chan struct{}),
	}

	go func() {
		time.Sleep(timeout)
		close(ctx.done)
	}()

	return ctx
}

// Done returns a channel that's closed when the timeout expires
func (tc *TimeoutContext) Done() <-chan struct{} {
	return tc.done
}

// Timeout returns the timeout duration
func (tc *TimeoutContext) Timeout() time.Duration {
	return tc.timeout
}

// WithTimeout executes a function with a timeout
func WithTimeout(timeout time.Duration, fn func() error) error {
	ctx := NewTimeoutContext(timeout)
	errChan := make(chan error, 1)

	go func() {
		errChan <- fn()
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return fmt.Errorf("operation timed out after %v", timeout)
	}
}
