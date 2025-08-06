package logging

import (
	"strings"
	"testing"
)

func TestNewDefaultLogger(t *testing.T) {
	logger := NewDefaultLogger()
	if logger == nil {
		t.Error("Expected logger to be created, got nil")
	}
	
	if logger.GetLevel() != LogLevelInfo {
		t.Errorf("Expected default log level to be info, got %s", logger.GetLevel())
	}
}

func TestLoggerSetLevel(t *testing.T) {
	logger := NewDefaultLogger()
	
	logger.SetLevel(LogLevelDebug)
	if logger.GetLevel() != LogLevelDebug {
		t.Errorf("Expected log level to be debug, got %s", logger.GetLevel())
	}
	
	logger.SetLevel(LogLevelError)
	if logger.GetLevel() != LogLevelError {
		t.Errorf("Expected log level to be error, got %s", logger.GetLevel())
	}
}

func TestLoggerFields(t *testing.T) {
	logger := NewDefaultLogger()
	
	// This test just ensures the methods don't panic
	logger.Debug("debug message", Field{Key: "test", Value: "value"})
	logger.Info("info message", Field{Key: "test", Value: "value"})
	logger.Warn("warn message", Field{Key: "test", Value: "value"})
	logger.Error("error message", Field{Key: "test", Value: "value"})
}

func TestShouldLog(t *testing.T) {
	logger := &simpleLogger{level: LogLevelInfo}
	
	// Info level should log info, warn, and error
	if !logger.shouldLog(LogLevelInfo) {
		t.Error("Expected info level to log info messages")
	}
	if !logger.shouldLog(LogLevelWarn) {
		t.Error("Expected info level to log warn messages")
	}
	if !logger.shouldLog(LogLevelError) {
		t.Error("Expected info level to log error messages")
	}
	if logger.shouldLog(LogLevelDebug) {
		t.Error("Expected info level not to log debug messages")
	}
}

func TestColorizeLevel(t *testing.T) {
	logger := &simpleLogger{useColor: true}
	
	tests := []struct {
		level    string
		contains string
	}{
		{"DEBUG", "\033[37m"},
		{"INFO", "\033[34m"},
		{"WARN", "\033[33m"},
		{"ERROR", "\033[31m"},
	}
	
	for _, test := range tests {
		result := logger.colorizeLevel(test.level)
		if !strings.Contains(result, test.contains) {
			t.Errorf("Expected colorized level %s to contain %s", test.level, test.contains)
		}
		if !strings.Contains(result, test.level) {
			t.Errorf("Expected colorized level to contain original level %s", test.level)
		}
	}
}
