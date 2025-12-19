package testsupport

// TestConfig implements cartridge.Config for testing.
type TestConfig struct {
	port            string
	environment     string
	publicDirectory string
	assetsPrefix    string
}

// NewTestConfig creates a test configuration with sensible defaults.
func NewTestConfig() *TestConfig {
	return &TestConfig{
		port:            "0", // Random port
		environment:     "test",
		publicDirectory: "",
		assetsPrefix:    "/assets",
	}
}

// IsDevelopment returns false for test config.
func (c *TestConfig) IsDevelopment() bool { return false }

// IsProduction returns false for test config.
func (c *TestConfig) IsProduction() bool { return false }

// IsTest returns true for test config.
func (c *TestConfig) IsTest() bool { return true }

// GetPort returns the configured port.
func (c *TestConfig) GetPort() string { return c.port }

// GetPublicDirectory returns the public assets directory.
func (c *TestConfig) GetPublicDirectory() string { return c.publicDirectory }

// GetAssetsPrefix returns the assets URL prefix.
func (c *TestConfig) GetAssetsPrefix() string { return c.assetsPrefix }

// TestLogger implements cartridge.Logger for testing.
// It discards all log messages by default.
type TestLogger struct {
	messages []LogMessage
	capture  bool
}

// LogMessage represents a captured log message.
type LogMessage struct {
	Level   string
	Message string
	Args    []any
}

// NewTestLogger creates a test logger that discards messages.
func NewTestLogger() *TestLogger {
	return &TestLogger{}
}

// NewCapturingTestLogger creates a test logger that captures messages.
func NewCapturingTestLogger() *TestLogger {
	return &TestLogger{capture: true}
}

// Debug logs a debug message.
func (l *TestLogger) Debug(msg string, keysAndValues ...any) {
	if l.capture {
		l.messages = append(l.messages, LogMessage{"debug", msg, keysAndValues})
	}
}

// Info logs an info message.
func (l *TestLogger) Info(msg string, keysAndValues ...any) {
	if l.capture {
		l.messages = append(l.messages, LogMessage{"info", msg, keysAndValues})
	}
}

// Warn logs a warning message.
func (l *TestLogger) Warn(msg string, keysAndValues ...any) {
	if l.capture {
		l.messages = append(l.messages, LogMessage{"warn", msg, keysAndValues})
	}
}

// Error logs an error message.
func (l *TestLogger) Error(msg string, keysAndValues ...any) {
	if l.capture {
		l.messages = append(l.messages, LogMessage{"error", msg, keysAndValues})
	}
}

// Messages returns all captured log messages.
func (l *TestLogger) Messages() []LogMessage {
	return l.messages
}

// Clear removes all captured messages.
func (l *TestLogger) Clear() {
	l.messages = nil
}
