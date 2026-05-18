package arcx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONOptionsDisallowUnknownFields(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		} `body:"json"`
	}

	r := New(WithJSONOptions(JSONOptions{DisallowUnknownFields: true}))
	r.Post("/users", JSON(func(c *Context, in input) (struct{}, error) {
		return struct{}{}, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ryan","extra":true}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestJSONRejectsTrailingValue(t *testing.T) {
	type input struct {
		Body struct {
			Name string `json:"name"`
		} `body:"json"`
	}

	r := New()
	r.Post("/users", JSON(func(c *Context, in input) (struct{}, error) {
		return struct{}{}, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ryan"} {}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestResponseMetadata(t *testing.T) {
	r := New()
	r.Post("/users", JSON(func(c *Context, in struct{}) (Response[map[string]string], error) {
		resp := Created(map[string]string{"id": "42"})
		resp.Header = http.Header{"X-Request-ID": []string{"abc"}}
		return resp, nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "abc" {
		t.Fatalf("X-Request-ID = %q", got)
	}
}

func TestNoBodyResponse(t *testing.T) {
	r := New()
	r.Delete("/users/{id}", JSON(func(c *Context, in struct {
		ID string `param:"id"`
	}) (Response[struct{}], error) {
		return NoBody(http.StatusNoContent), nil
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/users/42", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}
