package cartridge

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/karloscodes/cartridge/flash"
	"github.com/karloscodes/cartridge/inertia"
)

// Input returns a single request value by key, regardless of how it arrived:
// urlencoded/multipart form, JSON body (the Inertia protocol), route param, or
// query string — checked in that order. Returns "" if the key is absent.
//
// It's the single-field counterpart to Bind: reach for Input when you need one
// or two values, Bind when you want a typed struct. This avoids the
// declare-struct-then-pull-fields ceremony for the common case:
//
//	key := strings.TrimSpace(ctx.Input("openai_api_key"))
func (ctx *Context) Input(key string) string {
	// Form body (urlencoded/multipart). Fiber's FormValue can't read a JSON body.
	if v := ctx.FormValue(key); v != "" {
		return v
	}

	// JSON body (Inertia posts JSON).
	if ct := ctx.Get(fiber.HeaderContentType); len(ctx.Body()) > 0 && strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
		var m map[string]any
		if json.Unmarshal(ctx.Body(), &m) == nil {
			if v, ok := m[key]; ok && v != nil {
				if s, isString := v.(string); isString {
					return s
				}
				return fmt.Sprint(v) // numbers/bools rendered as their literal
			}
		}
	}

	// Route param, then query string.
	if v := ctx.Params(key); v != "" {
		return v
	}
	return ctx.Query(key)
}

// Bind decodes request input into out, regardless of how it arrived.
// It reads the body (Fiber's BodyParser is content-type-aware: JSON,
// x-www-form-urlencoded, multipart), then overlays route params and query
// values. Use this instead of FormValue so handlers don't break when the
// frontend posts JSON (e.g. the Inertia protocol) vs a form.
func (ctx *Context) Bind(out any) error {
	if len(ctx.Body()) > 0 {
		if err := ctx.BodyParser(out); err != nil {
			return err
		}
	}

	// Overlay route params and query values. These are best-effort:
	// a struct may legitimately have no fields that match, so their
	// errors are not propagated.
	_ = ctx.ParamsParser(out)
	_ = ctx.QueryParser(out)

	return nil
}

// Inertia renders an Inertia page, auto-injecting the current flash message
// under the "flash" prop if the caller didn't set one.
func (ctx *Context) Inertia(component string, props inertia.Props) error {
	if props == nil {
		props = inertia.Props{}
	}

	if _, exists := props["flash"]; !exists {
		if msg := flash.GetFlash(ctx.Ctx); msg != nil {
			props["flash"] = msg
		}
	}

	return inertia.RenderPage(ctx.Ctx, component, props)
}

// FlashError sets an "error" flash message and returns ctx for chaining.
func (ctx *Context) FlashError(message string) *Context {
	flash.SetFlash(ctx.Ctx, "error", message)
	return ctx
}

// FlashSuccess sets a "success" flash message and returns ctx for chaining.
func (ctx *Context) FlashSuccess(message string) *Context {
	flash.SetFlash(ctx.Ctx, "success", message)
	return ctx
}

// FlashInfo sets an "info" flash message and returns ctx for chaining.
func (ctx *Context) FlashInfo(message string) *Context {
	flash.SetFlash(ctx.Ctx, "info", message)
	return ctx
}

// RedirectBack issues a 302 to the Referer header, or to fallback if there's
// no referer. Combined with the Flash* helpers it enables the Post/Redirect/Get
// pattern in one line:
//
//	return ctx.FlashError("Invalid domain").RedirectBack("/admin/websites")
func (ctx *Context) RedirectBack(fallback string) error {
	loc := ctx.Get("Referer")
	if loc == "" {
		loc = fallback
	}
	return ctx.Redirect(loc, fiber.StatusFound)
}
