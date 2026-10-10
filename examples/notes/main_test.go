package main

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/karloscodes/cartridge/testsupport"
)

// The tests run the real app: its migrations, routes, and templates, on a
// new SQLite file for each test.

const password = "correct horse battery staple"

// The hash has the lowest bcrypt cost, so the tests stay fast.
// VerifyPassword reads the cost from the hash.
var passwordHash, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)

func newTestApp(t *testing.T) *testsupport.TestApp {
	t.Helper()
	ta := testsupport.NewTestApp(t, "notes", newApp)
	for _, email := range []string{"ada@example.com", "grace@example.com"} {
		if err := ta.DB().Create(&User{Email: email, PasswordHash: string(passwordHash)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return ta
}

// signIn returns a browser with the user's session.
func signIn(t *testing.T, ta *testsupport.TestApp, email string) *testsupport.Client {
	t.Helper()
	browser := ta.Client()
	resp := browser.PostForm("/login", url.Values{"email": {email}, "password": {password}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("sign in as %s: status %d", email, resp.StatusCode)
	}
	return browser
}

func read(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestSignIn(t *testing.T) {
	t.Run("a visitor is sent to the sign-in page", func(t *testing.T) {
		ta := newTestApp(t)

		resp := ta.Get("/notes")

		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login" {
			t.Errorf("GET /notes = %d to %q, want a redirect to /login", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("a wrong password shows the form again, with the email kept", func(t *testing.T) {
		ta := newTestApp(t)

		resp := ta.PostForm("/login", url.Values{"email": {"ada@example.com"}, "password": {"wrong"}})

		page := read(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(page, "Wrong email or password.") {
			t.Errorf("status %d, page:\n%s", resp.StatusCode, page)
		}
		if !strings.Contains(page, `value="ada@example.com"`) {
			t.Error("the form lost the email")
		}
	})

	t.Run("the right password opens the notes, with a flash", func(t *testing.T) {
		ta := newTestApp(t)
		browser := ta.Client()

		resp := browser.PostForm("/login", url.Values{"email": {"Ada@Example.com "}, "password": {password}})
		page := read(t, browser.Get(resp.Header.Get("Location")))

		if resp.Header.Get("Location") != "/notes" || !strings.Contains(page, "Signed in.") {
			t.Errorf("redirect to %q, page:\n%s", resp.Header.Get("Location"), page)
		}
	})

	t.Run("signing out ends the session", func(t *testing.T) {
		ta := newTestApp(t)
		browser := signIn(t, ta, "ada@example.com")

		browser.PostForm("/logout", nil)
		resp := browser.Get("/notes")

		if resp.StatusCode != http.StatusFound {
			t.Errorf("GET /notes after sign out = %d, want a redirect", resp.StatusCode)
		}
	})
}

func TestNotes(t *testing.T) {
	t.Run("a saved note shows in the list, with a flash", func(t *testing.T) {
		ta := newTestApp(t)
		browser := signIn(t, ta, "ada@example.com")

		resp := browser.PostForm("/notes", url.Values{"body": {"Buy milk"}})
		page := read(t, browser.Get("/notes"))

		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST /notes = %d, want 303", resp.StatusCode)
		}
		for _, want := range []string{"Note saved.", "<p>Buy milk</p>", "1 note", "just now"} {
			if !strings.Contains(page, want) {
				t.Errorf("the list lacks %q:\n%s", want, page)
			}
		}
	})

	t.Run("an empty note shows the problem and saves nothing", func(t *testing.T) {
		ta := newTestApp(t)
		browser := signIn(t, ta, "ada@example.com")

		resp := browser.PostForm("/notes", url.Values{"body": {"   "}})

		var count int64
		ta.DB().Model(&Note{}).Count(&count)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(read(t, resp), "Write something first.") {
			t.Errorf("POST /notes = %d without the problem", resp.StatusCode)
		}
		if count != 0 {
			t.Errorf("%d notes saved, want 0", count)
		}
	})

	t.Run("a note that is too long keeps what the user typed", func(t *testing.T) {
		ta := newTestApp(t)
		browser := signIn(t, ta, "ada@example.com")
		long := strings.Repeat("a", maxNoteLength+1)

		resp := browser.PostForm("/notes", url.Values{"body": {long}})

		page := read(t, resp)
		if !strings.Contains(page, "Keep it under 500 characters.") || !strings.Contains(page, ">"+long+"</textarea>") {
			t.Errorf("status %d, page:\n%s", resp.StatusCode, page)
		}
	})

	t.Run("a user sees only their own notes", func(t *testing.T) {
		ta := newTestApp(t)
		signIn(t, ta, "grace@example.com").PostForm("/notes", url.Values{"body": {"Grace's secret"}})

		page := read(t, signIn(t, ta, "ada@example.com").Get("/notes"))

		if strings.Contains(page, "Grace&#39;s secret") || !strings.Contains(page, "No notes yet.") {
			t.Errorf("Ada's list:\n%s", page)
		}
	})

	t.Run("the page links its stylesheet by a digested URL that the server serves", func(t *testing.T) {
		ta := newTestApp(t)
		page := read(t, signIn(t, ta, "ada@example.com").Get("/notes"))

		link := regexp.MustCompile(`href="(/assets/app-[0-9a-f]{8}\.css)"`).FindStringSubmatch(page)
		if link == nil {
			t.Fatalf("no digested stylesheet in:\n%s", page)
		}
		resp := ta.Get(link[1])

		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s = %d, Cache-Control %q", link[1], resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	})
}
