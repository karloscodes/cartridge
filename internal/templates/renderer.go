package templates

import (
	"fmt"
)

// Logger interface for template engine logging
type Logger interface {
	Debug(msg string, args ...interface{})
}

// Config interface for template engine configuration
type Config interface {
	IsDevelopment() bool
	IsDebug() bool
	GetEnvironment() string
}

// Engine manages template rendering and helper functions
type Engine struct {
	logger    Logger
	config    Config
	functions map[string]interface{}
}

// NewEngine creates a new template engine instance
func NewEngine(logger Logger, config Config) *Engine {
	return &Engine{
		logger:    logger,
		config:    config,
		functions: GetDefaultTemplateFunctions(),
	}
}

// RenderPage renders a template with common page data
func (e *Engine) RenderPage(ctx interface{}, page string, data map[string]interface{}, status int) error {
	// TODO: This would be implemented when Fiber is available
	e.logger.Debug("Rendering page",
		"page", page,
		"status", status)

	return nil
}

// RenderSuccess renders a page with success status
func (e *Engine) RenderSuccess(ctx interface{}, page string, data map[string]interface{}) error {
	return e.RenderPage(ctx, page, data, 200)
}

// RenderError renders an error page
func (e *Engine) RenderError(ctx interface{}, title, message string, status int) error {
	data := map[string]interface{}{
		"Title":   title,
		"Message": message,
		"Status":  status,
	}

	errorPage := "error"
	if status == 404 {
		errorPage = "404"
	}

	return e.RenderPage(ctx, errorPage, data, status)
}

// RenderNotFound renders a 404 page
func (e *Engine) RenderNotFound(ctx interface{}) error {
	return e.RenderError(ctx, "Page Not Found", "The requested page could not be found.", 404)
}

// RenderInternalError renders a 500 page
func (e *Engine) RenderInternalError(ctx interface{}) error {
	message := "An internal server error occurred."
	if e.config.IsDevelopment() {
		message = "An internal server error occurred. Check the logs for more details."
	}

	return e.RenderError(ctx, "Internal Server Error", message, 500)
}

// BuildCommonPageData builds common data that should be available to all templates
func (e *Engine) BuildCommonPageData(ctx interface{}) CommonPageData {
	data := CommonPageData{
		Environment: e.config.GetEnvironment(),
		Debug:       e.config.IsDebug(),
		Title:       "Cartridge App",
	}

	// TODO: This would extract data from Fiber context when available
	// For now, return basic data

	e.logger.Debug("Built common page data",
		"environment", data.Environment,
		"debug", data.Debug)

	return data
}

// ResponseHelper provides template rendering utilities - public wrapper
type ResponseHelper struct {
	engine *Engine
}

// NewResponseHelper creates a new response helper
func NewResponseHelper(logger Logger, config Config) *ResponseHelper {
	return &ResponseHelper{
		engine: NewEngine(logger, config),
	}
}

// RenderPage renders a template with common page data
func (rh *ResponseHelper) RenderPage(ctx interface{}, page string, data map[string]interface{}, status int) error {
	return rh.engine.RenderPage(ctx, page, data, status)
}

// RenderSuccess renders a page with success status
func (rh *ResponseHelper) RenderSuccess(ctx interface{}, page string, data map[string]interface{}) error {
	return rh.engine.RenderSuccess(ctx, page, data)
}

// RenderError renders an error page
func (rh *ResponseHelper) RenderError(ctx interface{}, title, message string, status int) error {
	return rh.engine.RenderError(ctx, title, message, status)
}

// RenderNotFound renders a 404 page
func (rh *ResponseHelper) RenderNotFound(ctx interface{}) error {
	return rh.engine.RenderNotFound(ctx)
}

// RenderInternalError renders a 500 page
func (rh *ResponseHelper) RenderInternalError(ctx interface{}) error {
	return rh.engine.RenderInternalError(ctx)
}

// BuildCommonPageData builds common data that should be available to all templates
func BuildCommonPageData(ctx interface{}, logger Logger, config Config) CommonPageData {
	engine := NewEngine(logger, config)
	return engine.BuildCommonPageData(ctx)
}

// SetFlash sets a flash message in the session
func SetFlash(ctx interface{}, flashType, message string) {
	SetFlashWithTitle(ctx, flashType, message, "")
}

// SetFlashWithTitle sets a flash message with a title in the session
func SetFlashWithTitle(ctx interface{}, flashType, message, title string) {
	// TODO: This would store the flash message in the session when Fiber is available
	// For now, just log it
	if title != "" {
		fmt.Printf("Flash message set: %s - %s: %s\n", flashType, title, message)
	} else {
		fmt.Printf("Flash message set: %s - %s\n", flashType, message)
	}
}

// GetFlash retrieves and clears a flash message from the session
func GetFlash(ctx interface{}) *FlashMessage {
	// TODO: This would retrieve the flash message from the session when Fiber is available
	// For now, return nil
	return nil
}

// TemplateFunction represents a custom template function
type TemplateFunction struct {
	Name string
	Func interface{}
}

// GetTemplateFunctions returns custom template functions
func GetTemplateFunctions() map[string]interface{} {
	return GetDefaultTemplateFunctions()
}

// GetDefaultTemplateFunctions returns custom template functions
func GetDefaultTemplateFunctions() map[string]interface{} {
	return map[string]interface{}{
		"dict": func(values ...interface{}) map[string]interface{} {
			if len(values)%2 != 0 {
				return nil
			}
			dict := make(map[string]interface{})
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					continue
				}
				dict[key] = values[i+1]
			}
			return dict
		},
		"formatBytes": func(bytes int64) string {
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
		},
		"formatDuration": func(ms float64) string {
			if ms < 1000 {
				return fmt.Sprintf("%.1fms", ms)
			}
			seconds := ms / 1000
			if seconds < 60 {
				return fmt.Sprintf("%.1fs", seconds)
			}
			minutes := seconds / 60
			return fmt.Sprintf("%.1fm", minutes)
		},
		"truncate": func(s string, length int) string {
			if len(s) <= length {
				return s
			}
			return s[:length] + "..."
		},
		"contains": func(s, substr string) bool {
			return fmt.Sprintf("%s", s) != "" && fmt.Sprintf("%s", substr) != "" &&
				len(s) >= len(substr) && s != substr
		},
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"mul": func(a, b int) int {
			return a * b
		},
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}
}

// DefaultPageMetadata returns default page metadata
func DefaultPageMetadata() PageMetadata {
	return PageMetadata{
		Title:       "Cartridge App",
		Description: "A web application built with Cartridge framework",
		Keywords:    "go, web, application, cartridge",
		Author:      "Cartridge Team",
	}
}

// NewFormHelper creates a new form helper
func NewFormHelper(csrfToken string) *FormHelper {
	return &FormHelper{
		csrfToken: csrfToken,
	}
}

// CommonPageData holds data that's common to all page templates
type CommonPageData struct {
	IsAuthenticated bool
	CurrentUser     interface{} // Will be *User when user model is available
	CsrfToken       string
	Flash           *FlashMessage
	Environment     string
	Debug           bool
	Title           string
	PageClass       string
}

// FlashMessage represents a flash message for user feedback
type FlashMessage struct {
	Type    string `json:"type"` // success, error, warning, info
	Message string `json:"message"`
	Title   string `json:"title,omitempty"`
}

// PageMetadata represents metadata for a page
type PageMetadata struct {
	Title       string
	Description string
	Keywords    string
	Author      string
	Image       string
	URL         string
}

// Breadcrumb represents a navigation breadcrumb
type Breadcrumb struct {
	Text string
	URL  string
}

// Navigation represents page navigation data
type Navigation struct {
	Breadcrumbs []Breadcrumb
	CurrentPage string
}

// ErrorPageData represents data for error pages
type ErrorPageData struct {
	CommonPageData
	Status  int
	Title   string
	Message string
	Details string
}

// SuccessPageData represents data for success pages
type SuccessPageData struct {
	CommonPageData
	Title   string
	Message string
	NextURL string
}

// FormHelper provides form rendering utilities
type FormHelper struct {
	csrfToken string
}

// CSRFField returns the CSRF token field HTML
func (fh *FormHelper) CSRFField() string {
	return fmt.Sprintf(`<input type="hidden" name="_csrf" value="%s">`, fh.csrfToken)
}

// MethodField returns the method override field HTML
func (fh *FormHelper) MethodField(method string) string {
	return fmt.Sprintf(`<input type="hidden" name="_method" value="%s">`, method)
}

// Flash message type constants
const (
	FlashSuccess = "success"
	FlashError   = "error"
	FlashWarning = "warning"
	FlashInfo    = "info"
)