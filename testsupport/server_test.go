package testsupport

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/karloscodes/cartridge"
)

func TestTestServer(t *testing.T) {
	newServer := func(t *testing.T) *TestServer {
		return NewTestServer(t, TestServerOptions{RouteMountFunc: func(s *cartridge.Server) {
			s.Post("/notes", func(c *cartridge.Context) error {
				var in struct {
					Title string `json:"title" form:"title"`
				}
				if err := c.Bind(&in); err != nil {
					return err
				}
				return c.SendString(in.Title)
			})
		}})
	}

	t.Run("PostForm sends a form that passes CSRF protection", func(t *testing.T) {
		ts := newServer(t)

		resp := ts.PostForm("/notes", url.Values{"title": {"Hello"}})

		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(got) != "Hello" {
			t.Errorf("status %d, body %q", resp.StatusCode, got)
		}
	})

	t.Run("Post sends JSON that passes CSRF protection", func(t *testing.T) {
		ts := newServer(t)

		resp := ts.Post("/notes", `{"title":"Hi"}`)

		got, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(got) != "Hi" {
			t.Errorf("status %d, body %q", resp.StatusCode, got)
		}
	})
}
