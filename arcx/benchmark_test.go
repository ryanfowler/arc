package arcx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var (
	benchmarkArcxStatus int
	benchmarkArcxString string
)

type benchmarkResponseWriter struct {
	header http.Header
	status int
}

func (w *benchmarkResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *benchmarkResponseWriter) Write([]byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return 0, nil
}

func (w *benchmarkResponseWriter) WriteHeader(status int) {
	w.status = status
}

func BenchmarkArcxImperativeStatic(b *testing.B) {
	r := New()
	r.Get("/healthz", func(c *Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := &benchmarkResponseWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.status = 0
		r.ServeHTTP(w, req)
	}
	benchmarkArcxStatus = w.status
}

func BenchmarkArcxImperativeParam(b *testing.B) {
	r := New()
	r.Get("/users/{id}", func(c *Context) error {
		id, err := c.Param("id").Int64()
		if err != nil {
			return err
		}
		benchmarkArcxString = strconv.FormatInt(id, 10)
		return c.NoContent(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := &benchmarkResponseWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.status = 0
		r.ServeHTTP(w, req)
	}
	benchmarkArcxStatus = w.status
}

func BenchmarkArcxJSONQueryParam(b *testing.B) {
	type input struct {
		ID    int64 `param:"id"`
		Limit int   `query:"limit,default=25"`
	}
	r := New()
	r.Get("/users/{id}", JSON(func(c *Context, in input) (map[string]int64, error) {
		return map[string]int64{"id": in.ID, "limit": int64(in.Limit)}, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/users/42?limit=50", nil)
	w := &benchmarkResponseWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.status = 0
		r.ServeHTTP(w, req)
	}
	benchmarkArcxStatus = w.status
}

func BenchmarkArcxJSONPathQueryBody(b *testing.B) {
	type input struct {
		ID   int64 `param:"id"`
		Body struct {
			Name string `json:"name"`
		} `body:"json"`
	}
	r := New()
	r.Post("/users/{id}", JSON(func(c *Context, in input) (map[string]string, error) {
		return map[string]string{"name": in.Body.Name}, nil
	}))
	w := &benchmarkResponseWriter{}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.status = 0
		req := httptest.NewRequest(http.MethodPost, "/users/42", strings.NewReader(`{"name":"Ryan"}`))
		r.ServeHTTP(w, req)
	}
	benchmarkArcxStatus = w.status
}

func BenchmarkArcxBindingPlanBuild(b *testing.B) {
	type input struct {
		ID    int64    `param:"id"`
		Role  []string `query:"role"`
		Token string   `header:"Authorization"`
	}
	t := reflect.TypeFor[input]()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := buildBindPlan(t); err != nil {
			b.Fatal(err)
		}
	}
}
