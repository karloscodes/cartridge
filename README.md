# Cartridge

An opinionated Go web framework on top of [Fiber](https://gofiber.io) and [GORM](https://gorm.io). It targets monolithic apps that ship as one binary with SQLite: config, logging, sessions, CSRF protection, background jobs, and embedded assets come wired in.

> **Note:** Cartridge is pre-1.0. APIs can change between minor versions.

```bash
go get github.com/karloscodes/cartridge
```

Requires Go 1.26+.

## Pick a constructor

| Constructor | Use it for | Database |
|---|---|---|
| `NewSSRApp` | Go `html/template` apps (with or without HTMX) | SQLite, managed for you |
| `NewInertiaApp` | React/Vue frontends through Inertia.js | SQLite, or your own `DBManager` |
| `NewApplication` | Full control: PostgreSQL, custom config, custom server | Anything that implements `DBManager` |

## Quick start (SSR)

Layout that `NewSSRApp` expects:

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

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"

	"myapp/web"
)

type Note struct {
	ID   uint
	Body string
}

func main() {
	app, err := cartridge.NewSSRApp("myapp",
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
	return ctx.Render("home", fiber.Map{"Notes": notes})
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

- Request ID, panic recovery, security headers (Helmet), and compression.
- Request logging. Development logs text to stdout. Production logs JSON to stdout and to a rotated file in `storage/logs`.
- CSRF protection on every route through the `Sec-Fetch-Site` header. No tokens needed.
- SQLite in WAL mode with `busy_timeout` and immediate transactions.
- Development reads templates and static files from `web/` on disk and reloads templates on each request. Other environments use the embedded files.

## Configuration

`NewSSRApp` reads env vars with the upper-cased app name as the prefix. It also reads a `.env` file in the working directory.

| Variable | Default | Notes |
|---|---|---|
| `MYAPP_ENV` | `production` | `development`, `production`, or `test` |
| `MYAPP_PORT` | `8080` | |
| `MYAPP_SESSION_SECRET` | none | Required in production. Falls back to `PRIVATE_KEY`. |
| `MYAPP_LOG_LEVEL` | `error` (`info` in dev/test) | `debug`, `info`, `warn`, `error` |
| `MYAPP_DATA_DIR` | `storage` | Holds the database file |
| `MYAPP_DEBUG` | `false` | |

The SQLite file is `<data dir>/<app>.<env>.db`, for example `storage/myapp.production.db`. Each environment gets its own file.

To add your own settings, load the config yourself and pass it in:

```go
cfg, err := config.Load("myapp") // github.com/karloscodes/cartridge/config
app, err := cartridge.NewSSRApp("myapp", cartridge.WithConfig(cfg))
```

## Handlers

Every handler has one signature: `func(*cartridge.Context) error`. `Context` embeds `*fiber.Ctx`, so all Fiber methods work. It adds:

| Member | What it does |
|---|---|
| `ctx.DB()` | GORM session bound to the request context |
| `ctx.Input("key")` | One value from form, JSON body, route param, or query, in that order |
| `ctx.Bind(&dst)` | Decodes the body (JSON, form, multipart), then overlays params and query |
| `ctx.FlashSuccess/FlashError/FlashInfo(msg)` | Sets a one-time flash cookie. Returns `ctx` for chaining. |
| `ctx.RedirectBack("/fallback")` | 302 to the `Referer`, or to the fallback |
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
	CustomMiddleware:   []fiber.Handler{middleware.RateLimiter(middleware.WithMax(10))},
})
```

For anything Fiber can do that `Server` does not wrap, use `s.App()` to reach the `*fiber.App`.

## Sessions

`WithSession(loginPath)` turns on signed cookie sessions (HMAC-SHA256). The cookie is `<app>_session`, and it is `Secure` in production.

```go
cartridge.WithSession("/login"),
cartridge.WithRoutes(func(s *cartridge.Server) {
	auth := &cartridge.RouteConfig{
		CustomMiddleware: []fiber.Handler{s.Session().Middleware()},
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
	if err := ctx.Session.SetSession(ctx.Ctx, user.ID); err != nil {
		return err
	}
	return ctx.Redirect("/dashboard")
}

func dashboard(ctx *cartridge.Context) error {
	userID, _ := ctx.Session.GetUserID(ctx.Ctx)
	// ...
}
```

The middleware redirects anonymous users to the login path. HTMX requests get a `401` instead. Call `ctx.Session.ClearSession(ctx.Ctx)` to log out. Use `crypto.GeneratePasswordHash` and `crypto.VerifyPassword` (bcrypt) for passwords.

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

Jobs start with `app.Run()` and stop during graceful shutdown.

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

```go
app, err := cartridge.NewInertiaApp(
	cartridge.InertiaWithConfig(cfg), // must implement cartridge.FactoryConfig; *config.Config does
	cartridge.InertiaWithStaticAssets(web.Assets()),
	cartridge.InertiaWithRoutes(mountRoutes),
	cartridge.InertiaWithSession("/login"),
	cartridge.InertiaWithJobs(time.Minute, SendEmails{}),
	cartridge.InertiaWithPageTitle("My App"),
)
```

```go
func dashboard(ctx *cartridge.Context) error {
	return ctx.Inertia("Dashboard", inertia.Props{
		"stats":  loadStats(ctx),
		"events": inertia.Defer(func() any { return loadEvents(ctx) }), // loads after first paint
	})
}
```

In development, Cartridge re-reads the Vite manifest on each request. Other options:

- `InertiaWithCrossOriginAPI()` accepts cross-site requests. Use it for tracking scripts and public APIs.
- `InertiaWithCatchAllRedirect("/")` redirects unknown paths.
- `InertiaWithWorker(w)` adds any `BackgroundWorker` (`Start() error`, `Stop()`).
- `InertiaWithDBManager(m)` replaces the default SQLite manager.

## Other packages

| Package | Contents |
|---|---|
| `config` | Env-based config loader used by `NewSSRApp` |
| `middleware` | `RateLimiter`, `SecFetchSiteMiddleware`, `Helmet`, `Recover`, `RequestLogger`, concurrency limiter |
| `cache` | Generic TTL cache (`NewCache`), GORM-backed cache, memory and database `Store`s |
| `crypto` | AES-GCM `Encrypt`/`Decrypt`, bcrypt password helpers |
| `flash` | Low-level flash cookie helpers behind `ctx.Flash*` |
| `inertia` | Inertia rendering, deferred props, Vite manifest |
| `sqlite`, `postgres`, `database` | Connection managers and drivers |
| `testsupport` | In-memory test DB and test server |

## Testing your app

`testsupport` starts a server on an in-memory SQLite database, with no mocks:

```go
func TestHome(t *testing.T) {
	ts := testsupport.NewTestServer(t, testsupport.TestServerOptions{
		Models:         []any{&Note{}},
		RouteMountFunc: func(s *cartridge.Server) { s.Get("/", home) },
	})

	resp := ts.Get("/")

	assert.Equal(t, 200, resp.StatusCode)
}
```

## Contributing

```bash
make test   # go test ./...
make lint
```

## License

MIT. See [LICENSE](LICENSE).
