package cartridge

import (
	"strconv"
)

// AdminIndexTemplate renders an admin index page with standard data structure
func (ctx *Context) AdminIndexTemplate(template string, pageType string, data interface{}, dataKey string) error {
	templateData := map[string]interface{}{
		"ShowNav":  true,
		"PageType": pageType,
		dataKey:    data,
	}
	return ctx.RenderTemplate(template, templateData)
}

// AdminShowTemplate renders an admin show page with standard data structure
func (ctx *Context) AdminShowTemplate(template string, pageType string, data interface{}, dataKey string) error {
	templateData := map[string]interface{}{
		"ShowNav":  true,
		"PageType": pageType,
		dataKey:    data,
	}
	return ctx.RenderTemplate(template, templateData)
}

// AdminFormTemplate renders an admin form page with standard data structure
func (ctx *Context) AdminFormTemplate(template string, pageType string, data interface{}, dataKey string) error {
	templateData := map[string]interface{}{
		"ShowNav":   true,
		"PageType":  pageType,
		dataKey:     data,
		"CSRFToken": "",
	}
	return ctx.RenderTemplate(template, templateData)
}

// FindOrFail finds a model by ID or returns 404 error
func (ctx *Context) FindOrFail(model interface{}, id string) error {
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		return ctx.Status(404).SendString("Invalid ID")
	}

	if err := ctx.DB().First(model, parsedID).Error; err != nil {
		return ctx.Status(404).SendString("Record not found")
	}

	return nil
}

// CreateModelSecurely handles secure model creation with validation and custom logic
func (ctx *Context) CreateModelSecurely(model interface{}, errorTemplate string, successRedirect string, customLogic func() error) error {
	// Use protected form binding
	if err := ctx.FillModel(model); err != nil {
		ctx.Logger.Error("Failed to bind form data", "error", err)
		return ctx.Status(400).SendString("Invalid form data")
	}

	// Validate the bound data
	if validationErrors := ctx.ValidateForm(model); validationErrors.HasErrors() {
		ctx.Logger.Warn("Form validation failed", "errors", validationErrors.Error())
		return ctx.Status(400).SendString("Validation failed")
	}

	// Execute custom business logic if provided
	if customLogic != nil {
		if err := customLogic(); err != nil {
			ctx.Logger.Error("Custom logic failed", "error", err)
			return ctx.Status(500).SendString("Creation failed")
		}
	}

	// Create the model
	if err := ctx.DB().Create(model).Error; err != nil {
		ctx.Logger.Error("Failed to create model", "error", err)
		return ctx.Status(500).SendString("Database error")
	}

	return ctx.Redirect(successRedirect)
}

// UpdateModelSecurely handles secure model updates with validation and custom logic
func (ctx *Context) UpdateModelSecurely(model interface{}, errorTemplate string, successRedirect string, customLogic func() error) error {
	// Use protected form binding
	if err := ctx.FillModel(model); err != nil {
		ctx.Logger.Error("Failed to bind form data", "error", err)
		return ctx.Status(400).SendString("Invalid form data")
	}

	// Validate the bound data
	if validationErrors := ctx.ValidateForm(model); validationErrors.HasErrors() {
		ctx.Logger.Warn("Form validation failed", "errors", validationErrors.Error())
		return ctx.Status(400).SendString("Validation failed")
	}

	// Execute custom business logic if provided
	if customLogic != nil {
		if err := customLogic(); err != nil {
			ctx.Logger.Error("Custom logic failed", "error", err)
			return ctx.Status(500).SendString("Update failed")
		}
	}

	// Save the model
	if err := ctx.DB().Save(model).Error; err != nil {
		ctx.Logger.Error("Failed to update model", "error", err)
		return ctx.Status(500).SendString("Database error")
	}

	return ctx.Redirect(successRedirect)
}

// HandleMethodOverride checks for HTTP method override and validates it
func (ctx *Context) HandleMethodOverride(expectedMethod string) error {
	if ctx.Method() != expectedMethod && !(ctx.Method() == "POST" && ctx.FormValue("_method") == expectedMethod) {
		return ctx.Status(405).SendString("Method not allowed")
	}
	return nil
}