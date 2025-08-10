package errors

import (
	"fmt"
	"runtime"
	"strings"
)

// Logger interface to avoid import cycles
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	With(args ...interface{}) Logger
}

// Config interface to avoid import cycles
type Config interface {
	IsDevelopment() bool
}

// ErrorHandler manages error handling and reporting
type ErrorHandler struct {
	logger Logger
	config Config
}

// NewErrorHandler creates a new error handler instance
func NewErrorHandler(logger Logger, config Config) *ErrorHandler {
	return &ErrorHandler{
		logger: logger,
		config: config,
	}
}

// ErrorPageData represents data for error pages
type ErrorPageData struct {
	Status      int
	Title       string
	Message     string
	Details     string
	StackTrace  string
	Environment string
	Debug       bool
}

// HandleError processes an error and generates appropriate error page data
func (eh *ErrorHandler) HandleError(err error, status int) ErrorPageData {
	data := ErrorPageData{
		Status:      status,
		Environment: "development", // TODO: Get from config
		Debug:       eh.config.IsDevelopment(),
	}

	// Set title based on status code
	switch status {
	case 400:
		data.Title = "Bad Request"
	case 401:
		data.Title = "Unauthorized"
	case 403:
		data.Title = "Forbidden"
	case 404:
		data.Title = "Page Not Found"
	case 500:
		data.Title = "Internal Server Error"
	default:
		data.Title = fmt.Sprintf("Error %d", status)
	}

	// Set message
	if err != nil {
		data.Message = err.Error()
		eh.logger.Error("Error occurred", "status", status, "error", err)
	} else {
		data.Message = "An error occurred"
	}

	// Add stack trace in development mode
	if eh.config.IsDevelopment() && err != nil {
		data.StackTrace = eh.captureStackTrace()
		data.Details = fmt.Sprintf("Error: %v", err)
	}

	return data
}

// captureStackTrace captures the current stack trace for debugging
func (eh *ErrorHandler) captureStackTrace() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:]) // Skip runtime.Callers, captureStackTrace, and HandleError

	frames := runtime.CallersFrames(pcs[:n])
	var stackTrace strings.Builder

	for {
		frame, more := frames.Next()
		
		// Filter out runtime and internal Go frames
		if !strings.Contains(frame.File, "runtime/") &&
		   !strings.Contains(frame.File, "internal/") {
			stackTrace.WriteString(fmt.Sprintf("%s:%d %s\n", 
				frame.File, frame.Line, frame.Function))
		}

		if !more {
			break
		}
	}

	return stackTrace.String()
}

// RenderErrorPage renders an error page with the given error data
func (eh *ErrorHandler) RenderErrorPage(data ErrorPageData) string {
	// TODO: This would render an actual HTML template in a real implementation
	// For now, return a simple text representation
	
	if eh.config.IsDevelopment() && data.StackTrace != "" {
		return fmt.Sprintf(`
Error %d: %s

Message: %s
Details: %s

Stack Trace:
%s
`, data.Status, data.Title, data.Message, data.Details, data.StackTrace)
	}

	return fmt.Sprintf(`
Error %d: %s

Message: %s
`, data.Status, data.Title, data.Message)
}

// LogError logs an error with appropriate severity
func (eh *ErrorHandler) LogError(err error, context string, fields ...interface{}) {
	args := []interface{}{"context", context, "error", err}
	args = append(args, fields...)
	
	eh.logger.Error("Application error", args...)
}

// LogWarning logs a warning
func (eh *ErrorHandler) LogWarning(msg string, fields ...interface{}) {
	eh.logger.Warn(msg, fields...)
}

// IsClientError returns true if the status code represents a client error (4xx)
func IsClientError(status int) bool {
	return status >= 400 && status < 500
}

// IsServerError returns true if the status code represents a server error (5xx)
func IsServerError(status int) bool {
	return status >= 500 && status < 600
}

// GetDefaultErrorMessage returns a default error message for the given status code
func GetDefaultErrorMessage(status int) string {
	switch status {
	case 400:
		return "The request was invalid or malformed."
	case 401:
		return "Authentication is required to access this resource."
	case 403:
		return "You do not have permission to access this resource."
	case 404:
		return "The requested resource could not be found."
	case 500:
		return "An internal server error occurred."
	case 502:
		return "Bad gateway error."
	case 503:
		return "Service temporarily unavailable."
	default:
		return "An error occurred while processing your request."
	}
}