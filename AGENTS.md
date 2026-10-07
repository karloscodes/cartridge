# Cartridge for AI agents

Rules for an agent that builds or changes an app on `github.com/karloscodes/cartridge`. The [README](README.md) has the full API. Read the package source with `go doc github.com/karloscodes/cartridge`.

## Start

- Use `cartridge.NewApp(cfg, opts...)` with `config.Load("appname")`. It gives SQLite, the server, the logger, and sessions.
- Use `cartridge.NewApplication` only for PostgreSQL or a custom server. Then start from `cartridge.DefaultServerConfig()`.
- Change server options through `cartridge.WithServerConfig(func(c *cartridge.ServerConfig) { ... })`.
- Do not add a router, a session library, a CSRF library, or a dotenv library. Cartridge has them.

## Handlers

- Every handler and middleware is `func(ctx *cartridge.Context) error`.
- Return an error to fail. `cartridge.NewError(404)` or `cartridge.NewError(400, "message")` sets the status. The client sees the message only for a status below 500.
- Do not write an error page or log the error in the handler. The error handler does both.
- Read a body with `ctx.Bind(&dst)`. Form fields need a `form:"name"` tag. A field without a tag is never set.
- Read one value with `ctx.Input("key")`. It also reads route params and the query.
- Use `ctx.DB()` for the database. It is a GORM session bound to the request.
- After a form post: `return ctx.FlashError("...").RedirectBack("/fallback")`.

## Security rules

- **GET, HEAD, and OPTIONS routes must not change state.** CSRF protection checks only POST, PUT, PATCH, and DELETE.
- **Do not turn off CSRF** (`EnableSecFetchSite: cartridge.Bool(false)`) on a route that reads the session cookie. Turn it off only for a public endpoint with its own credential, such as an API key or a webhook signature.
- **Protect each private route** with `s.Session().Middleware()` in `RouteConfig.CustomMiddleware`. A route without it is public.
- **Authorize the record, not only the user.** Scope each query by the user from `ctx.Session.GetUserID(ctx)`. Do not trust an ID from the request.
- **Never pass a request value to `ctx.Redirect`.** Use `ctx.RedirectLocal(ctx.Query("next"), "/")` or `ctx.RedirectBack("/")`.
- **Never pass a request value to `ctx.SendFile`** or to a file path.
- **Never build SQL with string concatenation or `fmt.Sprintf`.** Use GORM placeholders: `Where("email = ?", email)`.
- **Hash passwords** with `crypto.GeneratePasswordHash` and check them with `crypto.VerifyPassword`.
- **End old sessions** with `cartridge.WithSessionCheck`: return false when the user is gone or `issuedAt` is before the last password change.
- **Rate-limit login, signup, and password reset** with `middleware.RateLimiter(middleware.WithMax(n), middleware.WithDuration(d))`. For a browser form, add `middleware.WithLimitReached(fn)` to render the page with a message instead of JSON.
- **Set `AllowedHosts` in production** when the app builds links from `ctx.BaseURL()` or `ctx.Hostname()`, such as a password-reset email.
- **Behind a proxy, set `ProxyHeader` and `TrustedProxies`** to the proxy addresses only. Without them `ctx.IP()` is the proxy and every client shares one rate limit.
- **CORS:** `EnableCORS` allows any origin. Set `CORSConfig.AllowOrigins` for an endpoint that is not public.
- **Secrets come from the environment.** `{APP}_SESSION_SECRET` must be a random value of at least 32 bytes. Never commit `.env`. Never give `crypto.Encrypt` a short or empty key.
- **Do not log request bodies, cookies, tokens, or passwords.**
- **Templates:** `html/template` escapes values. Do not use `template.HTML`, `template.JS`, or `template.URL` on a request value.

## Development and tests

- Development and test bind `127.0.0.1` only. Set `{APP}_HOST=0.0.0.0` to open a dev server to the network or to Docker.
- Test through HTTP with `testsupport.NewTestServer`. It uses an in-memory SQLite database. Do not mock the database.
- `server.Test(req)` serves one request in memory.
- Use `sqlite.PerformWrite` for a write that can collide, and `RouteConfig.WriteConcurrency` on a write-heavy route.
