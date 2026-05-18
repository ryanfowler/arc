package arcx

import (
	"context"
	"encoding"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type customID string

var _ encoding.TextUnmarshaler = (*customID)(nil)

func (id *customID) UnmarshalText(text []byte) error {
	*id = customID("id:" + string(text))
	return nil
}

func TestJSONBindsRequestFields(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	type input struct {
		ID       int64         `param:"id"`
		Limit    int           `query:"limit,default=25"`
		Role     []string      `query:"role"`
		Token    string        `header:"Authorization"`
		Session  string        `cookie:"sid"`
		Timeout  time.Duration `query:"timeout"`
		At       time.Time     `query:"at"`
		CustomID customID      `query:"custom"`
		Body     body          `body:"json"`
	}

	r := New()
	r.Post("/users/{id}", JSON(func(c *Context, in input) (input, error) {
		return in, nil
	}))

	req := httptest.NewRequest(
		http.MethodPost,
		"/users/42?role=admin&role=owner&timeout=5s&at=2026-05-18T12:00:00Z&custom=abc",
		strings.NewReader(`{"name":"Ryan"}`),
	)
	req.Header.Set("Authorization", "Bearer token")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "session"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	gotBody := rec.Body.String()
	for _, want := range []string{
		`"ID":42`,
		`"Limit":25`,
		`"Role":["admin","owner"]`,
		`"Token":"Bearer token"`,
		`"Session":"session"`,
		`"Timeout":5000000000`,
		`"At":"2026-05-18T12:00:00Z"`,
		`"CustomID":"id:abc"`,
		`"Body":{"name":"Ryan"}`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body missing %s: %s", want, gotBody)
		}
	}
}

func TestRequiredBindingError(t *testing.T) {
	r := New()
	r.Get("/search", JSON(func(c *Context, in struct {
		Query string `query:"q,required"`
	}) (struct{}, error) {
		return struct{}{}, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/search", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "query") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestOptionalPointerBodyMayBeAbsent(t *testing.T) {
	type input struct {
		Body *struct {
			Name string `json:"name"`
		} `body:"json"`
	}

	r := New()
	r.Post("/users", JSON(func(c *Context, in input) (bool, error) {
		return in.Body == nil, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "true" {
		t.Fatalf("body = %q, want true", got)
	}
}

func TestValidateIsCalled(t *testing.T) {
	r := New()
	r.Get("/validate", JSON(func(c *Context, in validatingInput) (struct{}, error) {
		return struct{}{}, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/validate", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

type validatingInput struct{}

func (validatingInput) Validate(context.Context) error {
	return errors.New("invalid input")
}

func TestJSONPanicsForUnsupportedInputType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("recover = nil, want panic")
		}
	}()
	_ = JSON(func(c *Context, in string) (struct{}, error) {
		return struct{}{}, nil
	})
}

func TestJSONPanicsForMultipleBodyFields(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("recover = nil, want panic")
		}
	}()
	_ = JSON(func(c *Context, in struct {
		A struct{} `body:"json"`
		B struct{} `body:"json"`
	}) (struct{}, error) {
		return struct{}{}, nil
	})
}
