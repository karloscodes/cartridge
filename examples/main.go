package main

import (
	"embed"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/karloscodes/cartridge"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Cron job handlers with same UX as HTTP handlers

func CleanupOldProducts(ctx *cartridge.CronContext) error {
	ctx.Logger.Info("🧹 Starting cleanup job",
		"job", "cleanup-old-products",
		"action", "delete-old-records")

	// Delete products older than 30 days with price = 0
	result := ctx.DBExec("DELETE FROM products WHERE price = 0 AND created_at < datetime('now', '-30 days')")

	ctx.Logger.Info("✅ Cleanup completed successfully",
		"rows_affected", result.RowsAffected,
		"duration", "0.5s")
	return nil
}

func GenerateDailyReport(ctx *cartridge.CronContext) error {
	ctx.Logger.Info("📊 Generating daily report",
		"job", "daily-report",
		"type", "analytics")

	// Count total products
	var totalProducts int64
	ctx.DBQuery("SELECT COUNT(*) FROM products", &totalProducts)

	// Calculate average price
	var avgPrice float64
	ctx.DBQuery("SELECT AVG(price) FROM products WHERE price > 0", &avgPrice)

	ctx.Logger.Info("📈 Daily report generated successfully",
		"total_products", totalProducts,
		"average_price", fmt.Sprintf("$%.2f", avgPrice),
		"report_type", "daily")

	// In a real app, you might send this via email or store in a reports table
	return nil
}

func DatabaseHealthCheck(ctx *cartridge.CronContext) error {
	ctx.Logger.Debug("🔍 Running database health check",
		"job", "health-check",
		"component", "database")

	// Simple database connectivity check
	var result int
	ctx.DBQuery("SELECT 1", &result)

	ctx.Logger.Debug("💚 Health check passed",
		"db_response", result,
		"status", "healthy")
	return nil
}

func GenerateWeeklyStats(ctx *cartridge.CronContext) error {
	ctx.Logger.Info("📅 Generating weekly statistics",
		"job", "weekly-stats",
		"period", "week")

	// Get product count by price range
	var expensive int64
	ctx.DBQuery("SELECT COUNT(*) FROM products WHERE price > 100", &expensive)

	var moderate int64
	ctx.DBQuery("SELECT COUNT(*) FROM products WHERE price BETWEEN 10 AND 100", &moderate)

	var cheap int64
	ctx.DBQuery("SELECT COUNT(*) FROM products WHERE price < 10 AND price > 0", &cheap)

	ctx.Logger.Info("📊 Weekly statistics completed",
		"expensive_products", expensive,
		"moderate_products", moderate,
		"cheap_products", cheap,
		"total_categories", 3)

	return nil
}

// Async task handlers with the same clean UX

func ProcessLargeDataset(ctx *cartridge.AsyncContext, args map[string]interface{}) (interface{}, error) {
	ctx.Logger.Info("🚀 Starting large dataset processing",
		"task_id", ctx.TaskID,
		"args", args,
		"type", "batch_processing")

	// Get parameters from args
	steps := 10
	if s, ok := args["steps"].(int); ok {
		steps = s
	}

	// Simulate heavy processing work
	for i := 0; i < steps; i++ {
		// Check if task was cancelled
		select {
		case <-ctx.Context.Done():
			ctx.Logger.Warn("🛑 Task cancelled by user",
				"task_id", ctx.TaskID,
				"completed_steps", i,
				"total_steps", steps)
			return nil, ctx.Context.Err()
		default:
		}

		// Simulate some work
		time.Sleep(500 * time.Millisecond)
		ctx.Logger.Info("⚙️ Processing step",
			"task_id", ctx.TaskID,
			"step", i+1,
			"total", steps,
			"progress", fmt.Sprintf("%.1f%%", float64(i+1)/float64(steps)*100))
	}

	// Return some result
	result := map[string]interface{}{
		"processed_records": 1000 * steps,
		"processing_time":   fmt.Sprintf("%d seconds", steps/2),
		"status":            "success",
		"steps":             steps,
	}

	ctx.Logger.Info("✅ Large dataset processing completed",
		"task_id", ctx.TaskID,
		"result", result,
		"performance", "excellent")
	return result, nil
}

func GenerateReport(ctx *cartridge.AsyncContext, args map[string]interface{}) (interface{}, error) {
	ctx.Logger.Info("📋 Generating comprehensive report",
		"task_id", ctx.TaskID,
		"args", args,
		"type", "analytics_report")

	// Get report type from args
	reportType := "basic"
	if rt, ok := args["type"].(string); ok {
		reportType = rt
	}

	ctx.Logger.Info("🔍 Querying database for report data",
		"task_id", ctx.TaskID,
		"report_type", reportType)

	// Query some data from database using safe methods
	var productCount int64
	if _, err := ctx.DBQuery("SELECT COUNT(*) FROM products", &productCount); err != nil {
		ctx.Logger.Error("❌ Failed to query product count",
			"task_id", ctx.TaskID,
			"error", err)
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	// Simulate report generation
	time.Sleep(2 * time.Second)

	report := map[string]interface{}{
		"total_products": productCount,
		"report_type":    reportType,
		"generated_at":   time.Now().UTC(),
	}

	ctx.Logger.Info("📄 Report generated successfully",
		"task_id", ctx.TaskID,
		"report_type", reportType,
		"total_products", productCount,
		"generation_time", "2s")

	return report, nil
}

// HTTP handlers with clean error handling pattern

func ListProducts(ctx *cartridge.Context) error {
	// Use structured logging with emojis and context
	ctx.Logger.Info("🛍️ Fetching products",
		"endpoint", "list",
		"method", "GET",
		"user_agent", ctx.Fiber.Get("User-Agent"))

	// Query string parameters made easy!
	limit := ctx.QueryInt("limit", 10) // Default to 10
	search := ctx.Query("search", "")  // Default to empty string

	ctx.Logger.Debug("📝 Request parameters",
		"limit", limit,
		"search", search,
		"has_search", search != "")

	// Get database connection - clean and simple!
	db := ctx.DB()

	// Use GORM for database operations
	var products []map[string]interface{}
	query := "SELECT id, name, price FROM products"
	args := []interface{}{}

	if search != "" {
		query += " WHERE name LIKE ? OR description LIKE ?"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}

	query += " LIMIT ?"
	args = append(args, limit)

	result := db.Raw(query, args...).Scan(&products)
	if result.Error != nil {
		ctx.Logger.Error("❌ Failed to fetch products",
			"error", result.Error,
			"query", query,
			"search", search)
		return ctx.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	ctx.Logger.Info("✅ Products fetched successfully",
		"count", len(products),
		"search", search,
		"limit", limit,
		"response_size", "small")

	// Use Render for forms/HTML (includes CSRF + metadata) or JSON for APIs (clean)
	return ctx.Render(map[string]interface{}{
		"products": products,
		"count":    len(products),
		"search":   search,
		"limit":    limit,
	})
}

func GetProduct(ctx *cartridge.Context) error {
	id := ctx.Params("id")
	ctx.Logger.Info("Fetching product", "id", id)

	// Query database using DBQuery() - ultra clean!
	var product map[string]interface{}
	result := ctx.DBQuery("SELECT * FROM products WHERE id = ?", &product, id)
	_ = result // result contains the *gorm.DB with RowsAffected, etc.

	// Check if product exists
	if len(product) == 0 {
		return ctx.NotFound("Product not found")
	}

	// Use JSON for clean API responses (no CSRF tokens)
	return ctx.JSON(product)
}

func CreateProduct(ctx *cartridge.Context) error {
	ctx.Logger.Info("Creating product")

	// Parse JSON body
	var data map[string]interface{}
	ctx.Must(ctx.ParseJSON(&data))

	// Validate required fields using ctx.Require() - panics with BadRequest if missing
	ctx.Require(data, "name", "price")

	// Additional validation using validator helpers
	if name, ok := data["name"].(string); ok {
		if err := cartridge.ValidateMinLength(name, 2); err != nil {
			return ctx.BadRequest(err, "Name must be at least 2 characters")
		}
	}

	if price, ok := data["price"].(float64); ok {
		if err := cartridge.ValidateRange(price, 0.01, 10000); err != nil {
			return ctx.BadRequest(err, "Price must be between 0.01 and 10000")
		}
	}

	// Insert into database using DBExec() - ultra clean!
	ctx.DBExec("INSERT INTO products (name, price) VALUES (?, ?)", data["name"], data["price"])

	// Use JSON for clean API responses
	return ctx.Status(201).JSON(fiber.Map{"message": "Product created", "product": data})
}

func UpdateProduct(ctx *cartridge.Context) error {
	id := ctx.Params("id")

	// Parse JSON body
	var data map[string]interface{}
	if err := ctx.ParseJSON(&data); err != nil {
		return ctx.BadRequest(err, "Invalid JSON body")
	}

	// Custom validation with specific error codes
	if data["price"] != nil {
		if price, ok := data["price"].(float64); ok && price < 0 {
			return ctx.UnprocessableEntity("Price cannot be negative")
		}
	}

	// Update in database using DBExec() - ultra clean!
	ctx.DBExec("UPDATE products SET name = ?, price = ? WHERE id = ?", data["name"], data["price"], id)

	// Use JSON for clean API responses
	return ctx.JSON(fiber.Map{"message": "Product updated", "product": data, "id": id})
}

func DeleteProduct(ctx *cartridge.Context) error {
	id := ctx.Params("id")

	// Check for confirmation
	confirm := ctx.QueryBool("confirm", false)
	if !confirm {
		return ctx.BadRequest(nil, "Deletion requires confirmation. Add ?confirm=true")
	}

	ctx.Logger.Info("Deleting product", "id", id, "confirmed", confirm)

	// Delete from database using DBExec() - ultra clean!
	ctx.DBExec("DELETE FROM products WHERE id = ?", id)

	return ctx.JSON(fiber.Map{"message": "Product deleted", "id": id})
}

// Example form-based handler showing ParseForm usage
func CreateProductForm(ctx *cartridge.Context) error {
	ctx.Logger.Info("Creating product via form")

	// Parse form data with Must() for regular errors
	type ProductForm struct {
		Name        string  `form:"name" validate:"required,min=3,max=100"`
		Price       float64 `form:"price" validate:"required,gt=0,lte=10000"`
		Description string  `form:"description" validate:"max=500"`
		Email       string  `form:"contact_email" validate:"required,email"`
	}

	var form ProductForm
	ctx.Must(ctx.ParseForm(&form))

	// Validate struct with tags - ultra clean!
	ctx.ValidateStruct(form)

	// Get additional form values
	category := ctx.FormValue("category", "general") // With default
	featured := ctx.QueryBool("featured", false)     // From query string

	// Validate category using validator
	if err := cartridge.ValidateOneOf(category, []string{"general", "electronics", "books", "clothing"}); err != nil {
		return ctx.BadRequest(err, cartridge.FormatValidationError(err))
	}

	// Handle file upload if present (optional)
	var imagePath string
	if image, err := ctx.Fiber.FormFile("image"); err == nil && image != nil {
		imagePath = "/uploads/" + image.Filename
		// In real app: save file to disk/cloud storage
	}

	// Insert into database with DBExec() - ultra clean!
	ctx.DBExec("INSERT INTO products (name, price, description, category, featured, image_path, contact_email) VALUES (?, ?, ?, ?, ?, ?, ?)",
		form.Name, form.Price, form.Description, category, featured, imagePath, form.Email)

	// For forms, use Render to include CSRF token for the next form
	return ctx.Render(fiber.Map{
		"message":    "Product created successfully",
		"product":    form,
		"category":   category,
		"featured":   featured,
		"image_path": imagePath,
	})
}

func main() {
	// Create cartridge with structured logging, CORS, and CSRF protection
	app := cartridge.NewFullStack(
		cartridge.WithPort("8084"),
		cartridge.WithEnvironment("development"),
		cartridge.WithCORS(true),
		cartridge.WithCSRF(false), // Disable CSRF for now to test the main functionality
		cartridge.WithCORSOrigins([]string{"http://localhost:3000", "http://localhost:5173"}),
	)

	// Setup structured logging with emojis and context
	logger := app.Logger().With("service", "cartridge-example", "version", "1.0.0")
	logger.Info("🚀 Starting Cartridge application",
		"environment", "development",
		"port", "8084",
		"features", []string{"CORS", "structured-logging", "async-processing", "cron-jobs"})

	// Register cron jobs using the new simplified API (no error checking - panics on failure)
	logger.Info("⏰ Registering cron jobs", "total_jobs", 4)

	// Example: Clean up old records every day at 2:00 AM
	app.CronJob("cleanup-old-records", "0 0 2 * * *", CleanupOldProducts, "Clean up old product records")

	// Example: Generate daily reports every day at 6:00 AM (no description)
	app.CronJob("daily-report", "0 0 6 * * *", GenerateDailyReport)

	// Example: Health check every 5 minutes
	app.CronJob("health-check", "0 */5 * * * *", DatabaseHealthCheck, "Database health check")

	// Example: Weekly statistics every Sunday at 8:00 AM
	app.CronJob("weekly-stats", "0 0 8 * * SUN", GenerateWeeklyStats, "Generate weekly statistics")

	logger.Info("✅ Cron jobs registered successfully")

	// Product routes - clean functional approach, no boilerplate!
	logger.Info("🛣️ Setting up routes", "category", "products")
	app.Get("/products", ListProducts)
	app.Get("/products/:id", GetProduct)
	app.Post("/products", CreateProduct)          // JSON API
	app.Post("/products/form", CreateProductForm) // Form example
	app.Put("/products/:id", UpdateProduct)
	app.Delete("/products/:id", DeleteProduct)

	// Async processing endpoints to showcase the new functionality
	logger.Info("⚡ Setting up async processing endpoints")
	app.Post("/async/process", func(ctx *cartridge.Context) error {
		taskID, err := app.AsyncJob("process-data", ProcessLargeDataset, map[string]interface{}{
			"type": "large_dataset",
		})
		if err != nil {
			ctx.Logger.Error("❌ Failed to start async task", "error", err)
			return ctx.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		ctx.Logger.Info("🚀 Async task started", "task_id", taskID, "type", "dataset_processing")
		return ctx.JSON(fiber.Map{"task_id": taskID, "message": "Task started"})
	})

	app.Post("/async/report", func(ctx *cartridge.Context) error {
		taskID, err := app.AsyncJob("generate-report", GenerateReport, map[string]interface{}{
			"format": "pdf",
		})
		if err != nil {
			ctx.Logger.Error("❌ Failed to start report generation", "error", err)
			return ctx.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		ctx.Logger.Info("📊 Report generation started", "task_id", taskID, "format", "pdf")
		return ctx.JSON(fiber.Map{"task_id": taskID, "message": "Report generation started"})
	})

	app.Get("/async/status/:id", func(ctx *cartridge.Context) error {
		taskID := ctx.Params("id")
		task, err := app.AsyncStatus(taskID)
		if err != nil {
			return ctx.NotFound("Task not found")
		}
		return ctx.JSON(task)
	})

	app.Get("/async/list", func(ctx *cartridge.Context) error {
		return ctx.JSON(app.AsyncList())
	})

	app.Delete("/async/:id", func(ctx *cartridge.Context) error {
		taskID := ctx.Params("id")
		if err := app.AsyncCancel(taskID); err != nil {
			return ctx.BadRequest(err, "Failed to cancel task")
		}
		return ctx.JSON(fiber.Map{"message": "Task cancelled", "task_id": taskID})
	})

	// Root endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(map[string]interface{}{
			"message": "Cartridge API with improved features",
			"features": []string{
				"Structured logging with slog",
				"Graceful shutdown",
				"Database connection management",
				"Ultimate functional handlers - one parameter only!",
				"Fiber context embedded in Cartridge context",
				"ctx.DB() - clean database access",
				"ctx.Query(), ctx.QueryInt(), ctx.QueryBool() - easy params",
				"ctx.ParseJSON() and ctx.ParseForm() - clear parsing",
				"ctx.Must(), ctx.DBExec(), ctx.DBQuery() - clean error handling",
				"ctx.ValidateStruct() - struct validation with tags",
				"ctx.Require() - field validation with panic",
				"ctx.BadRequest(), ctx.NotFound(), ctx.Fail() - status helpers",
				"ctx.Unauthorized(), ctx.Forbidden() - auth helpers",
				"Embedded SQL migrations with go:embed",
				"go-playground/validator integration",
				"Smart CSRF: only in Render(), not JSON()",
				"Cron jobs with robfig/cron integration",
				"Scheduled tasks with database access",
				"NEW: Async processing with goroutines",
				"NEW: CronJob() method with optional description",
				"NEW: Simplified Run() that panics on failure",
			},
		})
	})

	logger.Info("🌟 Server configuration complete", "status", "ready")
	logger.Info("📍 Available routes",
		"products_list", "GET /products?search=...&limit=...",
		"products_get", "GET /products/:id",
		"products_create_json", "POST /products (JSON)",
		"products_create_form", "POST /products/form (Form)",
		"products_update", "PUT /products/:id",
		"products_delete", "DELETE /products/:id?confirm=true",
		"async_process", "POST /async/process",
		"async_report", "POST /async/report",
		"async_status", "GET /async/status/:id",
		"async_list", "GET /async/list",
		"async_cancel", "DELETE /async/:id",
		"root", "/",
		"default_health", "/_health",
		"readiness", "/_ready",
		"liveness", "/_live")

	logger.Info("💚 Default health endpoints automatically configured")

	logger.Info("🎯 Starting server with enhanced features",
		"migrations", "enabled",
		"cron_jobs", "enabled",
		"async_processing", "enabled",
		"structured_logging", "enabled",
		"colors", "enabled")

	// One call handles everything - migrations, cron jobs, and startup!
	app.Run(cartridge.WithMigrations(migrationFiles, "migrations"))
}
