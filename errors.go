package cartridge

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
)

// Error is an HTTP error with a status code. Return one from a handler to
// choose the response status.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

// NewError creates an Error. The message defaults to the status text.
func NewError(code int, message ...string) *Error {
	msg := http.StatusText(code)
	if len(message) > 0 {
		msg = message[0]
	}
	return &Error{Code: code, Message: msg}
}

// ErrorHandler turns an error from a handler chain into a response.
type ErrorHandler func(*Context, error) error

// DefaultErrorHandler is the error handler a server uses when
// ServerConfig.ErrorHandler is nil. The client sees only the status text,
// or the Message of an *Error with a code below 500. The log gets the full
// error. In development, the HTML page also shows the full error.
func DefaultErrorHandler(logger *slog.Logger, isDev bool) ErrorHandler {
	return func(c *Context, err error) error {
		code := errorCode(err)

		attrs := []any{
			slog.Any("error", err),
			slog.Int("status", code),
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
		}
		if code >= http.StatusInternalServerError {
			logger.Error("request failed", attrs...)
		} else {
			logger.Info("request rejected", attrs...)
		}

		title := http.StatusText(code)
		if c.Accepts("application/json") == "application/json" {
			return c.Status(code).JSON(Map{"error": title, "message": clientMessage(err, code)})
		}

		details := ""
		if isDev {
			details = err.Error()
		} else if msg := clientMessage(err, code); msg != title {
			details = msg
		}
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Status(code).SendString(errorHTML(code, title, details))
	}
}

// errorCode returns the status of an *Error, or 500.
func errorCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return http.StatusInternalServerError
}

// clientMessage is the error text that is safe to send to the client: the
// Message of an *Error with a code below 500, or else the status text.
func clientMessage(err error, code int) string {
	var e *Error
	if errors.As(err, &e) && e.Code < http.StatusInternalServerError && e.Message != "" {
		return e.Message
	}
	return http.StatusText(code)
}

// errorHTML generates a simple, styled HTML error page.
func errorHTML(code int, title, message string) string {
	details := ""
	if message != "" {
		details = fmt.Sprintf(`<p style="color:#666;font-size:14px;margin-top:20px;font-family:monospace;background:#f5f5f5;padding:10px;border-radius:4px;">%s</p>`, html.EscapeString(message))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%d - %s</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f8f9fa;
            color: #333;
        }
        .container {
            text-align: center;
            padding: 40px;
            max-width: 500px;
        }
        h1 {
            font-size: 72px;
            margin: 0;
            color: #dc3545;
        }
        h2 {
            font-size: 24px;
            margin: 10px 0 20px;
            color: #666;
        }
        a {
            color: #007bff;
            text-decoration: none;
        }
        a:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>%d</h1>
        <h2>%s</h2>
        <p><a href="/">← Go back home</a></p>
        %s
    </div>
</body>
</html>`, code, title, code, title, details)
}
