# Cartridge

An opinionated Go web framework on top of `net/http` and [GORM](https://gorm.io). It targets monolithic apps that ship as one binary with SQLite: config, logging, sessions, CSRF protection, background jobs, and embedded assets come wired in.

> **Note:** Cartridge is pre-1.0. APIs can change between minor versions.

```bash
go get github.com/karloscodes/cartridge
```

Requires Go 1.26+.

## Pick a constructor

| Constructor | Use it for | Database |
|---|---|---|
| `NewApp` | Go `html/template` apps (with or without HTMX), and React/Vue through Inertia.js with `WithInertia()` | SQLite, managed for you |
| `NewApplication` | Full control: PostgreSQL, custom server | Anything that implements `DBManager` |

## Quick start

Layout that `NewApp` expects:

```
myapp/
├── main.go
└── web/
    ├── embed.go
    ├── templates/home.html
    └── static/app.css      # served at /assets/app.css
```

`web/embed.go`:

```go
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Template names are relative to templates/, so "home" resolves to templates/home.html.
func Templates() fs.FS { sub, _ := fs.Sub(templatesFS, "templates"); return sub }
func Static() fs.FS    { sub, _ := fs.Sub(staticFS, "static"); return sub }
```

`main.go`:

```go
package main

import (
	"log"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/config"

	"myapp/web"
)

type Note struct {
	ID   uint
	Body string
}

func main() {
	cfg, err := config.Load("myapp")
	if err != nil {
		log.Fatal(err)
	}

	app, err := cartridge.NewApp(cfg,
		cartridge.WithAssets(web.Templates(), web.Static()),
		cartridge.WithRoutes(func(s *cartridge.Server) {
			s.Get("/", home)
			s.Post("/notes", createNote, &cartridge.RouteConfig{WriteConcurrency: true})
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := app.MigrateDatabase(cartridge.NewAutoMigrator(&Note{})); err != nil {
		log.Fatal(err)
	}

	log.Fatal(app.Run()) // blocks; shuts down gracefully on SIGINT/SIGTERM
}

func home(ctx *cartridge.Context) error {
	var notes []Note
	if err := ctx.DB().Find(&notes).Error; err != nil {
		return err
	}
	return ctx.Render("home", cartridge.Map{"Notes": notes})
}

func createNote(ctx *cartridge.Context) error {
	note := Note{Body: ctx.Input("body")}
	if err := ctx.DB().Create(&note).Error; err != nil {
		return err
	}
	return ctx.FlashSuccess("Saved").RedirectBack("/")
}
```

Run it:

```bash
MYAPP_ENV=development go run .
```

`MYAPP_ENV` defaults to `production`. Production refuses to start without a session secret, so set the env var for local work.

### What you get by default

- `ServerConfig.AllowedHosts` rejects requests for any other `Host`, so a forged host cannot reach `ctx.BaseURL()` and the links you build from it. Set it in production. With `NewApp`, set server options through `WithServerConfig(func(c *cartridge.ServerConfig) { ... })`.
- Request ID, panic recovery, security headers, and compression. Set `ServerConfig.ContentSecurityPolicy` to send a CSP. Production sends HSTS over https.
- Request logging. Development logs text to stdout. Production logs JSON to stdout and to a rotated file in `storage/logs`.
- CSRF protection on every POST, PUT, PATCH, and DELETE route through the `Sec-Fetch-Site` header. No tokens needed. GET, HEAD, and OPTIONS are never checked, so keep them free of side effects. This is CSRF protection, not client authentication: curl can send any header.
- Error responses that show the client only the status text, or the `Message` of a `cartridge.NewError` with a code below 500. The full error goes to the log.
- SQLite in WAL mode with `busy_timeout` and immediate transactions.
- Development reads templates and static files from `web/` on disk and reloads templates on each request. Other environments use the embedded files.

## Configuration

`config.Load("myapp")` reads env vars with the upper-cased app name as the prefix. It also reads a `.env` file in the working directory. In `.env`, use the field keys without the prefix, in any case: `ENVIRONMENT=development`, `PORT=3000`, `SESSIONTIMEOUTSECONDS=60`. An env var wins over `.env`.

| Variable | Default | Notes |
|---|---|---|
| `MYAPP_ENV` | `production` | `development`, `production`, or `test` |
| `MYAPP_HOST` | every interface (`127.0.0.1` in dev/test) | Set `0.0.0.0` to reach a dev server from the network or from Docker |
| `MYAPP_PORT` | `8080` | |
| `MYAPP_SESSION_SECRET` | none | Required in production, at least 32 bytes. Falls back to `PRIVATE_KEY`. |
| `MYAPP_LOG_LEVEL` | `error` (`info` in dev/test) | `debug`, `info`, `warn`, `error` |
| `MYAPP_DATA_DIR` | `storage` | Holds the database file |
| `MYAPP_DEBUG` | `false` | |

The SQLite file is `<data dir>/<app>.<env>.db`, for example `storage/myapp.production.db`. Each environment gets its own file.

`NewApp` accepts any `cartridge.AppConfig`. To add your own settings, embed `*config.Config` in your own struct and pass that struct.

## Handlers

Every handler and middleware has one signature: `func(*cartridge.Context) error`. Middleware calls `ctx.Next()`. `Context` wraps the `http.ResponseWriter` and `*http.Request` (`ctx.Response()`, `ctx.Request()`), and keeps the Fiber-style methods from cartridge v0: `Params`, `Query`, `Get`, `Set`, `Cookies`, `Cookie`, `Locals`, `Status(...).JSON(...)`, `SendString`, `Redirect`, and more. It adds:

| Member | What it does |
|---|---|
| `ctx.DB()` | GORM session bound to the request context |
| `ctx.Input("key")` | One value from form, JSON body, route param, or query, in that order |
| `ctx.Bind(&dst)` | Decodes the body only (JSON, form, multipart). Form fields need a `form` tag. Returns a 400, 413, or 415 `*Error` |
| `ctx.QueryParser(&dst)`, `ctx.ParamsParser(&dst)` | Decode the query or route params. Fields need a `query` or `params` tag |
| `ctx.FlashSuccess/FlashError/FlashInfo(msg)` | Sets a one-time flash cookie. Returns `ctx` for chaining. |
| `ctx.RedirectBack("/fallback")` | 302 to the `Referer` path on this host, or to the fallback |
| `ctx.Inertia("Page", props)` | Renders an Inertia page and injects the flash message |
| `ctx.Logger`, `ctx.Config`, `ctx.Session` | App dependencies |

Post/Redirect/Get in one line:

```go
return ctx.FlashError("Invalid domain").RedirectBack("/websites")
```

## Routes

`s.Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, and `Options` take an optional `*RouteConfig`:

```go
s.Post("/api/events", ingest, &cartridge.RouteConfig{
	WriteConcurrency:   true,                  // queue writes (max 8 at once) to protect SQLite
	EnableCORS:         true,                  // allows any origin unless CORSConfig is set
	EnableSecFetchSite: cartridge.Bool(false), // turn off CSRF check for a public endpoint
	CustomMiddleware:   []cartridge.HandlerFunc{middleware.RateLimiter(middleware.WithMax(10))},
})
```

`Server` is an `http.Handler`. `s.Use(mw)` adds middleware to every route. A CORS route without its own OPTIONS route gets one that answers browser preflight requests. Route paths use `:param` and a trailing `*`, as in v0.

### Behind a proxy

`ctx.IP()` and `ctx.Protocol()` ignore proxy headers unless the direct peer is a trusted proxy. Name your proxies:

```go
cfg.ProxyHeader = "X-Forwarded-For"
cfg.TrustedProxies = []string{"10.0.0.0/8", "127.0.0.1"}
```

`ctx.IP()` reads the header from right to left and returns the first address that is not a trusted proxy. It skips entries it cannot read and accepts entries with a port or quotes. When no entry qualifies, it returns the peer. Without `TrustedProxies`, every client behind the proxy shares the proxy's IP, and `ctx.BaseURL()` says `http`.

### Streams and WriteTimeout

`ServerConfig.WriteTimeout` (30s by default) cuts off long responses. A stream (server-sent events, a large download) lifts it per request:

```go
http.NewResponseController(ctx.Response()).SetWriteDeadline(time.Time{})
```

## Sessions

`WithSession(loginPath)` turns on signed cookie sessions (HMAC-SHA256). The cookie is `<app>_session`, and it is `Secure` in production. The secret must be at least 32 bytes.

Built by hand, `cartridge.NewSessionManager(cartridge.SessionConfig{...})` returns an error for a short secret. Its cookie is `Secure` unless you set `Insecure: true` for local http development.

```go
cartridge.WithSession("/login"),
cartridge.WithRoutes(func(s *cartridge.Server) {
	auth := &cartridge.RouteConfig{
		CustomMiddleware: []cartridge.HandlerFunc{s.Session().Middleware()},
	}
	s.Get("/login", showLogin)
	s.Post("/login", login)
	s.Get("/dashboard", dashboard, auth)
}),
```

```go
func login(ctx *cartridge.Context) error {
	user, ok := authenticate(ctx.Input("email"), ctx.Input("password"))
	if !ok {
		return ctx.FlashError("Wrong email or password").RedirectBack("/login")
	}
	if err := ctx.Session.SetSession(ctx, user.ID); err != nil {
		return err
	}
	return ctx.Redirect("/dashboard")
}

func dashboard(ctx *cartridge.Context) error {
	userID, _ := ctx.Session.GetUserID(ctx)
	// ...
}
```

A signed cookie stays good until it expires, also after a logout or a password change. `WithSessionCheck` ends such sessions: return `false` and the user is signed out.

```go
cartridge.WithSessionCheck(func(userID uint, issuedAt time.Time) bool {
	return users.SessionStillValid(userID, issuedAt)
}),
```

The middleware redirects anonymous users to the login path. Pages behind it send `Cache-Control: private, no-store`, unless the handler sets its own. HTMX requests get a `401` instead. Call `ctx.Session.ClearSession(ctx)` to log out. After a login, `ctx.RedirectLocal(ctx.Query("next"), "/")` follows the target only on this host. Use `crypto.GeneratePasswordHash` and `crypto.VerifyPassword` (bcrypt) for passwords.

## Background jobs

A processor runs on a fixed interval. `JobContext` gives it a logger and a `*gorm.DB`:

```go
type SendEmails struct{}

func (SendEmails) ProcessBatch(ctx *cartridge.JobContext) error {
	var pending []Email
	if err := ctx.DB.Where("sent_at IS NULL").Limit(50).Find(&pending).Error; err != nil {
		return err
	}
	// send, then mark sent...
	return nil
}

cartridge.WithJobs(time.Minute, SendEmails{}),
cartridge.WithJobs(time.Hour, PruneSessions{}), // each call gets its own schedule
```

`WithWorker(w)` runs any `BackgroundWorker` (`Start() error`, `Stop()`). Jobs and workers start with `app.Run()` and stop during graceful shutdown.

## Database

### Migrations

`NewAutoMigrator` wraps GORM's `AutoMigrate`. For anything else, implement `Migrator`:

```go
type Migrator interface {
	Migrate(db *gorm.DB) error
}
```

`app.MigrateDatabase` runs the migrator, then checkpoints the SQLite WAL.

### Writes under load

Use `sqlite.PerformWrite` for writes that can collide. It retries `SQLITE_BUSY` with backoff:

```go
err := sqlite.PerformWrite(ctx.Logger, ctx.DB(), func(tx *gorm.DB) error {
	return tx.Create(&event).Error
})
```

### PostgreSQL

Use `NewApplication` with the generic manager and the Postgres driver:

```go
dbManager := database.NewManager(postgres.NewDriver(), &database.Config{
	DSN:          "host=localhost user=app dbname=myapp",
	MaxOpenConns: 25,
	MaxIdleConns: 5,
	Postgres:     database.PostgresOptions{SSLMode: "disable", Timezone: "UTC"},
}, logger)

app, err := cartridge.NewApplication(cartridge.ApplicationOptions{
	Config:         cfg,       // implements cartridge.Config
	Logger:         logger,
	DBManager:      dbManager, // implements cartridge.DBManager
	RouteMountFunc: mountRoutes,
})
```

To support another database, implement `database.Driver`.

## Inertia.js

Add `WithInertia()`. In development, Cartridge then re-reads the Vite manifest on each request:

```go
app, err := cartridge.NewApp(cfg,
	cartridge.WithInertia(),
	cartridge.WithAssets(nil, web.Assets()), // no Go templates
	cartridge.WithRoutes(mountRoutes),
	cartridge.WithSession("/login"),
)
inertia.SetTitle("My App") // optional; empty by default
```

```go
func dashboard(ctx *cartridge.Context) error {
	return ctx.Inertia("Dashboard", inertia.Props{
		"stats":  loadStats(ctx),
		"events": inertia.Defer(func() any { return loadEvents(ctx) }), // loads after first paint
	})
}
```

To redirect unknown paths, call `s.SetCatchAllRedirect("/")` in your routes function.

## Other packages

| Package | Contents |
|---|---|
| `cartridge` | `SecFetchSiteMiddleware`, `SecurityHeaders`, `Recover`, `RequestID`, `RequestLogger`, `Compress`, `CORS`, write concurrency limiter |
| `config` | Env-based config loader (`config.Load`) for `NewApp` |
| `middleware` | `RateLimiter`. A client over the limit gets a 429 with a JSON body, or the page from `WithLimitReached` |
| `cache` | Generic TTL cache (`NewCache`), GORM-backed cache, memory and database `Store`s |
| `crypto` | AES-GCM `Encrypt`/`Decrypt`, bcrypt password helpers |
| `flash` | Low-level flash cookie helpers behind `ctx.Flash*` |
| `inertia` | Inertia rendering, deferred props, Vite manifest |
| `sqlite`, `postgres`, `database` | Connection managers and drivers |
| `testsupport` | In-memory test DB and test server |

## Testing your app

`testsupport` starts a server on an in-memory SQLite database, with no mocks. Its requests send `Sec-Fetch-Site: same-origin`, as a browser does, so they pass CSRF protection. It has no views engine, so test handlers that do not call `ctx.Render`:

```go
func TestCreateNote(t *testing.T) {
	ts := testsupport.NewTestServer(t, testsupport.TestServerOptions{
		Models:         []any{&Note{}},
		RouteMountFunc: func(s *cartridge.Server) { s.Post("/notes", createNote) },
	})

	resp := ts.PostForm("/notes", url.Values{"body": {"Hello"}})

	assert.Equal(t, http.StatusFound, resp.StatusCode)
}
```

## Upgrading from 1.2

`NewSSRApp` and `NewInertiaApp` are now one constructor, `NewApp`, with one set of options. `NewApp` takes the config as its first argument and does not load it for you.

| 1.2 | 1.3 |
|---|---|
| `NewSSRApp("myapp", WithConfig(cfg), ...)` | `NewApp(cfg, ...)` |
| `NewSSRApp("myapp", ...)` without `WithConfig` | `cfg, err := config.Load("myapp")`, then `NewApp(cfg, ...)` |
| `NewInertiaApp(InertiaWithConfig(cfg), ...)` | `NewApp(cfg, WithInertia(), ...)` |
| `InertiaWithStaticAssets(fs)` | `WithAssets(nil, fs)` |
| `InertiaWithRoutes`, `InertiaWithJobs`, `InertiaWithSession` | `WithRoutes`, `WithJobs`, `WithSession` |
| `InertiaWithWorker(w)` | `WithWorker(w)` |
| `InertiaWithPageTitle("X")` | `inertia.SetTitle("X")` |
| `InertiaWithCatchAllRedirect("/")` | `s.SetCatchAllRedirect("/")` in your routes function |
| `InertiaWithCrossOriginAPI()` | `&cartridge.RouteConfig{EnableSecFetchSite: cartridge.Bool(false)}` on the public routes only |
| `InertiaWithDBManager(m)` | `NewApplication` with `DBManager: m` |
| `WithInit(fn)` | Run `fn(app)` after `NewApp` returns |
| `FactoryConfig` | `AppConfig` |
| `*InertiaApp` | `*App` |
| `app.Config` (`*config.Config`) | Keep your own `cfg`. `app.Config` is now the `cartridge.Config` interface |
| `app.GetDB()` | `app.DBManager.Connect()` |

Other changes:

- Without templates in `WithAssets`, `NewApp` sets no views engine. Before, it read `web/templates` from disk.
- `Context.IP()` skips proxy header entries it cannot read, and reads entries with a port or quotes. When no entry qualifies, it returns the peer.
- `Run` returns the error when the server fails after it binds the port, and stops the workers.
- `config.Load` no longer uses viper. The env vars, the `.env` format, and the defaults stay the same. Cartridge now has 31 modules in `go list -m all` instead of 42.

## AI agents

[AGENTS.md](AGENTS.md) holds the rules for an AI agent that builds an app on cartridge. Point your app's `CLAUDE.md` or `AGENTS.md` at it.

## Contributing

```bash
make test   # go test ./...
make lint
```

## License

MIT. See [LICENSE](LICENSE).
