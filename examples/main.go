package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"
)

// Functional handlers with clean error handling pattern

func ListProducts(ctx *cartridge.Context) error {
	// Use structured logging with slog
	ctx.Logger.Info("Fetching products", "endpoint", "list")

	// Query string parameters made easy!
	limit := ctx.QueryInt("limit", 10)    // Default to 10
	search := ctx.Query("search", "")     // Default to empty string
	
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
		ctx.Logger.Error("Failed to fetch products", "error", result.Error)
		return ctx.Status(500).JSON(fiber.Map{"error": "Database error"})
	}

	ctx.Logger.Info("Products fetched successfully", "count", len(products), "search", search, "limit", limit)
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
	if err := ctx.ParseJSON(&data); err != nil {
		return ctx.BadRequest(err, "Invalid JSON body")
	}
	
	// Validate the data
	if data["name"] == nil || data["price"] == nil {
		return ctx.BadRequest(nil, "Name and price are required")
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
		Name        string  `form:"name"`
		Price       float64 `form:"price"`
		Description string  `form:"description"`
	}
	
	var form ProductForm
	ctx.Must(ctx.ParseForm(&form))
	
	// Get additional form values
	category := ctx.FormValue("category", "general")  // With default
	featured := ctx.QueryBool("featured", false)     // From query string
	
	// Insert into database with DBExec() - ultra clean!
	ctx.DBExec("INSERT INTO products (name, price, description, category, featured) VALUES (?, ?, ?, ?, ?)", 
		form.Name, form.Price, form.Description, category, featured)
	
	// For forms, use Render to include CSRF token for the next form
	return ctx.Render(fiber.Map{
		"message": "Product created successfully", 
		"product": form,
		"category": category,
		"featured": featured,
	})
}

func main() {
	// Create app with structured logging, CORS, and CSRF protection
	app := cartridge.NewFullStack(
		cartridge.WithPort("8080"),
		cartridge.WithEnvironment("development"),
		cartridge.WithCORS(true),
		cartridge.WithCSRF(true),
		cartridge.WithCORSOrigins([]string{"http://localhost:3000", "http://localhost:5173"}),
	)

	// Setup structured logging
	logger := app.Logger().With("service", "cartridge-example")
	logger.Info("Starting Cartridge application")

	// Product routes - clean functional approach, no boilerplate!
	app.Get("/products", ListProducts)
	app.Get("/products/:id", GetProduct)  
	app.Post("/products", CreateProduct)          // JSON API
	app.Post("/products/form", CreateProductForm) // Form example
	app.Put("/products/:id", UpdateProduct)
	app.Delete("/products/:id", DeleteProduct)

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
				"ctx.BadRequest(), ctx.NotFound(), ctx.Fail() - status helpers",
				"ctx.Unauthorized(), ctx.Forbidden() - auth helpers",
				"Smart CSRF: only in Render(), not JSON()",
			},
		})
	})

	logger.Info("Server configuration complete")
	logger.Info("Available routes",
		"products_list", "GET /products?search=...&limit=...",
		"products_get", "GET /products/:id",
		"products_create_json", "POST /products (JSON)",
		"products_create_form", "POST /products/form (Form)",
		"products_update", "PUT /products/:id",
		"products_delete", "DELETE /products/:id?confirm=true",
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