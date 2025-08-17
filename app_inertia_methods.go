package cartridge

// Inertia methods for App struct

// Inertia returns the Inertia manager (only available for FullStackInertia apps)
func (app *App) Inertia() *InertiaManager {
	return app.inertia
}

// InertiaRender is a convenience method for rendering Inertia pages
func (app *App) InertiaRender(component string, props map[string]interface{}) func(*Context) error {
	return func(ctx *Context) error {
		if app.inertia == nil {
			return ctx.Status(500).JSON(map[string]string{
				"error": "Inertia.js is not configured for this app type",
			})
		}
		return app.inertia.Render(ctx.Fiber, component, props)
	}
}

// InertiaHelper returns a helper for working with Inertia in handlers
func (app *App) InertiaHelper(ctx *Context) *InertiaContext {
	if app.inertia == nil {
		return nil
	}
	return NewInertiaContext(ctx, app.inertia)
}

// IsInertiaApp returns true if this is a FullStackInertia app
func (app *App) IsInertiaApp() bool {
	return app.appType == AppTypeFullStackInertia
}