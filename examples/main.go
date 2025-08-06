package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"
)

// ProductController demonstrates clean controller pattern
type ProductController struct {
	*cartridge.Context // Gets DB, Logger, Config, Auth, Middleware
}

func NewProductController(ctx *cartridge.Context) cartridge.CrudController {
	return &ProductController{Context: ctx}
}

func (pc *ProductController) Index(c *fiber.Ctx) error {
	// Use structured logging with slog
	pc.Logger.Info("Fetching products", "endpoint", "index")

	// Get GORM database connection directly
	db := pc.DB.GetGenericConnection().(*gorm.DB)
	
	// Use GORM for database operations
	var products []map[string]interface{}
	result := db.Raw("SELECT id, name, price FROM products LIMIT 10").Scan(&products)
	if result.Error != nil {
		pc.Logger.Error("Failed to fetch products", "error", result.Error)
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	pc.Logger.Info("Products fetched successfully", "count", len(products))
	return c.JSON(map[string]interface{}{
		"products": products,
		"env":      pc.Context.Config.Environment,
	})
}

func (pc *ProductController) Show(c *fiber.Ctx) error {
	id := c.Params("id")
	pc.Logger.Info("Fetching product", "id", id)
	
	db := pc.DB.GetGenericConnection().(*gorm.DB)
	var product map[string]interface{}
	result := db.Raw("SELECT * FROM products WHERE id = ?", id).Scan(&product)
	if result.Error != nil {
		pc.Logger.Error("Failed to fetch product", "error", result.Error, "id", id)
		return c.Status(404).JSON(fiber.Map{"error": "Product not found"})
	}
	
	return c.JSON(product)
}

func (pc *ProductController) Create(c *fiber.Ctx) error {
	// Access middleware config if needed
	csrfEnabled := pc.Context.Config.EnableCSRF
	pc.Logger.Info("Creating product", "csrf_enabled", csrfEnabled)

	var data map[string]interface{}
	if err := c.BodyParser(&data); err != nil {
		pc.Logger.Error("Failed to parse request body", "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}
	
	db := pc.DB.GetGenericConnection().(*gorm.DB)
	result := db.Exec("INSERT INTO products (name, price) VALUES (?, ?)", data["name"], data["price"])
	if result.Error != nil {
		pc.Logger.Error("Failed to create product", "error", result.Error)
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}
	
	return c.Status(201).JSON(fiber.Map{"message": "Product created", "data": data})
}

func (pc *ProductController) Update(c *fiber.Ctx) error {
	id := c.Params("id")
	var data map[string]interface{}
	if err := c.BodyParser(&data); err != nil {
		pc.Logger.Error("Failed to parse request body", "error", err)
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}
	
	db := pc.DB.GetGenericConnection().(*gorm.DB)
	result := db.Exec("UPDATE products SET name = ?, price = ? WHERE id = ?", data["name"], data["price"], id)
	if result.Error != nil {
		pc.Logger.Error("Failed to update product", "error", result.Error, "id", id)
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}
	
	return c.JSON(fiber.Map{"message": "Product updated", "data": data})
}

func (pc *ProductController) Delete(c *fiber.Ctx) error {
	id := c.Params("id")
	pc.Logger.Info("Deleting product", "id", id)
	
	db := pc.DB.GetGenericConnection().(*gorm.DB)
	result := db.Exec("DELETE FROM products WHERE id = ?", id)
	if result.Error != nil {
		pc.Logger.Error("Failed to delete product", "error", result.Error, "id", id)
		return c.Status(500).JSON(fiber.Map{"error": "Database error"})
	}
	
	return c.JSON(fiber.Map{"message": "Product deleted", "id": id})
}

func (pc *ProductController) New(c *fiber.Ctx) error {
	return c.JSON(map[string]string{"form": "new_product"})
}

func (pc *ProductController) Edit(c *fiber.Ctx) error {
	return c.JSON(map[string]string{"form": "edit_product"})
}

// APIHandlers for individual endpoints
type APIHandlers struct {
	*cartridge.Context
}

func NewAPIHandlers(ctx *cartridge.Context) *APIHandlers {
	return &APIHandlers{Context: ctx}
}

func (h *APIHandlers) Health(c *fiber.Ctx) error {
	h.Logger.Info("Health check requested")

	// Check database health
	dbHealthy := h.DB.Ping() == nil

	// Access middleware configurations
	security := h.Middleware.Security
	
	return c.JSON(map[string]interface{}{
		"status":            "healthy",
		"database":          dbHealthy,
		"environment":       h.Context.Config.Environment,
		"security_headers":  security.ContentTypeOptions == "nosniff",
		"rate_limit_max":    h.Middleware.RateLimit.Max,
	})
}

func (h *APIHandlers) Config(c *fiber.Ctx) error {
	h.Logger.Info("Configuration requested")

	return c.JSON(map[string]interface{}{
		"app": map[string]interface{}{
			"environment": h.Context.Config.Environment,
			"port":        h.Context.Config.Port,
		},
		"middleware": map[string]interface{}{
			"csrf_enabled":     h.Context.Config.EnableCSRF,
			"cors_enabled":     h.Context.Config.EnableCORS,
			"rate_limit_max":   h.Middleware.RateLimit.Max,
			"security_headers": h.Middleware.Security.ContentTypeOptions,
		},
	})
}

func main() {
	// Create app with structured logging and configurable CORS
	app := cartridge.NewAPIOnly(
		cartridge.WithPort("8080"),
		cartridge.WithEnvironment("development"),
		cartridge.WithCORS(true),
		cartridge.WithCORSOrigins([]string{"http://localhost:3000", "http://localhost:5173"}),
	)

	// Setup structured logging
	logger := app.Logger().With("service", "cartridge-example")
	logger.Info("Starting Cartridge application")

	// Controller factory
	factory := app.NewController()

	// CRUD resource - creates 7 REST endpoints
	app.Resource("products", factory.Controller(NewProductController))

	// Individual handlers
	api := NewAPIHandlers(app.Ctx())
	app.Get("/health", api.Health)
	app.Get("/config", api.Config)

	// Root endpoint
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(map[string]interface{}{
			"message": "Cartridge API with improved features",
			"features": []string{
				"Structured logging with slog",
				"Graceful shutdown",
				"Database connection management",
				"Middleware configuration access",
				"Clean controller pattern",
			},
		})
	})

	logger.Info("Server configuration complete")
	logger.Info("Available routes",
		"crud", "/products (7 routes)",
		"custom_health", "/health",
		"config", "/config", 
		"root", "/",
		"default_health", "/_health",
		"readiness", "/_ready",
		"liveness", "/_live")

	logger.Info("Default health endpoints automatically configured")

	// Start server with graceful shutdown
	// Press Ctrl+C to test graceful shutdown
	if err := app.Start(); err != nil {
		logger.Error("Server failed", "error", err)
	}
}