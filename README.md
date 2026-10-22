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

	// Run blocks. It returns nil after a graceful shutdown on SIGINT or
	// SIGTERM, so log.Fatal(app.Run()) would exit 1 after a clean stop.
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
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

[examples/notes](examples/notes) is a complete small app: sign-in, a list, a form with validation errors, flash messages, and HTTP tests. Run it with `make example`.

### What you get by default

- `WithDefaults("1.7")` loads the stricter defaults that cartridge added since 1.6, like `load_defaults` in Rails. A new app uses the newest version; an older app raises it when it is ready. Today it turns on `BlockExternalRedirects`: `ctx.Redirect` returns an error for another host, and `ctx.RedirectExternal` is the way out. `WithServerConfig` runs after it and can turn one default off.
- [docs/rails-security-checklist.md](docs/rails-security-checklist.md) compares cartridge with the security features and the past advisories of Rails.
- `ServerConfig.AllowedHosts` rejects requests for any other `Host`, so a forged host cannot reach `ctx.BaseURL()` and the links you build from it. Set it in production. An entry that starts with a dot, `.example.com`, also allows every subdomain. With `NewApp`, set server options through `WithServerConfig(func(c *cartridge.ServerConfig) { ... })`.
- Request ID, panic recovery, security headers, and compression. Set `ServerConfig.ContentSecurityPolicy` to send a CSP. Production sends HSTS over https.
- Request logging. Development logs text to stdout. Production logs JSON to stdout and to a rotated file in `storage/logs`.
- CSRF protection on every POST, PUT, PATCH, and DELETE route through the `Sec-Fetch-Site` header. No tokens needed. GET, HEAD, and OPTIONS are never checked, so keep them free of side effects. This is CSRF protection, not client authentication: curl can send any header.
- Error responses that show the client only the status text, or the `Message` of a `cartridge.NewError` with a code below 500. The full error goes to the log.
- SQLite in WAL mode with `busy_timeout` and immediate transactions.
- Development reads templates and static files from `web/` on disk and reloads templates on each request. Static files from disk get `Cache-Control: no-cache`, so the browser never uses a stale file. Other environments use the embedded files.

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
| `MYAPP_LOGS_DIR` | `storage/logs` | The rotated log files. Every line also goes to stdout. In a container, set it outside the data directory, like `/app/logs`, so the backups hold only data |
| `MYAPP_DEBUG` | `false` | |

The SQLite file is `<data dir>/<app>.<env>.db`, for example `storage/myapp.production.db`. Each environment gets its own file.

`NewApp` accepts any `cartridge.AppConfig`. To add your own settings, embed `*config.Config` in your own struct and pass that struct.

## Handlers

Every handler and middleware has one signature: `func(*cartridge.Context) error`. Middleware calls `ctx.Next()`. `Context` wraps the `http.ResponseWriter` and `*http.Request` (`ctx.Response()`, `ctx.Request()`), and keeps the Fiber-style methods from cartridge v0: `Params`, `Query`, `Get`, `Set`, `Cookies`, `Cookie`, `Locals`, `Status(...).JSON(...)`, `SendString`, `Redirect`, and more. It adds:

| Member | What it does |
|---|---|
| `ctx.DB()` | GORM session bound to the request context |
| `ctx.Database("name")`, `ctx.DatabaseWriteTx("name", fn)` | Read and write a database from `WithDatabase`. See [More databases](#more-databases) |
| `ctx.Input("key")` | One value from form, JSON body, route param, or query, in that order |
| `ctx.Bind(&dst)` | Decodes the body only (JSON, form, multipart). Form fields need a `form` tag. Returns a 400, 413, or 415 `*Error` |
| `ctx.QueryParser(&dst)`, `ctx.ParamsParser(&dst)` | Decode the query or route params. Fields need a `query` or `params` tag |
| `ctx.FlashSuccess/FlashError/FlashInfo(msg)` | Sets a one-time flash cookie. Returns `ctx` for chaining. |
| `ctx.RedirectLocal(target, "/fallback")` | 302 to a target from the request, only when it is on this host |
| `ctx.RedirectExternal(url)` | 302 to another site. The only redirect that leaves the site with `BlockExternalRedirects` |
| `ctx.RedirectBack("/fallback")` | 302 to the `Referer` path on this host, or to the fallback |
| `ctx.Render("page", data, "layouts/app")` | Renders a template as `text/html`. See [Templates](#templates) |
| `ctx.RenderAs("text/vnd.turbo-stream.html", "page", data)` | Renders a template with another content type. A text type gets `; charset=utf-8` |
| `ctx.GetCookie("name")` | Reads a request cookie, or `""`. The pair of `SetCookie` |
| `ctx.SetCookie("name", "value")` | Sets a cookie with `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` in production, until the browser closes. Use `ctx.Cookie(&cartridge.Cookie{...})` for other settings |
| `ctx.IsPrefetch()` | True when the browser asks for the page before the user opens it, as Turbo does on hover (`Sec-Purpose`, `Purpose`, or `X-Sec-Purpose` contains `prefetch`). Skip side effects that only a real visit must cause |
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

## Templates

`WithAssets(templates, static)` gives the app its templates and static files. A template's name is its path without `.html`, so `templates/notes/index.html` is `notes/index`:

```go
return ctx.Render("notes/index", cartridge.Map{"Notes": notes}, "layouts/app")
```

In a layout, `{{embed}}` writes the page. `{{render "notes/row" .}}` and `{{template "notes/row" .}}` write another template. Renders run in parallel. Each file is parsed once, or on each render in development.

Every template has these functions:

| Function | Example | Result |
|---|---|---|
| `timeAgo` | `{{timeAgo .CreatedAt}}` | `5 minutes ago`, `in 3 days`, `just now`. A month is 30 days and a year is 365 days. A zero or nil time gives nothing |
| `pluralize` | `{{pluralize .Count "reply"}}`, `{{pluralize .Count "person" "people"}}` | `1 reply`, `2 replies`. It takes any integer type |
| `truncate` | `{{truncate 140 .Body}}` | At most 140 characters, with `…` at the cut |
| `squish` | `{{squish .Body}}` | No white space at the ends, and one space for each run inside |
| `dict` | `{{template "chip" (dict "Label" "Open" "Count" 3)}}` | A map, to give a template more than one value |
| `asset`, `importmap` | `{{asset "app.css"}}` | Digested static URLs, in templates of `NewApp`. See [Static files](#static-files) |

Add your own functions with `WithTemplateFuncs`. A function of yours wins over a default function with the same name:

```go
cartridge.WithTemplateFuncs(template.FuncMap{
	"money": func(cents int64) string { return fmt.Sprintf("$%.2f", float64(cents)/100) },
}),
```

`ctx.RenderAs` renders a template with another content type, for example a Turbo Stream:

```go
if strings.Contains(ctx.Get("Accept"), "text/vnd.turbo-stream.html") {
	return ctx.RenderAs("text/vnd.turbo-stream.html", "notes/more", data)
}
return ctx.Render("notes/index", data, "layouts/app")
```

### Static files

The files in `web/static` are served under `/assets`. Link them with `{{asset}}`. The URL holds a hash of the file's content, so it changes when the file changes:

```html
<link rel="stylesheet" href="{{asset "app.css"}}"> <!-- /assets/app-1a2b3c4d.css -->
```

| URL | Production (embedded files) | Development (files on disk) |
|---|---|---|
| Digested, from `asset` | `Cache-Control: public, max-age=31536000, immutable` | `Cache-Control: no-cache`. The file is hashed on each render, so a changed file gets a new URL |
| Plain, like `/assets/app.css` | `Cache-Control: public, max-age=31536000` | `Cache-Control: no-cache` |

Embedded files are hashed once, at startup. There is no build step. An unknown file name is a template error, so a typo fails the render.

`{{importmap}}` writes a `<script type="importmap">` with digested URLs, and a `<link rel="modulepreload">` for each module. Each argument is a glob or `name=file`:

```html
{{importmap "application.js" "@hotwired/turbo=turbo.min.js" "controllers/*.js"}}
<script type="module">import "application"</script>
```

A file from a glob is a module named by its path without the extension: `controllers/hello_controller.js` is `controllers/hello_controller`. An index file is named by its folder: `controllers/index.js` is `controllers`. A glob that matches no file is an error. The script tag has `data-turbo-track="reload"`, so Turbo reloads the page when a deploy changes a module. With a `ContentSecurityPolicy`, allow this inline script.

`asset` does not change the `url()` and `@import` paths inside a CSS file. Those files keep their plain URL. A plain URL is sent with `Cache-Control: no-cache` and an `ETag`, so the browser checks it on each use and a deploy with a changed file reaches it. Only a Vite build has a hash in its file names: `WithInertia` (or `ServerConfig.StaticNamesHashed`) gives plain URLs a one-year cache.

Outside a template, `app.Server.Asset("app.js")` and `app.Server.Importmap(...)` return the same values. An app from `NewApplication` adds them to its own template functions.

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

The middleware redirects anonymous users to the login path. Pages behind it send `Cache-Control: private, no-store`, unless the handler sets its own. HTMX requests get a `401` with `HX-Redirect` to the login path, so htmx loads the login page. Call `ctx.Session.ClearSession(ctx)` to log out. After a login, `ctx.RedirectLocal(ctx.Query("next"), "/")` follows the target only on this host. Use `crypto.GeneratePasswordHash` and `crypto.VerifyPassword` (bcrypt) for passwords.

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

### SQL without GORM

An app can use `database/sql` in place of GORM, for all of its queries or for some. This path adds no dependency.

```go
//go:embed migrations/*.sql
var migrations embed.FS

sub, _ := fs.Sub(migrations, "migrations")
err := app.MigrateDatabase(cartridge.NewSQLMigrator(sub))
```

`NewSQLMigrator` runs the `.sql` files in the order of their names (`0001_create_notes.sql`, `0002_add_pinned.sql`), each one once and in one transaction. The table `schema_migrations` records them. Never change a file that ran: add a new one. There are no down migrations. A file can hold several statements on SQLite and PostgreSQL; MySQL takes one statement per file.

```go
type Note struct {
	ID        int64
	Body      string
	CreatedAt time.Time
}

func listNotes(ctx *cartridge.Context) error {
	notes, err := query.All[Note](ctx.Context(), ctx.SQL(),
		"SELECT id, body, created_at FROM notes WHERE user_id = ? ORDER BY id", userID)
	if err != nil {
		return err
	}
	return ctx.JSON(notes)
}

func createNote(ctx *cartridge.Context) error {
	return ctx.WriteSQL(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx.Context(), "INSERT INTO notes (user_id, body) VALUES (?, ?)", userID, body)
		return err
	})
}
```

| Member | What it does |
|---|---|
| `ctx.SQL()` | The `*sql.DB` of the main database: the same pool that `ctx.DB()` uses |
| `ctx.WriteSQL(fn)` | One write transaction with a `*sql.Tx`, through the same write queue as `WriteTx` |
| `jobCtx.SQL()`, `jobCtx.WriteSQL(fn)` | The same in a job |
| `query.All[T]`, `query.One[T]` | Read rows into structs, or one column into values. `One` returns `sql.ErrNoRows` without a row |

A column fills the field with its name: the `db` tag, or the field name in snake case (`CreatedAt` reads `created_at`). A column without a field is an error. A column that can be `NULL` needs a pointer field or a `sql.Null` type. The placeholder is the one of the database: `?` for SQLite and MySQL, `$1` for PostgreSQL.

For queries that the compiler checks, run [sqlc](https://sqlc.dev) on the same migration files: its generated code takes `ctx.SQL()` and the `*sql.Tx` of `WriteSQL`.

### Two pools: one writer, many readers

`WithReadPool()` opens each SQLite database with two connection pools: one write connection, and a pool of read-only connections.

```go
app, err := cartridge.NewApp(cfg,
	cartridge.WithReadPool(),
	// ...
)
```

With `NewApplication`, set `sqlite.Config{ReadPool: true}`. App code does not change:

- GORM's read methods (`Find`, `First`, `Take`, `Scan`, `Count`, `Pluck`, `Rows`, `Row`) run on a read connection. Reads never wait for a write.
- `Create`, `Update`, `Delete`, `Exec`, and every transaction run on the one write connection, one at a time. Writes wait for each other in Go, so two of them cannot fail with `database is locked` against each other.
- The method decides the pool, not the SQL text. A statement that writes through a read method, such as `Raw("UPDATE ... RETURNING id").Scan(&id)`, is refused outside a transaction.
- `MaxOpenConns` is the number of read connections.
- `ctx.SQL()` returns the read-only pool. A statement that writes fails on it: write through `ctx.WriteSQL`.
- `ctx.WriteTx` has no queue of its own: the one write connection is the queue. It waits for that connection at most `WriteWait` (5 seconds), and then returns `sqlite.ErrBusy`, as before.
- A write outside `WriteTx` waits for the connection as long as its request lives.

Three things to check before you turn it on:

- Inside a transaction, use the transaction handle (`tx`) for every query. A write on `ctx.DB()` inside `WriteTx` waits for the connection that the transaction holds.
- Code that takes `db.DB()` gets the write connection, and holding it stops every write. Code that needs its own connection for a read takes it from `sqlite.ReadPoolOf(db)` or `Manager.Reader()`.
- A database in memory keeps one pool.
- Change the schema at startup, before the app serves requests, and call `Manager.SchemaChanged()` after it. A read connection that was open during the change can still return the old columns for its next `SELECT *`. `app.MigrateDatabase` makes the call for you.

`ctx.DataVersion()` returns a token that changes after every commit to the database file, by this process or another one. Put it in a cache key or an `ETag` to keep a value until the data changes, with no code that invalidates it:

```go
version, err := ctx.DataVersion()
if err != nil {
	return err
}
stats, err := cache.Fetch(ctx.Context(), memoryStore, "stats:"+siteID+":"+version, time.Hour, loadStats)
```

The token is good for the life of the process, so use it with a cache in memory. It works with or without the read pool.

It is off by default in 1.x. In 2.0 it is the only way.

### Writes under load

SQLite allows one writer. `WriteTx` runs a write transaction when its turn comes. Writers wait in arrival order and hold no pool connection while they wait, so reads go on:

```go
err := ctx.WriteTx(func(tx *gorm.DB) error {
	return tx.Create(&event).Error
})
if errors.Is(err, sqlite.ErrBusy) {
	ctx.Set("Retry-After", "5")
	return ctx.Status(http.StatusServiceUnavailable).JSON(cartridge.Map{"error": "busy, retry"})
}
```

- A write that waits longer than `sqlite.Config.WriteWait` (default 5s) gets `sqlite.ErrBusy`. Answer it with 503 and `Retry-After`, so clients slow down. The server does not retry.
- The wait has a deadline. The transaction does not: once it starts, it commits or rolls back, even if the client leaves.
- Inside `fn`, use `tx` for every query, and run nothing slow (no HTTP calls, no email). Do not nest `WriteTx`.
- In a job, use `jobCtx.WriteTx`. Outside a request, use `cartridge.Write(ctx, dbManager, fn)`.
- Batch many small writes into one `WriteTx`. Use `tx.Transaction(...)` inside it for a savepoint per item, so one bad item does not roll back the batch.

`sqlite.PerformWrite` is deprecated. It waits inside SQLite while it holds a pool connection, so a write burst can block every reader.

### Pragmas

Every connection gets WAL, `synchronous=NORMAL`, `busy_timeout`, and immediate transactions. The manager runs `PRAGMA optimize` at open and close, so the query planner has statistics. Add app-specific pragmas with `Pragmas`. They run on every connection:

```go
sqlite.NewManager(sqlite.Config{
	Path:    "storage/app.db",
	Pragmas: []string{"PRAGMA mmap_size = 268435456"},
})
```

With `NewApp`, pass them with `cartridge.WithPragmas("PRAGMA foreign_keys = ON", ...)`.

Cartridge adds no other pragma: each app sets its own. Measure a pragma on real data before you add it. SQLite ignores an unknown pragma name without an error. With the read pool, a pragma runs on the write connection and on every read connection, and a read connection refuses a pragma that writes.

`foreign_keys = ON` needs a check of the app first. SQLite does not enforce foreign keys by default. With it on, a write that leaves a row without its parent fails, and a migration that rebuilds a table, as GORM's `AutoMigrate` does to change or drop a column, drops the old table with the rows that refer to it.

### More databases

`WithDatabase` opens another SQLite file next to the main one:

```go
cartridge.WithDatabase("shared", sqlite.Config{Path: filepath.Join(cfg.DataDirectory, "shared.sqlite3")}),
```

```go
func showAccount(ctx *cartridge.Context) error {
	var account Account
	if err := ctx.Database("shared").First(&account, "slug = ?", ctx.Params("slug")).Error; err != nil {
		return err
	}
	return ctx.Render("accounts/show", account)
}

func renameAccount(ctx *cartridge.Context) error {
	err := ctx.DatabaseWriteTx("shared", func(tx *gorm.DB) error {
		return tx.Model(&Account{}).Where("slug = ?", ctx.Params("slug")).Update("name", ctx.Input("name")).Error
	})
	if err != nil {
		return err
	}
	return ctx.FlashSuccess("Renamed").RedirectBack("/")
}
```

- `ctx.Database(name)` is a GORM session bound to the request, like `ctx.DB()`. An unknown name panics, and the client gets a 500.
- `ctx.DatabaseWriteTx(name, fn)` writes through the write queue of that database. See [Writes under load](#writes-under-load).
- `Logger`, `MaxOpenConns`, and `MaxIdleConns` default to those of the main database.
- The managers are in `app.Databases`. With `NewApplication`, set `ServerConfig.Databases`.
- A job reads one with `db, err := jobCtx.Database("shared")` and writes it with `jobCtx.DatabaseWriteTx("shared", fn)`.
- `Run` and `Shutdown` close the main database and the named ones.

### Read-only files

Set `ReadOnly` for a file that the app must not change, such as a copy of another app's data:

```go
cartridge.WithDatabase("tenant", sqlite.Config{Path: "data/main.sqlite3", ReadOnly: true}),
```

The manager opens the file with `mode=ro` and keeps its journal mode. It does not run `PRAGMA optimize`, because that writes statistics. `Write` and `ctx.DatabaseWriteTx` return `sqlite.ErrReadOnly`. The file must exist. For a WAL file, SQLite creates the `-shm` file, so the directory must be writable.

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

`SSLMode`, `Timezone`, and `SearchPath` go into the DSN, so every connection of the pool has them. A URL and a keyword DSN (`host=... dbname=...`) both work, and an option that the DSN sets itself wins.

### MySQL

The `mysql` package is the driver for MySQL, used like the PostgreSQL one: `database.NewManager(mysql.NewDriver(), database.DefaultConfig(dsn), logger)`. It turns `parseTime` on in the DSN, so `time.Time` fields read `DATETIME` columns.

### Database tests

The PostgreSQL and MySQL tests need a server. They skip themselves without their DSN, and CI runs each in its own job:

```bash
CARTRIDGE_POSTGRES_DSN=postgres://postgres:test@127.0.0.1:5432/cartridge_test go test ./postgres
CARTRIDGE_MYSQL_DSN='root:test@tcp(127.0.0.1:3306)/cartridge_test' go test ./mysql
```

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
| `sqlite`, `postgres`, `mysql`, `database` | Connection managers and drivers |
| `query` | Reads `database/sql` rows into structs, without GORM |
| `testsupport` | `NewTestApp` for the app's own App on a temporary database file, `NewTestServer` for handlers alone |

## Testing your app

`testsupport.NewTestApp` builds your app with the function that `main` uses, on a new SQLite file in `t.TempDir()`. The test runs your real migrations and renders your real views, with no mocks:

```go
// main.go
func newApp(cfg *config.Config) (*cartridge.App, error) {
	app, err := cartridge.NewApp(cfg, cartridge.WithAssets(web.Templates(), web.Static()), cartridge.WithRoutes(routes))
	if err != nil {
		return nil, err
	}
	return app, app.MigrateDatabase(cartridge.NewAutoMigrator(&Note{}))
}

// main_test.go
func TestNotes(t *testing.T) {
	ta := testsupport.NewTestApp(t, "myapp", newApp)

	ta.PostForm("/notes", url.Values{"body": {"Hello"}})
	resp := ta.Get("/")

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Hello")
}
```

- The config is for the test environment. `NewTestApp` reads no env var and no `.env` file. To start from a copy of a database, copy it into `cfg.DataDirectory` in your build function, before `NewApp`.
- Requests send `Sec-Fetch-Site: same-origin`, as a browser does, so they pass CSRF protection.
- `ta.Client()` keeps the cookies the app sets, like one browser. Sign in once, and the next requests have the session and the flash message. `ta.Get` and the other `ta` methods send no cookies.
- `ta.DB()` is the main database, to add or count rows.
- At cleanup, `NewTestApp` closes the databases.

[examples/notes/main_test.go](examples/notes/main_test.go) tests sign-in, validation errors, and flash messages this way.

`testsupport.NewTestServer` tests handlers alone, on an in-memory SQLite database with `AutoMigrate`. It has no views engine, so use it for handlers that do not call `ctx.Render`:

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
