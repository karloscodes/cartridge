# Cartridge for AI agents

Rules for an agent that builds or changes an app on `github.com/karloscodes/cartridge`. The [README](README.md) has the full API. Read the package source with `go doc github.com/karloscodes/cartridge`.

## Start

- Use `cartridge.NewApp(cfg, opts...)` with `config.Load("appname")`. It gives SQLite, the server, the logger, and sessions.
- Use `cartridge.NewApplication` only for PostgreSQL or a custom server. Then start from `cartridge.DefaultServerConfig()`.
- Change server options through `cartridge.WithServerConfig(func(c *cartridge.ServerConfig) { ... })`.
- Do not add a router, a session library, a CSRF library, or a dotenv library. Cartridge has them.
- A new app loads the newest defaults: `cartridge.WithDefaults("1.7")`. They turn on the stricter behaviour that cartridge added since 1.6. Do not remove the option, and do not lower its version.
- End `main` with `if err := app.Run(); err != nil { log.Fatal(err) }`. `Run` returns nil after a clean shutdown, so `log.Fatal(app.Run())` exits 1.
- Put the app's wiring in one `newApp(cfg *config.Config) (*cartridge.App, error)` that also runs the migrations. `main` and the tests call it.
- [examples/notes](examples/notes) is a complete app that follows these rules: sign-in, a list, a form with validation errors, flash messages, and HTTP tests.

## Handlers

- Every handler and middleware is `func(ctx *cartridge.Context) error`.
- Return an error to fail. `cartridge.NewError(404)` or `cartridge.NewError(400, "message")` sets the status. The client sees the message only for a status below 500.
- Do not write an error page or log the error in the handler. The error handler does both.
- Read a body with `ctx.Bind(&dst)`. Form fields need a `form:"name"` tag. A field without a tag is never set.
- Read one value with `ctx.Input("key")`. It also reads route params and the query.
- Use `ctx.DB()` for the database. It is a GORM session bound to the request.
- After a form post: `return ctx.FlashError("...").RedirectBack("/fallback")`.
- For a form with errors, render the form again with status 422: `ctx.Status(http.StatusUnprocessableEntity).Render("notes/new", data, "layouts/app")`.
- Render a response that is not HTML, such as a Turbo Stream, with `ctx.RenderAs("text/vnd.turbo-stream.html", "notes/more", data)`. Do not build a second views engine.
- Set a cookie with `ctx.SetCookie(name, value)` and read it with `ctx.GetCookie(name)`. `SetCookie` is HttpOnly, SameSite=Lax, and Secure in production. Use `ctx.Cookie(&cartridge.Cookie{...})` only for a cookie that needs other settings, such as an expiry.
- `ctx.IsPrefetch()` is true when the browser asks for the page before the user opens it. Do not let a prefetch remember anything, such as the last folder.

## Templates and static files

- Templates of `NewApp` have `timeAgo`, `pluralize`, `truncate`, `squish`, and `dict`. Do not write these helpers again.
- Add app functions with `cartridge.WithTemplateFuncs(template.FuncMap{...})`. An app function wins over a default with the same name.
- Give a partial more than one value with `dict`: `{{template "notes/row" (dict "Note" . "Compact" true)}}`.
- Link every file in `web/static` with `{{asset "app.css"}}`, not with a fixed `/assets/app.css` path. The digested URL gets an immutable cache, and a deploy with a changed file gets a new URL.
- Write the import map with `{{importmap "application.js" "controllers/*.js" "name=file.js"}}`. Do not build it in Go.
- `asset` does not rewrite `url()` or `@import` inside CSS. Link a CSS file that changes often from the HTML with `asset`. A file at its plain URL is checked by the browser on each use.

## Security rules

- **GET, HEAD, and OPTIONS routes must not change state.** CSRF protection checks only POST, PUT, PATCH, and DELETE.
- **Do not turn off CSRF** (`EnableSecFetchSite: cartridge.Bool(false)`) on a route that reads the session cookie. Turn it off only for a public endpoint with its own credential, such as an API key or a webhook signature.
- **Protect each private route** with `s.Session().Middleware()` in `RouteConfig.CustomMiddleware`. A route without it is public.
- **Authorize the record, not only the user.** Scope each query by the user from `ctx.Session.GetUserID(ctx)`. Do not trust an ID from the request.
- **Never pass a request value to `ctx.Redirect`.** Use `ctx.RedirectLocal(ctx.Query("next"), "/")` or `ctx.RedirectBack("/")`. With `WithDefaults("1.7")`, `ctx.Redirect` returns an error for another host. Use `ctx.RedirectExternal(url)` only for a fixed URL that must leave the site, such as a payment page.
- **Never pass a request value to `ctx.Render` as the template name.** Choose the template in code.
- **Bind into a request struct, never into a GORM model.** `ctx.Bind` sets every exported field that a JSON body names, so a model would let the client set `IsAdmin` or `UserID`. Copy the allowed fields to the model by hand.
- **Never pass a request value to GORM as a condition or a name.** `db.First(&user, ctx.Params("id"))` is SQL injection when the value is not a number. Convert an ID with `ctx.ParamsInt("id")`, or write `db.First(&user, "id = ?", id)`. The same holds for `Order`, `Select`, `Group`, `Table`, `Joins`, and `Pluck`: choose their text in code, for example from a fixed map of sort names.
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
- Test the real app with `testsupport.NewTestApp(t, "myapp", newApp)`. It runs the app's own migrations and views on a new SQLite file per test. Do not mock the database.
- Use `ta.Client()` for a flow over several requests, such as sign-in. It keeps the cookies like a browser. The `ta.Get` and `ta.PostForm` methods send no cookies.
- To start from a copy of a database, copy it into `cfg.DataDirectory` in the build function, before `NewApp`.
- `testsupport.NewTestServer` tests handlers alone, on an in-memory database without views.
- `server.Test(req)` serves one request in memory.
- Write in one transaction with `ctx.WriteTx(func(tx *gorm.DB) error { ... })` (in a job: `jobCtx.WriteTx`). Writes wait for their turn, one at a time. Use `tx` for every query inside, run nothing slow inside, and do not nest `WriteTx`.
- `WriteTx` returns `sqlite.ErrBusy` when a write waits too long. Answer it with 503 and a `Retry-After` header, so the client slows down. Do not retry on the server.
- Add app-specific pragmas with `sqlite.Config.Pragmas`. They run on every connection.

## More databases

- Do not close the databases in `main`. `Run` and `Shutdown` close them.
- Open a second SQLite file with `cartridge.WithDatabase("name", sqlite.Config{Path: ...})`. Read it with `ctx.Database("name")` and write it with `ctx.DatabaseWriteTx("name", fn)`. In a job: `jobCtx.Database("name")` (it returns an error, not a panic) and `jobCtx.DatabaseWriteTx`. Do not open a `sqlite.Manager` by hand, and do not override `DatabaseDSN` to point at another file.
- Open a file that the app must not change with `sqlite.Config{Path: ..., ReadOnly: true}`. Writes then return `sqlite.ErrReadOnly`. Do not put `mode=ro` in the path.
