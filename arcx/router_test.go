package arcx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryanfowler/arc"
)

func TestRouterGet(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(c *Context) error {
		if got := c.Param("id").String(); got != "42" {
			t.Fatalf("id = %q, want 42", got)
		}
		return c.JSON(http.StatusAccepted, map[string]string{"ok": "true"})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
}

func TestRouterMiddleware(t *testing.T) {
	r := New()
	var sawPattern string
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			sawPattern = req.Pattern
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/users/{id}", func(c *Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if sawPattern != "/users/{id}" {
		t.Fatalf("middleware pattern = %q", sawPattern)
	}
}

func TestSubRouterAndHostParams(t *testing.T) {
	r := New()
	api := r.Host("{tenant}.example.com").SubRouter("/api/{version}")
	api.Get("/users/{id}", JSON(func(c *Context, in struct {
		Tenant  string `param:"tenant"`
		Version string `param:"version"`
		ID      string `param:"id"`
	}) (map[string]string, error) {
		return map[string]string{
			"tenant":  in.Tenant,
			"version": in.Version,
			"id":      in.ID,
		}, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://acme.example.com/api/v1/users/42", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "{\"id\":\"42\",\"tenant\":\"acme\",\"version\":\"v1\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestBaseAllowsRawArcRoutes(t *testing.T) {
	r := New()
	r.Base().Get("/raw", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/raw", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestWrapUsesExistingRouter(t *testing.T) {
	base := arc.New()
	r := Wrap(base)
	r.Get("/wrapped", func(c *Context) error {
		return c.NoContent(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	base.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wrapped", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestTryHandleReturnsRegistrationErrors(t *testing.T) {
	r := New()
	if err := r.TryHandle(http.MethodGet, "relative", func(*Context) error { return nil }); err == nil {
		t.Fatal("TryHandle error = nil, want error")
	}
}

func TestMountAcceptsHTTPHandler(t *testing.T) {
	r := New()
	r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/app.css" {
			t.Fatalf("mounted path = %q", req.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}
