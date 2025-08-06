package utils

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateRandomString(t *testing.T) {
	length := 16
	result := GenerateRandomString(length)
	
	if len(result) != length {
		t.Errorf("Expected random string length %d, got %d", length, len(result))
	}
	
	// Generate another string to ensure they're different
	result2 := GenerateRandomString(length)
	if result == result2 {
		t.Error("Expected different random strings, got identical ones")
	}
}

func TestGenerateSecretKey(t *testing.T) {
	key := GenerateSecretKey()
	
	if len(key) != 64 { // 32 bytes = 64 hex characters
		t.Errorf("Expected secret key length 64, got %d", len(key))
	}
}

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"test@example.com", true},
		{"user.name@domain.co.uk", true},
		{"invalid.email", false},
		{"@domain.com", false},
		{"user@", false},
		{"", false},
	}
	
	for _, test := range tests {
		result := IsValidEmail(test.email)
		if result != test.valid {
			t.Errorf("Expected IsValidEmail(%s) = %v, got %v", test.email, test.valid, result)
		}
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"https://example.com/path", true},
		{"ftp://example.com", false},
		{"not-a-url", false},
		{"", false},
	}
	
	for _, test := range tests {
		result := IsValidURL(test.url)
		if result != test.valid {
			t.Errorf("Expected IsValidURL(%s) = %v, got %v", test.url, test.valid, result)
		}
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input     string
		maxLength int
		expected  string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exact", 5, "exact"},
	}
	
	for _, test := range tests {
		result := TruncateString(test.input, test.maxLength)
		if result != test.expected {
			t.Errorf("Expected TruncateString(%s, %d) = %s, got %s", 
				test.input, test.maxLength, test.expected, result)
		}
	}
}

func TestSlugifyString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"Test@#$%String", "test-string"},
		{"  Multiple   Spaces  ", "multiple-spaces"},
		{"CamelCase", "camelcase"},
	}
	
	for _, test := range tests {
		result := SlugifyString(test.input)
		if result != test.expected {
			t.Errorf("Expected SlugifyString(%s) = %s, got %s", 
				test.input, test.expected, result)
		}
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"enable", true},
		{"enabled", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
	}
	
	for _, test := range tests {
		result := ParseBool(test.input)
		if result != test.expected {
			t.Errorf("Expected ParseBool(%s) = %v, got %v", 
				test.input, test.expected, result)
		}
	}
}

func TestContainsString(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}
	
	if !ContainsString(slice, "banana") {
		t.Error("Expected slice to contain 'banana'")
	}
	
	if ContainsString(slice, "grape") {
		t.Error("Expected slice not to contain 'grape'")
	}
}

func TestUniqueStrings(t *testing.T) {
	input := []string{"apple", "banana", "apple", "cherry", "banana"}
	expected := []string{"apple", "banana", "cherry"}
	
	result := UniqueStrings(input)
	
	if len(result) != len(expected) {
		t.Errorf("Expected %d unique strings, got %d", len(expected), len(result))
	}
	
	for _, item := range expected {
		if !ContainsString(result, item) {
			t.Errorf("Expected result to contain %s", item)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		contains string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
	}
	
	for _, test := range tests {
		result := FormatBytes(test.bytes)
		if !strings.Contains(result, test.contains) {
			t.Errorf("Expected FormatBytes(%d) to contain %s, got %s", 
				test.bytes, test.contains, result)
		}
	}
}

func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()
	
	tests := []struct {
		time     time.Time
		contains string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-2 * time.Minute), "2 minutes ago"},
		{now.Add(-1 * time.Hour), "1 hour ago"},
		{now.Add(-25 * time.Hour), "1 day ago"},
	}
	
	for _, test := range tests {
		result := FormatTimeAgo(test.time)
		if result != test.contains {
			t.Errorf("Expected FormatTimeAgo to return %s, got %s", 
				test.contains, result)
		}
	}
}

func TestCoalesceString(t *testing.T) {
	result := CoalesceString("", "", "first", "second")
	if result != "first" {
		t.Errorf("Expected CoalesceString to return 'first', got %s", result)
	}
	
	result = CoalesceString("", "", "")
	if result != "" {
		t.Errorf("Expected CoalesceString to return empty string, got %s", result)
	}
}
