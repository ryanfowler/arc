package arcx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPErrorResponse(t *testing.T) {
	r := New()
	r.Get("/conflict", func(c *Context) error {
		return Conflict("already exists")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conflict", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestUnknownErrorDoesNotLeakDetails(t *testing.T) {
	r := New()
	r.Get("/boom", func(c *Context) error {
		return errors.New("database password is secret")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "database password") {
		t.Fatalf("body leaked internal error: %q", rec.Body.String())
	}
}

func TestCustomErrorHandler(t *testing.T) {
	r := New(WithErrorHandler(func(c *Context, err error) {
		c.ResponseWriter.Header().Set("X-Error", err.Error())
		c.ResponseWriter.WriteHeader(http.StatusTeapot)
	}))
	r.Get("/error", func(c *Context) error {
		return errors.New("custom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/error", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if got := rec.Header().Get("X-Error"); got != "custom" {
		t.Fatalf("X-Error = %q", got)
	}
}
