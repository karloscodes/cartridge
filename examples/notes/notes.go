package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/crypto"
	"github.com/karloscodes/cartridge/flash"
	"github.com/karloscodes/cartridge/sqlite"

	"github.com/karloscodes/cartridge/examples/notes/db"
)

// The schema is in db/migrations and the queries are in db/queries.sql.
// sqlc writes the types db.User and db.Note and one function per query.

const maxNoteLength = 500

// validateNote returns the problems with a note, by field name.
func validateNote(body string) map[string]string {
	problems := map[string]string{}
	switch {
	case body == "":
		problems["Body"] = "Write something first."
	case utf8.RuneCountInString(body) > maxNoteLength:
		problems["Body"] = fmt.Sprintf("Keep it under %d characters.", maxNoteLength)
	}
	return problems
}

// createUser adds a user, or keeps the one with that email.
func createUser(m cartridge.DBManager, email, password string) error {
	hash, err := crypto.GeneratePasswordHash(password)
	if err != nil {
		return err
	}
	return cartridge.WriteSQL(context.Background(), m, func(tx *sql.Tx) error {
		return db.New(tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: string(hash)})
	})
}

// withFlash adds the flash message to a page's data. Reading it clears it.
func withFlash(c *cartridge.Context, data cartridge.Map) cartridge.Map {
	data["Flash"] = flash.GetFlash(c.Response(), c.Request())
	return data
}

func showLogin(c *cartridge.Context) error {
	return c.Render("sessions/new", withFlash(c, cartridge.Map{}), "layouts/app")
}

func login(c *cartridge.Context) error {
	email := strings.ToLower(strings.TrimSpace(c.Input("email")))
	user, err := db.New(c.SQL()).UserByEmail(c.Context(), email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil || !crypto.VerifyPassword(user.PasswordHash, c.Input("password")) {
		return c.Status(http.StatusUnprocessableEntity).Render("sessions/new",
			cartridge.Map{"Email": email, "Error": "Wrong email or password."}, "layouts/app")
	}
	if err := c.Session.SetSession(c, uint(user.ID)); err != nil {
		return err
	}
	return c.FlashSuccess("Signed in.").RedirectLocal(c.Query("next"), "/notes")
}

func logout(c *cartridge.Context) error {
	c.Session.ClearSession(c)
	return c.Redirect("/login", http.StatusSeeOther)
}

func listNotes(c *cartridge.Context) error {
	userID, _ := c.Session.GetUserID(c)
	// Every query on notes takes the signed-in user.
	notes, err := db.New(c.SQL()).NotesOfUser(c.Context(), int64(userID))
	if err != nil {
		return err
	}
	return c.Render("notes/index", withFlash(c, cartridge.Map{"Notes": notes}), "layouts/app")
}

func newNote(c *cartridge.Context) error {
	return c.Render("notes/new", cartridge.Map{"Body": "", "Problems": map[string]string{}}, "layouts/app")
}

func createNote(c *cartridge.Context) error {
	userID, _ := c.Session.GetUserID(c)
	body := strings.TrimSpace(c.Input("body"))
	if problems := validateNote(body); len(problems) > 0 {
		// 422 keeps what the user typed on the screen, and Turbo shows it.
		return c.Status(http.StatusUnprocessableEntity).Render("notes/new",
			cartridge.Map{"Body": body, "Problems": problems}, "layouts/app")
	}

	err := c.WriteSQL(func(tx *sql.Tx) error {
		return db.New(tx).CreateNote(c.Context(), db.CreateNoteParams{UserID: int64(userID), Body: body})
	})
	if errors.Is(err, sqlite.ErrBusy) {
		c.Set("Retry-After", "5")
		return cartridge.NewError(http.StatusServiceUnavailable, "Busy. Try again in a few seconds.")
	}
	if err != nil {
		return err
	}
	return c.FlashSuccess("Note saved.").Redirect("/notes", http.StatusSeeOther)
}
