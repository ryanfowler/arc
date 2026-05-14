package arc

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

var (
	benchmarkParam  string
	benchmarkStatus int
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

func BenchmarkRouterHandle(b *testing.B) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	for _, routeCount := range []int{1, 10, 100} {
		b.Run(strconv.Itoa(routeCount)+"_routes", func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				r := New()
				for route := 0; route < routeCount; route++ {
					r.Handle(http.MethodGet, "/users/"+strconv.Itoa(route)+"/posts/{id}", handler)
				}
			}
		})
	}
}

func BenchmarkRouterServeHTTP(b *testing.B) {
	benchmarks := []struct {
		name string
		new  func() (*Router, *http.Request)
	}{
		{
			name: "static",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/healthz", writeBenchmarkStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodGet, "/healthz", nil)
			},
		},
		{
			name: "static_among_many",
			new: func() (*Router, *http.Request) {
				r := New()
				for i := 0; i < 100; i++ {
					r.Get("/articles/"+strconv.Itoa(i), writeBenchmarkStatus(http.StatusNoContent))
				}
				return r, httptest.NewRequest(http.MethodGet, "/articles/73", nil)
			},
		},
		{
			name: "param",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = Param(req, "id")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/users/42", nil)
			},
		},
		{
			name: "catch_all",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/assets/{*path}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = Param(req, "path")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/assets/css/app.css", nil)
			},
		},
		{
			name: "method_not_allowed",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/users/{id}", writeBenchmarkStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodPost, "/users/42", nil)
			},
		},
		{
			name: "not_found",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/known", writeBenchmarkStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodGet, "/missing", nil)
			},
		},
		{
			name: "subrouter_params",
			new: func() (*Router, *http.Request) {
				r := New()
				api := r.SubRouter("/api/{version}")
				api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = Param(req, "id")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil)
			},
		},
		{
			name: "host_params",
			new: func() (*Router, *http.Request) {
				r := New()
				tenant := r.Host("{tenant}.example.com")
				tenant.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = Param(req, "tenant")
					w.WriteHeader(http.StatusNoContent)
				})
				req := httptest.NewRequest(http.MethodGet, "http://acme.example.com/users/42", nil)
				req.Host = "ACME.example.com:8080"
				return r, req
			},
		},
		{
			name: "middleware_chain",
			new: func() (*Router, *http.Request) {
				r := New()
				for i := 0; i < 8; i++ {
					r.Use(benchmarkMiddleware)
				}
				r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = Param(req, "id")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/users/42", nil)
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			r, req := bm.new()
			w := &benchmarkResponseWriter{}
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				w.status = 0
				r.ServeHTTP(w, req)
			}

			benchmarkStatus = w.status
		})
	}
}

func BenchmarkMountMatcherMatch(b *testing.B) {
	benchmarks := []struct {
		name  string
		path  string
		param string
	}{
		{
			name:  "param_siblings_first",
			path:  "/tenant/route0",
			param: "tenant0",
		},
		{
			name:  "param_siblings_last",
			path:  "/tenant/route99",
			param: "tenant99",
		},
		{
			name:  "param_siblings_miss",
			path:  "/tenant/missing",
			param: "tenant0",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			var m mountMatcher
			for i := 0; i < 100; i++ {
				pattern := "/{tenant" + strconv.Itoa(i) + "}/route" + strconv.Itoa(i)
				if err := m.TryInsert(pattern, &childRouter{}); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			var matched bool
			var param string
			for i := 0; i < b.N; i++ {
				_, _, params, ok := m.Match(bm.path)
				matched = ok
				if ok {
					param = params.Get(bm.param)
				}
			}

			if matched {
				benchmarkStatus = 1
			} else {
				benchmarkStatus = 0
			}
			benchmarkParam = param
		})
	}
}

func writeBenchmarkStatus(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}
}

func benchmarkMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req)
	})
}
