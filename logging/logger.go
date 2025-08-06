package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogLevel represents the logging level
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Field represents a structured logging field
type Field struct {
	Key   string
	Value interface{}
}

// Logger interface for structured logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	SetLevel(level LogLevel)
	GetLevel() LogLevel
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level         LogLevel
	Directory     string
	MaxSize       int
	MaxBackups    int
	MaxAge        int
	UseJSON       bool
	UseColor      bool
	EnableConsole bool
}

// simpleLogger implements the Logger interface
type simpleLogger struct {
	level         LogLevel
	fileLogger    *log.Logger
	consoleLogger *log.Logger
	useJSON       bool
	useColor      bool
	enableConsole bool
	logFile       *os.File
}

// NewLogger creates a new logger instance
func NewLogger(config LogConfig) Logger {
	logger := &simpleLogger{
		level:         config.Level,
		useJSON:       config.UseJSON,
		useColor:      config.UseColor,
		enableConsole: config.EnableConsole,
	}

	// Setup file logging if directory is provided
	if config.Directory != "" {
		if err := os.MkdirAll(config.Directory, 0755); err == nil {
			logFilePath := filepath.Join(config.Directory, "app.log")
			if file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
				logger.logFile = file
				logger.fileLogger = log.New(file, "", 0)
			}
		}
	}

	// Setup console logging
	if logger.enableConsole {
		logger.consoleLogger = log.New(os.Stdout, "", 0)
	}

	return logger
}

// NewDefaultLogger creates a logger with sensible defaults
func NewDefaultLogger() Logger {
	return NewLogger(LogConfig{
		Level:         LogLevelInfo,
		Directory:     "",
		UseJSON:       false,
		UseColor:      true,
		EnableConsole: true,
	})
}

// Debug logs a debug message
func (l *simpleLogger) Debug(msg string, fields ...Field) {
	if l.shouldLog(LogLevelDebug) {
		l.log(LogLevelDebug, msg, fields...)
	}
}

// Info logs an info message
func (l *simpleLogger) Info(msg string, fields ...Field) {
	if l.shouldLog(LogLevelInfo) {
		l.log(LogLevelInfo, msg, fields...)
	}
}

// Warn logs a warning message
func (l *simpleLogger) Warn(msg string, fields ...Field) {
	if l.shouldLog(LogLevelWarn) {
		l.log(LogLevelWarn, msg, fields...)
	}
}

// Error logs an error message
func (l *simpleLogger) Error(msg string, fields ...Field) {
	if l.shouldLog(LogLevelError) {
		l.log(LogLevelError, msg, fields...)
	}
}

// Fatal logs a fatal message and exits
func (l *simpleLogger) Fatal(msg string, fields ...Field) {
	l.log(LogLevelError, msg, fields...)
	os.Exit(1)
}

// SetLevel sets the logging level
func (l *simpleLogger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *simpleLogger) GetLevel() LogLevel {
	return l.level
}

// shouldLog determines if a message should be logged based on level
func (l *simpleLogger) shouldLog(level LogLevel) bool {
	levels := map[LogLevel]int{
		LogLevelDebug: 0,
		LogLevelInfo:  1,
		LogLevelWarn:  2,
		LogLevelError: 3,
	}

	currentLevel, exists := levels[l.level]
	if !exists {
		currentLevel = 1 // Default to info
	}

	messageLevel, exists := levels[level]
	if !exists {
		messageLevel = 1 // Default to info
	}

	return messageLevel >= currentLevel
}

// log writes the actual log message
func (l *simpleLogger) log(level LogLevel, msg string, fields ...Field) {
	timestamp := time.Now().Format(time.RFC3339)

	if l.useJSON {
		l.logJSON(timestamp, level, msg, fields...)
	} else {
		l.logText(timestamp, level, msg, fields...)
	}
}

// logJSON writes log in JSON format
func (l *simpleLogger) logJSON(timestamp string, level LogLevel, msg string, fields ...Field) {
	logEntry := map[string]interface{}{
		"timestamp": timestamp,
		"level":     level,
		"message":   msg,
	}

	// Add fields
	for _, field := range fields {
		logEntry[field.Key] = field.Value
	}

	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		return
	}

	logLine := string(jsonData)

	// Write to file
	if l.fileLogger != nil {
		l.fileLogger.Println(logLine)
	}

	// Write to console
	if l.consoleLogger != nil {
		l.consoleLogger.Println(logLine)
	}
}

// logText writes log in human-readable format
func (l *simpleLogger) logText(timestamp string, level LogLevel, msg string, fields ...Field) {
	levelStr := strings.ToUpper(string(level))
	
	// Add color if enabled
	if l.useColor && l.consoleLogger != nil {
		levelStr = l.colorizeLevel(levelStr)
	}

	logLine := fmt.Sprintf("%s [%s] %s", timestamp, levelStr, msg)

	// Add fields
	if len(fields) > 0 {
		fieldParts := make([]string, len(fields))
		for i, field := range fields {
			fieldParts[i] = fmt.Sprintf("%s=%v", field.Key, field.Value)
		}
		logLine += " " + strings.Join(fieldParts, " ")
	}

	// Write to file (without colors)
	if l.fileLogger != nil {
		plainLogLine := fmt.Sprintf("%s [%s] %s", timestamp, strings.ToUpper(string(level)), msg)
		if len(fields) > 0 {
			fieldParts := make([]string, len(fields))
			for i, field := range fields {
				fieldParts[i] = fmt.Sprintf("%s=%v", field.Key, field.Value)
			}
			plainLogLine += " " + strings.Join(fieldParts, " ")
		}
		l.fileLogger.Println(plainLogLine)
	}

	// Write to console (with colors if enabled)
	if l.consoleLogger != nil {
		l.consoleLogger.Println(logLine)
	}
}

// colorizeLevel adds ANSI color codes to log levels
func (l *simpleLogger) colorizeLevel(level string) string {
	const (
		ColorReset  = "\033[0m"
		ColorRed    = "\033[31m"
		ColorYellow = "\033[33m"
		ColorBlue   = "\033[34m"
		ColorGray   = "\033[37m"
	)

	switch level {
	case "DEBUG":
		return ColorGray + level + ColorReset
	case "INFO":
		return ColorBlue + level + ColorReset
	case "WARN":
		return ColorYellow + level + ColorReset
	case "ERROR":
		return ColorRed + level + ColorReset
	default:
		return level
	}
}

// Close closes the log file if open
func (l *simpleLogger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// SetOutput sets a custom output writer
func (l *simpleLogger) SetOutput(w io.Writer) {
	if l.fileLogger != nil {
		l.fileLogger.SetOutput(w)
	}
	if l.consoleLogger != nil {
		l.consoleLogger.SetOutput(w)
	}
}
