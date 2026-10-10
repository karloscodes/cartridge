// Command notes is a small, complete cartridge app: sign-in, a list of
// notes, and a form that shows its validation errors. Run it from this
// folder:
//
//	NOTES_ENV=development go run .
//
// Then open http://127.0.0.1:8080 and sign in as demo@example.com with the
// password "password". Development creates that user.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/config"
	"github.com/karloscodes/cartridge/middleware"

	"github.com/karloscodes/cartridge/examples/notes/db"
	"github.com/karloscodes/cartridge/examples/notes/web"
)

func main() {
	cfg, err := config.Load("notes")
	if err != nil {
		log.Fatal(err)
	}
	app, err := newApp(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.IsDevelopment() {
		if err := createUser(app.DBManager, "demo@example.com", "password"); err != nil {
			log.Fatal(err)
		}
	}
	// Run returns nil after a graceful shutdown on SIGINT or SIGTERM.
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// newApp builds the app and runs its migrations. main and the tests call it.
func newApp(cfg *config.Config) (*cartridge.App, error) {
	var app *cartridge.App
	app, err := cartridge.NewApp(cfg,
		cartridge.WithDefaults("1.7"),
		cartridge.WithAssets(web.Templates(), web.Static()),
		cartridge.WithSession("/login"),
		// A session ends when its user is gone. The check runs on requests,
		// after NewApp has set app.
		cartridge.WithSessionCheck(func(userID uint, issuedAt time.Time) bool {
			conn, err := app.DBManager.Connect()
			if err != nil {
				return false
			}
			sqlDB, err := conn.DB()
			if err != nil {
				return false
			}
			count, err := db.New(sqlDB).CountUsersByID(context.Background(), int64(userID))
			return err == nil && count == 1
		}),
		cartridge.WithRoutes(routes),
	)
	if err != nil {
		return nil, err
	}
	return app, app.MigrateDatabase(cartridge.NewSQLMigrator(db.Migrations()))
}

func routes(s *cartridge.Server) {
	auth := &cartridge.RouteConfig{CustomMiddleware: []cartridge.HandlerFunc{s.Session().Middleware()}}
	limitLogins := &cartridge.RouteConfig{CustomMiddleware: []cartridge.HandlerFunc{
		middleware.RateLimiter(middleware.WithMax(10), middleware.WithDuration(time.Minute),
			middleware.WithLimitReached(func(c *cartridge.Context) error {
				return c.Render("sessions/new", cartridge.Map{"Error": "Too many tries. Wait a minute."}, "layouts/app")
			})),
	}}

	s.Get("/", func(c *cartridge.Context) error { return c.Redirect("/notes", http.StatusSeeOther) })
	s.Get("/login", showLogin)
	s.Post("/login", login, limitLogins)
	s.Post("/logout", logout, auth)
	s.Get("/notes", listNotes, auth)
	s.Get("/notes/new", newNote, auth)
	s.Post("/notes", createNote, auth)
}
