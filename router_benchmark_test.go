package arc

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ryanfowler/match"
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
					r.Handle("/users/"+strconv.Itoa(route)+"/posts/{id}", handler)
				}
			}
		})
	}
}

func BenchmarkNormalizePercentEncodedPattern(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "no_escape_static",
			pattern: "/healthz",
		},
		{
			name:    "no_escape_param",
			pattern: "/users/{id}/posts/{postID}",
		},
		{
			name:    "decoded_space",
			pattern: "/files/meta%20data",
		},
		{
			name:    "escaped_slash",
			pattern: "/files/a%2Fb/meta%20data",
		},
		{
			name:    "decoded_literal_braces",
			pattern: "/files/%7Bmeta%7D",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				benchmarkParam = normalizePercentEncodedPattern(bm.pattern)
			}
		})
	}
}

func BenchmarkValidateUniqueParamNames(b *testing.B) {
	benchmarks := []struct {
		name    string
		pattern string
	}{
		{
			name:    "static",
			pattern: "/healthz",
		},
		{
			name:    "one_param",
			pattern: "/users/{id}",
		},
		{
			name:    "four_params",
			pattern: "/{tenant}/{version}/{resource}/{id}",
		},
		{
			name:    "five_params",
			pattern: "/{tenant}/{version}/{resource}/{id}/{slug}",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if err := validateUniqueParamNames(bm.pattern); err != nil {
					b.Fatal(err)
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
					benchmarkParam = req.PathValue("id")
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
					benchmarkParam = req.PathValue("path")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/assets/css/app.css", nil)
			},
		},
		{
			name: "escaped_slash_param",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.PathValue("name")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil)
			},
		},
		{
			name: "escaped_slash_decoded_static",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/files/{name}/meta data", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.PathValue("name")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "/files/a%2Fb/meta%20data", nil)
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
					benchmarkParam = req.PathValue("id")
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
					benchmarkParam = req.PathValue("tenant")
					w.WriteHeader(http.StatusNoContent)
				})
				return r, httptest.NewRequest(http.MethodGet, "http://acme.example.com/users/42", nil)
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
					benchmarkParam = req.PathValue("id")
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

func BenchmarkRouterServeHTTPRealWorld(b *testing.B) {
	benchmarks := []struct {
		name string
		new  func() (*Router, *http.Request)
	}{
		{
			name: "tenant_api_detail_route",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Use(benchmarkMiddleware)

				tenant := r.Host("{tenant}.example.com")
				api := tenant.SubRouter("/api/{version}")
				for i := 0; i < 40; i++ {
					api.Get("/resources/"+strconv.Itoa(i), writeBenchmarkStatus(http.StatusNoContent))
				}
				api.Get("/users/{id}/projects/{projectID}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.PathValue("projectID")
					w.WriteHeader(http.StatusNoContent)
				})

				return r, httptest.NewRequest(http.MethodGet, "https://acme.example.com/api/v1/users/42/projects/abc", nil)
			},
		},
		{
			name: "mounted_assets",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.URL.Path
					w.WriteHeader(http.StatusNoContent)
				}))
				r.Get("/assets/healthz", writeBenchmarkStatus(http.StatusAccepted))

				return r, httptest.NewRequest(http.MethodGet, "/assets/css/app.css", nil)
			},
		},
		{
			name: "api_not_found_below_subrouter",
			new: func() (*Router, *http.Request) {
				r := New()
				api := r.SubRouter("/api")
				api.Get("/users/{id}", writeBenchmarkStatus(http.StatusNoContent))

				return r, httptest.NewRequest(http.MethodGet, "/api/missing", nil)
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

func BenchmarkRouterServeHTTPEdgeCases(b *testing.B) {
	benchmarks := []struct {
		name string
		new  func() (*Router, *http.Request)
	}{
		{
			name: "relaxed_trailing_slash_param",
			new: func() (*Router, *http.Request) {
				r := New()
				r.SetStrictSlash(false)
				r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.PathValue("id")
					w.WriteHeader(http.StatusNoContent)
				})

				return r, httptest.NewRequest(http.MethodGet, "/users/42/", nil)
			},
		},
		{
			name: "direct_route_below_subrouter",
			new: func() (*Router, *http.Request) {
				r := New()
				api := r.SubRouter("/api")
				api.Get("/users/{id}", writeBenchmarkStatus(http.StatusNoContent))
				r.Get("/api/healthz", writeBenchmarkStatus(http.StatusAccepted))

				return r, httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
			},
		},
		{
			name: "uppercase_host_with_port",
			new: func() (*Router, *http.Request) {
				r := New()
				tenant := r.Host("{tenant}.example.com")
				tenant.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
					benchmarkParam = req.PathValue("tenant")
					w.WriteHeader(http.StatusNoContent)
				})

				return r, httptest.NewRequest(http.MethodGet, "https://ACME.EXAMPLE.COM:8443/users/42", nil)
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
			var m match.Router[*childRouter]
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
				got, ok := m.MatchPrefix(bm.path)
				matched = ok
				if ok {
					param = got.Params.Get(bm.param)
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
