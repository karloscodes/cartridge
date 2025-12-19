package cartridge

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig configures the logger.
type LogConfig struct {
	// Level is the minimum log level. Defaults based on environment:
	// - Development: "info"
	// - Test: "info"
	// - Production: "error"
	// Can be overridden via LOG_LEVEL env var or this field.
	Level string

	// Directory for log files. Only used in production.
	// Defaults to "logs" in the current directory.
	Directory string

	// MaxSizeMB is the max size in megabytes before rotation.
	// Defaults to 100.
	MaxSizeMB int

	// MaxBackups is the max number of old log files to keep.
	// Defaults to 3.
	MaxBackups int

	// MaxAgeDays is the max age in days before a log file is deleted.
	// Defaults to 28.
	MaxAgeDays int

	// AppName is used in the log filename. Defaults to "app".
	AppName string
}

// LogConfigProvider allows configuration objects to provide log settings directly.
// Implement this interface on your config type to avoid manual LogConfig mapping.
type LogConfigProvider interface {
	GetLogLevel() string
	GetLogDirectory() string
	GetLogMaxSizeMB() int
	GetLogMaxBackups() int
	GetLogMaxAgeDays() int
	GetAppName() string
}

// LogConfigFromProvider creates a LogConfig from a LogConfigProvider.
func LogConfigFromProvider(p LogConfigProvider) *LogConfig {
	return &LogConfig{
		Level:      p.GetLogLevel(),
		Directory:  p.GetLogDirectory(),
		MaxSizeMB:  p.GetLogMaxSizeMB(),
		MaxBackups: p.GetLogMaxBackups(),
		MaxAgeDays: p.GetLogMaxAgeDays(),
		AppName:    p.GetAppName(),
	}
}

// NewLogger creates a configured slog.Logger based on the environment.
//
// If cfg implements LogConfigProvider, log settings are extracted automatically.
// Otherwise, provide explicit logCfg or use defaults.
//
// Development and Test:
//   - Logs to stdout only
//   - Uses colored text output
//   - Default level: info
//
// Production:
//   - Logs to both stdout and rotating file
//   - Uses JSON format
//   - Default level: error
//   - Files rotated via lumberjack
func NewLogger(cfg Config, logCfg *LogConfig) *slog.Logger {
	// Auto-extract log config if cfg implements LogConfigProvider
	if logCfg == nil {
		if provider, ok := cfg.(LogConfigProvider); ok {
			logCfg = LogConfigFromProvider(provider)
		} else {
			logCfg = &LogConfig{}
		}
	}

	// Determine log level
	level := resolveLogLevel(cfg, logCfg.Level)

	// Create appropriate handler based on environment
	if cfg.IsDevelopment() || cfg.IsTest() {
		return newDevLogger(level)
	}
	return newProdLogger(level, logCfg)
}

// resolveLogLevel determines the log level from config, env, or defaults.
func resolveLogLevel(cfg Config, configLevel string) slog.Level {
	// Check explicit config first
	levelStr := configLevel

	// Check env var override
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		levelStr = envLevel
	}

	// Use defaults if not set
	if levelStr == "" {
		if cfg.IsDevelopment() || cfg.IsTest() {
			levelStr = "info"
		} else {
			levelStr = "error"
		}
	}

	// Parse level string
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// newDevLogger creates a colored text logger for development/test.
func newDevLogger(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	// Use text handler with colors for dev/test
	handler := newColorHandler(os.Stdout, opts)
	return slog.New(handler)
}

// newProdLogger creates a JSON logger that writes to stdout and file.
func newProdLogger(level slog.Level, logCfg *LogConfig) *slog.Logger {
	// Apply defaults
	appName := logCfg.AppName
	if appName == "" {
		appName = "app"
	}

	dir := logCfg.Directory
	if dir == "" {
		dir = "logs"
	}

	maxSize := logCfg.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 100
	}

	maxBackups := logCfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 3
	}

	maxAge := logCfg.MaxAgeDays
	if maxAge <= 0 {
		maxAge = 28
	}

	// Ensure logs directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Fall back to stdout only if we can't create the directory
		opts := &slog.HandlerOptions{Level: level}
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}

	// Configure rotating file writer
	rotator := &lumberjack.Logger{
		Filename:   filepath.Join(dir, appName+".log"),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   true,
	}

	// Write to both stdout and file
	multiWriter := io.MultiWriter(os.Stdout, rotator)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	return slog.New(slog.NewJSONHandler(multiWriter, opts))
}

// colorHandler is a colored text handler for development.
type colorHandler struct {
	slog.Handler
	w     io.Writer
	level slog.Level
}

func newColorHandler(w io.Writer, opts *slog.HandlerOptions) *colorHandler {
	level := slog.LevelInfo
	if opts != nil && opts.Level != nil {
		level = opts.Level.Level()
	}
	return &colorHandler{
		Handler: slog.NewTextHandler(w, opts),
		w:       w,
		level:   level,
	}
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
)

func (h *colorHandler) Handle(ctx context.Context, r slog.Record) error {
	// Get level color
	var levelColor string
	switch r.Level {
	case slog.LevelDebug:
		levelColor = colorGray
	case slog.LevelInfo:
		levelColor = colorBlue
	case slog.LevelWarn:
		levelColor = colorYellow
	case slog.LevelError:
		levelColor = colorRed
	}

	// Format: [LEVEL] message key=value...
	timeStr := r.Time.Format("15:04:05")
	levelStr := r.Level.String()

	// Build output
	var buf strings.Builder
	buf.WriteString(colorGray)
	buf.WriteString(timeStr)
	buf.WriteString(colorReset)
	buf.WriteString(" ")
	buf.WriteString(levelColor)
	buf.WriteString(levelStr)
	buf.WriteString(colorReset)
	buf.WriteString(" ")
	buf.WriteString(r.Message)

	// Add attributes
	r.Attrs(func(a slog.Attr) bool {
		buf.WriteString(" ")
		buf.WriteString(colorGray)
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(colorReset)
		buf.WriteString(a.Value.String())
		return true
	})

	buf.WriteString("\n")
	_, err := h.w.Write([]byte(buf.String()))
	return err
}

func (h *colorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *colorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &colorHandler{
		Handler: h.Handler.WithAttrs(attrs),
		w:       h.w,
		level:   h.level,
	}
}

func (h *colorHandler) WithGroup(name string) slog.Handler {
	return &colorHandler{
		Handler: h.Handler.WithGroup(name),
		w:       h.w,
		level:   h.level,
	}
}
