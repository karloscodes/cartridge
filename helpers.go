package cartridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/karloscodes/cartridge/flash"
	"github.com/karloscodes/cartridge/inertia"
)

// Input returns a single request value by key, regardless of how it arrived:
// urlencoded/multipart form, JSON body (the Inertia protocol), route param, or
// query string — checked in that order. Returns "" if the key is absent.
//
// Unlike Bind, Input also reads route params and the query string. Reach
// for Input when you need one or two values, Bind when you want a typed
// struct of body fields. This avoids the
// declare-struct-then-pull-fields ceremony for the common case:
//
//	key := strings.TrimSpace(ctx.Input("openai_api_key"))
func (ctx *Context) Input(key string) string {
	// Form body (urlencoded/multipart). FormValue can't read a JSON body.
	if v := ctx.FormValue(key); v != "" {
		return v
	}

	// JSON body (Inertia posts JSON).
	if ct := ctx.Get("Content-Type"); len(ctx.Body()) > 0 && strings.HasPrefix(ct, "application/json") {
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

// Bind decodes the request body into out: JSON (the Inertia protocol),
// x-www-form-urlencoded, or multipart. Use it instead of FormValue so
// handlers work for both JSON and form posts. An empty body is not an error.
//
// Bind reads the body only. Read route params and query values with Params,
// Query, ParamsParser, or QueryParser, so a query string cannot set a field
// the body did not set. JSON follows encoding/json rules; form fields map
// only by an explicit `form` tag, and "-" skips a field.
//
// Bind returns an *Error: 400 for a malformed body, 413 for a body over
// ServerConfig.BodyLimit, 415 for an unsupported Content-Type.
func (ctx *Context) Bind(out any) error {
	if len(ctx.Body()) == 0 && ctx.bodyErr == nil {
		return nil
	}
	return ctx.BodyParser(out)
}

// Inertia renders an Inertia page, auto-injecting the current flash message
// under the "flash" prop if the caller didn't set one.
func (ctx *Context) Inertia(component string, props inertia.Props) error {
	if props == nil {
		props = inertia.Props{}
	}

	if _, exists := props["flash"]; !exists {
		if msg := flash.GetFlash(ctx.Response(), ctx.Request()); msg != nil {
			props["flash"] = msg
		}
	}

	return inertia.RenderPage(ctx.Response(), ctx.Request(), component, props)
}

// FlashError sets an "error" flash message and returns ctx for chaining.
func (ctx *Context) FlashError(message string) *Context {
	ctx.setFlash("error", message)
	return ctx
}

// FlashSuccess sets a "success" flash message and returns ctx for chaining.
func (ctx *Context) FlashSuccess(message string) *Context {
	ctx.setFlash("success", message)
	return ctx
}

// FlashInfo sets an "info" flash message and returns ctx for chaining.
func (ctx *Context) FlashInfo(message string) *Context {
	ctx.setFlash("info", message)
	return ctx
}

// setFlash marks the flash cookie Secure in production, like the session cookie.
func (ctx *Context) setFlash(messageType, message string) {
	secure := ctx.Config != nil && ctx.Config.IsProduction()
	flash.SetFlash(ctx.Response(), messageType, message, secure)
}

// RedirectBack issues a 302 to the page in the Referer header, or to
// fallback. Combined with the Flash* helpers it enables the Post/Redirect/Get
// pattern in one line:
//
//	return ctx.FlashError("Invalid domain").RedirectBack("/admin/websites")
//
// It follows a Referer only on this host, and redirects to its path and
// query only. Any other Referer gives the fallback, so a forged Referer
// cannot send the user to another site.
func (ctx *Context) RedirectBack(fallback string) error {
	return ctx.Redirect(localReferer(ctx.Get("Referer"), ctx.Hostname(), fallback), http.StatusFound)
}

// localReferer returns the path and query of referer when it points at host,
// or else fallback.
func localReferer(referer, host, fallback string) string {
	u, err := url.Parse(referer)
	if referer == "" || err != nil || (u.Host != "" && u.Host != host) || (u.Host == "" && u.Scheme != "") {
		return fallback
	}
	// A browser reads "//evil.com" and "/\evil.com" as another host.
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.HasPrefix(u.Path, "/\\") {
		return fallback
	}
	return u.RequestURI()
}
