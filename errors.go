package cartridge

import (
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v2"
)

// DefaultErrorHandler returns a production-ready error handler.
// It returns JSON for API requests and simple HTML for browser requests.
// For custom error pages with templates, use WithErrorHandler to provide your own.
func DefaultErrorHandler(logger *slog.Logger, isDev bool) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		logger.Error("request failed",
			slog.Any("error", err),
			slog.String("path", c.Path()),
			slog.String("method", c.Method()),
			slog.Int("status", code),
		)

		// JSON error response for API requests
		if c.Accepts(fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
			return c.Status(code).JSON(fiber.Map{
				"error":   ErrorCodeName(code),
				"message": err.Error(),
			})
		}

		// Simple HTML error page for browser requests
		errorMsg := ""
		if isDev {
			errorMsg = err.Error()
		}
		return c.Status(code).SendString(errorHTML(code, ErrorCodeName(code), errorMsg))
	}
}

// ErrorCodeName returns a human-readable name for common HTTP status codes.
func ErrorCodeName(code int) string {
	switch code {
	case fiber.StatusBadRequest:
		return "Bad Request"
	case fiber.StatusUnauthorized:
		return "Unauthorized"
	case fiber.StatusForbidden:
		return "Forbidden"
	case fiber.StatusNotFound:
		return "Not Found"
	case fiber.StatusMethodNotAllowed:
		return "Method Not Allowed"
	case fiber.StatusTooManyRequests:
		return "Too Many Requests"
	case fiber.StatusInternalServerError:
		return "Internal Server Error"
	case fiber.StatusBadGateway:
		return "Bad Gateway"
	case fiber.StatusServiceUnavailable:
		return "Service Unavailable"
	default:
		return "Error"
	}
}

// errorHTML generates a simple, styled HTML error page.
func errorHTML(code int, title, message string) string {
	details := ""
	if message != "" {
		details = fmt.Sprintf(`<p style="color:#666;font-size:14px;margin-top:20px;font-family:monospace;background:#f5f5f5;padding:10px;border-radius:4px;">%s</p>`, message)
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
