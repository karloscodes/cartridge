package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/karloscodes/cartridge"
	"github.com/karloscodes/cartridge/crypto"
	"github.com/karloscodes/cartridge/flash"
	"github.com/karloscodes/cartridge/sqlite"
)

// User signs in with an email and a password.
type User struct {
	ID           uint
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
}

// Note is a short text that belongs to one user.
type Note struct {
	ID        uint
	UserID    uint `gorm:"index"`
	Body      string
	CreatedAt time.Time
}

const maxNoteLength = 500

// Validate returns the problems with the note, by field name.
func (n Note) Validate() map[string]string {
	problems := map[string]string{}
	switch {
	case n.Body == "":
		problems["Body"] = "Write something first."
	case utf8.RuneCountInString(n.Body) > maxNoteLength:
		problems["Body"] = fmt.Sprintf("Keep it under %d characters.", maxNoteLength)
	}
	return problems
}

// createUser adds a user, or keeps the one with that email.
func createUser(db cartridge.DBManager, email, password string) error {
	hash, err := crypto.GeneratePasswordHash(password)
	if err != nil {
		return err
	}
	return cartridge.Write(context.Background(), db, func(tx *gorm.DB) error {
		return tx.Where(User{Email: email}).Attrs(User{PasswordHash: string(hash)}).FirstOrCreate(&User{}).Error
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
	var user User
	err := c.DB().Where("email = ?", email).First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil || !crypto.VerifyPassword(user.PasswordHash, c.Input("password")) {
		return c.Status(http.StatusUnprocessableEntity).Render("sessions/new",
			cartridge.Map{"Email": email, "Error": "Wrong email or password."}, "layouts/app")
	}
	if err := c.Session.SetSession(c, user.ID); err != nil {
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
	var notes []Note
	// Scope every query by the signed-in user.
	if err := c.DB().Where("user_id = ?", userID).Order("created_at DESC, id DESC").Find(&notes).Error; err != nil {
		return err
	}
	return c.Render("notes/index", withFlash(c, cartridge.Map{"Notes": notes}), "layouts/app")
}

func newNote(c *cartridge.Context) error {
	return c.Render("notes/new", cartridge.Map{"Note": Note{}, "Problems": map[string]string{}}, "layouts/app")
}

func createNote(c *cartridge.Context) error {
	userID, _ := c.Session.GetUserID(c)
	note := Note{UserID: userID, Body: strings.TrimSpace(c.Input("body"))}
	if problems := note.Validate(); len(problems) > 0 {
		// 422 keeps what the user typed on the screen, and Turbo shows it.
		return c.Status(http.StatusUnprocessableEntity).Render("notes/new",
			cartridge.Map{"Note": note, "Problems": problems}, "layouts/app")
	}

	err := c.WriteTx(func(tx *gorm.DB) error { return tx.Create(&note).Error })
	if errors.Is(err, sqlite.ErrBusy) {
		c.Set("Retry-After", "5")
		return cartridge.NewError(http.StatusServiceUnavailable, "Busy. Try again in a few seconds.")
	}
	if err != nil {
		return err
	}
	return c.FlashSuccess("Note saved.").Redirect("/notes", http.StatusSeeOther)
}
