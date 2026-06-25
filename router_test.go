package arc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ryanfowler/match"
)

var normalizeHostSink string

type allocationResponseWriter struct {
	header http.Header
	status int
}

func (w *allocationResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *allocationResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}

func (w *allocationResponseWriter) WriteHeader(status int) {
	w.status = status
}

func allocationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req)
	})
}

func TestRouterMatchesRouteAndParams(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRouterMatchesStaticBeforeParameterizedRoute(t *testing.T) {
	r := New()
	r.Get("/users/me", writeStatus(http.StatusAccepted))
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterMatchesCatchAll(t *testing.T) {
	r := New()
	r.Get("/assets/{*path}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("path"); got != "css/app.css" {
			t.Fatalf("PathValue(path) = %q, want %q", got, "css/app.css")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/css/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterMatchesEscapedSlashWithinSegment(t *testing.T) {
	t.Run("single segment route matches", func(t *testing.T) {
		r := New()
		r.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
			if got := req.URL.Path; got != "/files/a/b" {
				t.Fatalf("req.URL.Path = %q, want %q", got, "/files/a/b")
			}
			if got := req.PathValue("name"); got != "a/b" {
				t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
			}
			if got := req.PathValue("name"); got != "a/b" {
				t.Fatalf("req.PathValue(name) = %q, want %q", got, "a/b")
			}
			w.WriteHeader(http.StatusAccepted)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})

	t.Run("catch all sees decoded slash", func(t *testing.T) {
		r := New()
		r.Get("/files/{*path}", func(w http.ResponseWriter, req *http.Request) {
			if got := req.URL.Path; got != "/files/a/b" {
				t.Fatalf("req.URL.Path = %q, want %q", got, "/files/a/b")
			}
			if got := req.PathValue("path"); got != "a/b" {
				t.Fatalf("PathValue(path) = %q, want %q", got, "a/b")
			}
			w.WriteHeader(http.StatusAccepted)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})
}

func TestRouterMatchesEscapedSlashWithDecodedStaticSegments(t *testing.T) {
	t.Run("decoded space segment", func(t *testing.T) {
		r := New()
		r.Get("/files/{name}/meta data", func(w http.ResponseWriter, req *http.Request) {
			if got := req.PathValue("name"); got != "a/b" {
				t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
			}
			w.WriteHeader(http.StatusAccepted)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb/meta%20data", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})

	t.Run("decoded literal braces segment", func(t *testing.T) {
		r := New()
		r.Get("/files/{name}/{{meta}}", func(w http.ResponseWriter, req *http.Request) {
			if got := req.PathValue("name"); got != "a/b" {
				t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
			}
			w.WriteHeader(http.StatusAccepted)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb/%7Bmeta%7D", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})

	t.Run("literal nul stays distinct from escaped slash", func(t *testing.T) {
		r := New()
		r.Get("/files/{name}/{slug}", func(w http.ResponseWriter, req *http.Request) {
			if got := req.PathValue("name"); got != "a\x00b" {
				t.Fatalf("PathValue(name) = %q, want %q", got, "a\x00b")
			}
			if got := req.PathValue("slug"); got != "c/d" {
				t.Fatalf("PathValue(slug) = %q, want %q", got, "c/d")
			}
			w.WriteHeader(http.StatusAccepted)
		})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%00b/c%2Fd", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})
}

func TestRouterMatchesStaticEscapedSlashWithinSegment(t *testing.T) {
	r := New()
	r.Get("/files/a%2Fb", writeStatus(http.StatusAccepted))

	t.Run("escaped slash", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil))

		assertStatus(t, rec, http.StatusAccepted)
	})

	t.Run("path separator", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a/b", nil))

		assertStatus(t, rec, http.StatusNotFound)
	})
}

func TestRouterNormalizesPercentEncodedStaticPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
	}{
		{
			name:    "decoded space segment",
			pattern: "/files/meta%20data",
			path:    "/files/meta%20data",
		},
		{
			name:    "decoded literal braces segment",
			pattern: "/files/%7Bmeta%7D",
			path:    "/files/%7Bmeta%7D",
		},
		{
			name:    "decoded percent segment",
			pattern: "/files/%25done",
			path:    "/files/%25done",
		},
		{
			name:    "escaped slash and decoded space segment",
			pattern: "/files/a%2Fb/meta%20data",
			path:    "/files/a%2Fb/meta%20data",
		},
		{
			name:    "escaped slash and decoded literal braces segment",
			pattern: "/files/a%2Fb/%7Bmeta%7D",
			path:    "/files/a%2Fb/%7Bmeta%7D",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			r.Get(tt.pattern, writeStatus(http.StatusAccepted))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assertStatus(t, rec, http.StatusAccepted)
		})
	}
}

func TestRouterPercentEncodedStaticPatternConflictsWithDecodedPattern(t *testing.T) {
	tests := []struct {
		name    string
		first   string
		second  string
		request string
	}{
		{
			name:    "space",
			first:   "/files/meta data",
			second:  "/files/meta%20data",
			request: "/files/meta%20data",
		},
		{
			name:    "literal braces",
			first:   "/files/{{meta}}",
			second:  "/files/%7Bmeta%7D",
			request: "/files/%7Bmeta%7D",
		},
		{
			name:    "percent",
			first:   "/files/%done",
			second:  "/files/%25done",
			request: "/files/%25done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			if err := r.TryHandleAll(tt.first, writeStatus(http.StatusAccepted)); err != nil {
				t.Fatalf("TryHandleAll first route error = %v", err)
			}

			var conflict *match.ConflictError
			if err := r.TryHandleAll(tt.second, writeStatus(http.StatusNoContent)); !errors.As(err, &conflict) {
				t.Fatalf("TryHandleAll second route error = %v, want *match.ConflictError", err)
			}

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.request, nil))

			assertStatus(t, rec, http.StatusAccepted)
		})
	}
}

func TestRouterDoesNotMatchStaticRouteWithEscapedSlash(t *testing.T) {
	r := New()
	r.Get("/files/a/b", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestRouterDecodesEscapedParamValue(t *testing.T) {
	r := New()
	r.Get("/search/{query}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("query"); got != "what's up" {
			t.Fatalf("PathValue(query) = %q, want %q", got, "what's up")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/search/what%27s%20up", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterUsesDecodedPathWhenRawPathHasNoEscapedSlash(t *testing.T) {
	r := New()
	r.Get("/search/{query}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("query"); got != "what's up" {
			t.Fatalf("PathValue(query) = %q, want %q", got, "what's up")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/search/what%27s%20up", nil)
	req.URL.RawPath = "/search/what%27s%20up"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterEscapesLiteralBracesInPattern(t *testing.T) {
	r := New()
	r.Get("/files/{{name}}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/%7Bname%7D", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterDoesNotCleanRequestPath(t *testing.T) {
	tests := []struct {
		name  string
		route string
		path  string
	}{
		{"dot dot segment", "/admin", "/static/../admin"},
		{"repeated slash", "/static/admin", "/static//admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			r.Get(tt.route, writeStatus(http.StatusAccepted))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assertStatus(t, rec, http.StatusNotFound)
			if got := rec.Header().Get("Location"); got != "" {
				t.Fatalf("Location = %q, want empty", got)
			}
		})
	}
}

func TestRouterStrictSlashDefaultRejectsTrailingSlash(t *testing.T) {
	r := New()
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestRouterRelaxedSlashMatchesTrailingSlash(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		if got := req.URL.Path; got != "/users/42/" {
			t.Fatalf("URL.Path = %q, want %q", got, "/users/42/")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterRelaxedSlashPreservesExactTrailingSlashRoute(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	r.Get("/resource", writeStatus(http.StatusAccepted))
	r.Get("/resource/", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/resource/", nil))

	assertStatus(t, rec, http.StatusCreated)
}

func TestRouterRelaxedSlashDoesNotAddSecondTrailingSlash(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	r.Get("/resource/", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/resource//", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestRouterRelaxedSlashDoesNotMatchDoubleSlashToRoot(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	r.Get("/", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "//", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestRouterRelaxedSlashReturnsMethodNotAllowed(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusConflict)
	}))
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42/", nil))

	assertStatus(t, rec, http.StatusConflict)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterReturnsNotFound(t *testing.T) {
	r := New()
	r.Get("/known", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))

	assertStatus(t, rec, http.StatusNotFound)
	if got, want := rec.Body.String(), "404 page not found\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("X-Content-Type-Options"), "nosniff"; got != want {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, want)
	}
}

func TestRouterReturnsMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterDefaultFallbackHandlersAllocateZero(t *testing.T) {
	tests := []struct {
		name string
		new  func() (*Router, *http.Request)
	}{
		{
			name: "not_found_without_middleware",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/known", writeStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodGet, "/missing", nil)
			},
		},
		{
			name: "method_not_allowed_without_middleware",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Get("/users/{id}", writeStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodPost, "/users/42", nil)
			},
		},
		{
			name: "not_found_with_middleware",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Use(allocationMiddleware)
				r.Get("/known", writeStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodGet, "/missing", nil)
			},
		},
		{
			name: "method_not_allowed_with_middleware",
			new: func() (*Router, *http.Request) {
				r := New()
				r.Use(allocationMiddleware)
				r.Get("/known", writeStatus(http.StatusNoContent))
				return r, httptest.NewRequest(http.MethodPost, "/known", nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, req := tt.new()
			w := &allocationResponseWriter{header: make(http.Header)}

			allocs := testing.AllocsPerRun(1000, func() {
				w.status = 0
				r.ServeHTTP(w, req)
			})

			if allocs != 0 {
				t.Fatalf("%s allocated %v times, want 0", tt.name, allocs)
			}
		})
	}
}

func TestRouterAllowsSamePatternForDifferentMethods(t *testing.T) {
	r := New()
	r.Get("/resource", writeStatus(http.StatusAccepted))
	r.Post("/resource", writeStatus(http.StatusCreated))

	tests := []struct {
		method string
		want   int
	}{
		{http.MethodGet, http.StatusAccepted},
		{http.MethodPost, http.StatusCreated},
		{http.MethodPut, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tt.method, "/resource", nil))
			assertStatus(t, rec, tt.want)
		})
	}
}

func TestRouterHandleAllMatchesAnyMethod(t *testing.T) {
	r := New()
	r.HandleAll("/resource", writeStatus(http.StatusNoContent))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(method, "/resource", nil))
			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestRouterHandleAllUsesPathSpecificityBeforeMethod(t *testing.T) {
	r := New()
	r.Get("/users/{id}", writeStatus(http.StatusAccepted))
	r.HandleAll("/users/me", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRouterMethodRouteWinsOverAnyMethodForSamePattern(t *testing.T) {
	r := New()
	r.HandleAll("/resource", writeStatus(http.StatusNoContent))
	r.Post("/resource", writeStatus(http.StatusCreated))

	post := httptest.NewRecorder()
	r.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/resource", nil))
	assertStatus(t, post, http.StatusCreated)

	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/resource", nil))
	assertStatus(t, get, http.StatusNoContent)
}

func TestRouterMethodNotAllowedAllowHeaderIncludesRegisteredMethods(t *testing.T) {
	r := New()
	r.Post("/resource/{id}", writeStatus(http.StatusCreated))
	r.Get("/resource/{id}", writeStatus(http.StatusAccepted))
	r.Delete("/resource/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/resource/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "DELETE, GET, HEAD, POST"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterMethodNotAllowedAllowHeaderWithCustomHandler(t *testing.T) {
	r := New()
	r.SetMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict)))
	r.Get("/known", writeStatus(http.StatusNoContent))
	r.Patch("/known", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/known", nil))

	assertStatus(t, rec, http.StatusConflict)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD, PATCH"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterMethodNotAllowedAllowHeaderDoesNotDuplicateExplicitHead(t *testing.T) {
	r := New()
	r.Get("/known", writeStatus(http.StatusNoContent))
	r.Head("/known", writeStatus(http.StatusNoContent))
	r.Post("/known", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/known", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD, POST"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterMethodNotAllowedPassesRouteParams(t *testing.T) {
	r := New()
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusConflict)
	}))
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	assertStatus(t, rec, http.StatusConflict)
}

func TestRouterImplicitHeadUsesGetRoute(t *testing.T) {
	r := New()
	r.Get("/resource", func(w http.ResponseWriter, req *http.Request) {
		if got := req.Method; got != http.MethodHead {
			t.Fatalf("req.Method = %q, want %q", got, http.MethodHead)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/resource", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRouterExplicitHeadWinsOverImplicitGet(t *testing.T) {
	r := New()
	r.Get("/resource", writeStatus(http.StatusAccepted))
	r.Head("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/resource", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRouterAnyMethodWinsOverImplicitHead(t *testing.T) {
	r := New()
	r.Get("/resource", writeStatus(http.StatusAccepted))
	r.HandleAll("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/resource", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRouterSetImplicitHeadFalseRequiresExplicitHeadRoute(t *testing.T) {
	r := New()
	r.SetImplicitHead(false)
	r.Get("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/resource", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
	}
}

func TestSubRouterInheritsImplicitHeadSetting(t *testing.T) {
	r := New()
	r.SetImplicitHead(false)
	api := r.SubRouter("/api")
	api.Get("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/resource", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}

func TestSubRouterSnapshotsImplicitHeadSetting(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	r.SetImplicitHead(false)
	api.Get("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/resource", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterInheritsImplicitHeadSetting(t *testing.T) {
	r := New()
	r.SetImplicitHead(false)
	api := r.Host("api.example.com")
	api.Get("/resource", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "http://api.example.com/resource", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}

func TestMethodHelpers(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		register func(*Router, string, http.HandlerFunc)
	}{
		{"Get", http.MethodGet, (*Router).Get},
		{"Post", http.MethodPost, (*Router).Post},
		{"Put", http.MethodPut, (*Router).Put},
		{"Patch", http.MethodPatch, (*Router).Patch},
		{"Delete", http.MethodDelete, (*Router).Delete},
		{"Head", http.MethodHead, (*Router).Head},
		{"Options", http.MethodOptions, (*Router).Options},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			tt.register(r, "/route", writeStatus(http.StatusNoContent))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tt.method, "/route", nil))

			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestRouterSetFallbackHandlers(t *testing.T) {
	r := New()
	r.SetNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot)))
	r.SetMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict)))
	r.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusConflict)
}

func TestRouterSetFallbackHandlersIgnoreNil(t *testing.T) {
	r := New()
	r.SetNotFound(nil)
	r.SetMethodNotAllowed(nil)
	r.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	assertStatus(t, notFound, http.StatusNotFound)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusMethodNotAllowed)
}

func TestTryHandleReturnsMatchErrors(t *testing.T) {
	r := New()
	if err := r.TryHandle(http.MethodGet, "/users/{}", writeStatus(http.StatusNoContent)); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("TryHandle invalid param error = %v, want ErrInvalidParam", err)
	}

	if err := r.TryHandle(http.MethodGet, "/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("TryHandle valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.TryHandle(http.MethodGet, "/users/{name}", writeStatus(http.StatusNoContent)); !errors.As(err, &conflict) {
		t.Fatalf("TryHandle conflict error = %v, want *match.ConflictError", err)
	}
}

func TestTryHandleRejectsInvalidMethods(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{
			name:   "empty",
			method: "",
		},
		{
			name:   "whitespace",
			method: " ",
		},
		{
			name:   "space in token",
			method: "BAD METHOD",
		},
		{
			name:   "separator in token",
			method: "BAD/METHOD",
		},
		{
			name:   "non ascii",
			method: "METH\xffD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			if err := r.TryHandle(tt.method, "/resource", writeStatus(http.StatusNoContent)); !errors.Is(err, ErrInvalidMethod) {
				t.Fatalf("TryHandle invalid method error = %v, want ErrInvalidMethod", err)
			}
			if r.hasRoutes {
				t.Fatal("router hasRoutes = true after failed registration, want false")
			}
		})
	}
}

func TestTryHandleLowercaseMethodsAreCaseSensitiveTokens(t *testing.T) {
	r := New()
	if err := r.TryHandle("get", "/resource", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("TryHandle lowercase method error = %v", err)
	}

	lower := httptest.NewRecorder()
	r.ServeHTTP(lower, httptest.NewRequest("get", "/resource", nil))
	assertStatus(t, lower, http.StatusNoContent)

	upper := httptest.NewRecorder()
	r.ServeHTTP(upper, httptest.NewRequest(http.MethodGet, "/resource", nil))
	assertStatus(t, upper, http.StatusMethodNotAllowed)
	if got, want := upper.Header().Get("Allow"), "get"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestTryHandleAcceptsValidExtensionMethods(t *testing.T) {
	tests := []string{
		"PROPFIND",
		"M-SEARCH",
		"X_ARC.1+JSON",
		"X!#$%&'*+-.^_`|~",
	}

	for _, method := range tests {
		t.Run(method, func(t *testing.T) {
			r := New()
			if err := r.TryHandle(method, "/resource", writeStatus(http.StatusNoContent)); err != nil {
				t.Fatalf("TryHandle extension method error = %v", err)
			}

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(method, "/resource", nil))
			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestPathRegistrationsRejectNonAbsolutePatterns(t *testing.T) {
	handler := writeStatus(http.StatusNoContent)
	tests := []struct {
		name     string
		register func(*Router) error
	}{
		{
			name: "route relative",
			register: func(r *Router) error {
				return r.TryHandleAll("users/{id}", handler)
			},
		},
		{
			name: "route empty",
			register: func(r *Router) error {
				return r.TryHandleAll("", handler)
			},
		},
		{
			name: "method route relative",
			register: func(r *Router) error {
				return r.TryHandle(http.MethodGet, "users/{id}", handler)
			},
		},
		{
			name: "subrouter relative",
			register: func(r *Router) error {
				_, err := r.TrySubRouter("api")
				return err
			},
		},
		{
			name: "mount relative",
			register: func(r *Router) error {
				return r.TryMount("assets", handler)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			if err := tt.register(r); !errors.Is(err, ErrInvalidPathPattern) {
				t.Fatalf("registration error = %v, want ErrInvalidPathPattern", err)
			}
			if r.hasRoutes {
				t.Fatal("router hasRoutes = true after failed registration, want false")
			}
			if r.hasSubRouters {
				t.Fatal("router hasSubRouters = true after failed registration, want false")
			}
		})
	}
}

func TestRegistrationRejectsDuplicateParamNames(t *testing.T) {
	handler := writeStatus(http.StatusNoContent)
	tests := []struct {
		name     string
		register func(*Router) error
	}{
		{
			name: "route",
			register: func(r *Router) error {
				return r.TryHandleAll("/{id}/{id}", handler)
			},
		},
		{
			name: "route catch-all",
			register: func(r *Router) error {
				return r.TryHandleAll("/{id}/{*id}", handler)
			},
		},
		{
			name: "route many params",
			register: func(r *Router) error {
				return r.TryHandleAll("/{a}/{b}/{c}/{d}/{e}/{a}", handler)
			},
		},
		{
			name: "subrouter",
			register: func(r *Router) error {
				_, err := r.TrySubRouter("/{id}/{id}")
				return err
			},
		},
		{
			name: "mount",
			register: func(r *Router) error {
				return r.TryMount("/{id}/{id}", handler)
			},
		},
		{
			name: "host",
			register: func(r *Router) error {
				_, err := r.TryHost("{id}.{id}.example.com")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.register(New())
			if !errors.Is(err, ErrDuplicateParamName) {
				t.Fatalf("registration error = %v, want ErrDuplicateParamName", err)
			}
			if !errors.Is(err, match.ErrInvalidParam) {
				t.Fatalf("registration error = %v, want ErrInvalidParam", err)
			}
		})
	}
}

func TestTryHandleDoesNotPartiallyRegisterFailedRoute(t *testing.T) {
	r := New()
	if err := r.TryHandle(http.MethodGet, "/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("TryHandle valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.TryHandle(http.MethodPost, "/users/{name}", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("TryHandle conflict error = %v, want *match.ConflictError", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestTryHandleDuplicateDoesNotCorruptRouteTables(t *testing.T) {
	r := New()
	if err := r.TryHandle(http.MethodGet, "/resource", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("TryHandle valid route error = %v", err)
	}
	if err := r.TryHandle(http.MethodGet, "/resource", writeStatus(http.StatusAccepted)); err == nil {
		t.Fatal("TryHandle duplicate route error = nil, want error")
	}
	if err := r.TryHandle(http.MethodPost, "/resource", writeStatus(http.StatusCreated)); err != nil {
		t.Fatalf("TryHandle post route error = %v", err)
	}

	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/resource", nil))
	assertStatus(t, get, http.StatusNoContent)

	put := httptest.NewRecorder()
	r.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/resource", nil))
	assertStatus(t, put, http.StatusMethodNotAllowed)
	if got, want := put.Header().Get("Allow"), "GET, HEAD, POST"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestHandlePanicsForInvalidPattern(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Handle did not panic")
		}
	}()

	New().Handle(http.MethodGet, "/users/{}", writeStatus(http.StatusNoContent))
}

func TestNilHandlerUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.Handle(http.MethodGet, "/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestNilHandleAllUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.HandleAll("/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestNilTryHandleAllUsesNotFoundHandler(t *testing.T) {
	r := New()
	if err := r.TryHandleAll("/nil", nil); err != nil {
		t.Fatalf("TryHandleAll route error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestNilMethodHelperUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.Get("/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestMiddlewareOrder(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("a", &calls), namedMiddleware("b", &calls))
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"a before", "b before", "handler", "b after", "a after"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestUsePanicsForNilMiddleware(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Use did not panic")
		}
	}()

	New().Use(nil)
}

func TestMiddlewareAppliesOnlyToLaterRegistrations(t *testing.T) {
	var calls []string
	r := New()
	r.Get("/before", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "before handler")
	})
	r.Use(namedMiddleware("mw", &calls))
	r.Get("/after", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "after handler")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/before", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/after", nil))

	want := []string{"before handler", "mw before", "after handler", "mw after"}
	assertStrings(t, calls, want)
}

func TestFallbacksRunMiddleware(t *testing.T) {
	var calls []string
	r := New()
	r.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "not found")
		w.WriteHeader(http.StatusTeapot)
	}))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "method not allowed")
		w.WriteHeader(http.StatusConflict)
	}))
	r.Use(namedMiddleware("mw", &calls))
	r.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))

	assertStatus(t, notFound, http.StatusTeapot)
	assertStrings(t, calls, []string{"mw before", "not found", "mw after"})

	calls = nil
	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/known", nil))

	assertStatus(t, methodNotAllowed, http.StatusConflict)
	assertStrings(t, calls, []string{"mw before", "method not allowed", "mw after"})
}

func TestSubRouterMatchesAndMergesParams(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "v1")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestSubRouterMountParamCanUseFormerRestName(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{__arc_rest}")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("__arc_rest"); got != "v1" {
			t.Fatalf("PathValue(__arc_rest) = %q, want %q", got, "v1")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterRegistrationEnablesSubRouterMatching(t *testing.T) {
	r := New()
	if r.hasSubRouters {
		t.Fatal("new router hasSubRouters = true, want false")
	}

	api := r.SubRouter("/api")
	if !r.hasSubRouters {
		t.Fatal("router hasSubRouters = false, want true after SubRouter")
	}

	api.Get("/", writeStatus(http.StatusCreated))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))

	assertStatus(t, rec, http.StatusCreated)
}

func TestSubRouterRootPaths(t *testing.T) {
	tests := []string{"/api", "/api/"}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			r := New()
			api := r.SubRouter("/api")
			api.Get("/", writeStatus(http.StatusCreated))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			assertStatus(t, rec, http.StatusCreated)
		})
	}
}

func TestSubRouterDoesNotMatchPartialSegment(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apix", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestSubRouterInheritsStrictSlashSetting(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	api := r.SubRouter("/api")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterSnapshotsStrictSlashSetting(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	r.SetStrictSlash(false)
	api.Get("/users/{id}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42/", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestSubRouterCanConfigureStrictSlashIndependently(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	api := r.SubRouter("/api")
	api.SetStrictSlash(true)
	api.Get("/users/{id}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42/", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestSubRouterPreservesQuery(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/search", func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Query().Get("q"); got != "arc" {
			t.Fatalf("query q = %q, want %q", got, "arc")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=arc", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterMountSkipsOnlyMountSeparator(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/users/{id}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api//users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterReturnsChildNotFoundAndMethodNotAllowed(t *testing.T) {
	r := New()
	r.SetNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot)))
	r.SetMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict)))
	api := r.SubRouter("/api")
	api.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusConflict)
}

func TestParentRouteWinsOverSubRouterUnderSamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		register func(*Router)
	}{
		{
			name: "subrouter first",
			register: func(r *Router) {
				api := r.SubRouter("/api")
				api.SetNotFound(http.HandlerFunc(writeStatus(http.StatusGone)))
				r.Get("/api/healthz", writeStatus(http.StatusNoContent))
			},
		},
		{
			name: "route first",
			register: func(r *Router) {
				r.Get("/api/healthz", writeStatus(http.StatusNoContent))
				api := r.SubRouter("/api")
				api.SetNotFound(http.HandlerFunc(writeStatus(http.StatusGone)))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			tt.register(r)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))

			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestParentParamRouteWinsOverSubRouterUnderSamePrefix(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/{id}", writeStatus(http.StatusGone))
	r.Get("/api/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/42", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRoutePrefixDoesNotBlockShorterSubRouter(t *testing.T) {
	r := New()
	root := r.SubRouter("/")
	root.Get("/api/users", writeStatus(http.StatusAccepted))
	r.Get("/api", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterSnapshotsFallbackHandlers(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/known", writeStatus(http.StatusNoContent))
	r.SetNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot)))
	r.SetMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict)))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	assertStatus(t, notFound, http.StatusNotFound)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusMethodNotAllowed)
}

func TestSubRouterCanConfigureFallbackHandlers(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.SetNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot)))
	api.SetMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict)))
	api.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusConflict)
}

func TestSubRouterFallbacksReceiveMergedParams(t *testing.T) {
	r := New()
	r.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1" {
			t.Fatalf("not found PathValue(version) = %q, want %q", got, "v1")
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1" {
			t.Fatalf("method not allowed PathValue(version) = %q, want %q", got, "v1")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("method not allowed PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusConflict)
	}))
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/v1/users/42", nil))
	assertStatus(t, methodNotAllowed, http.StatusConflict)
}

func TestRootSubRouterMatchesAllPaths(t *testing.T) {
	r := New()
	root := r.SubRouter("/")
	root.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterEmptyMountMatchesRoot(t *testing.T) {
	r := New()
	root := r.SubRouter("")
	root.Get("/healthz", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestSubRouterTrailingSlashMountIsCleaned(t *testing.T) {
	r := New()
	api := r.SubRouter("/api///")
	api.Get("/users/{id}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterRunsParentAndChildMiddleware(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("parent", &calls))

	api := r.SubRouter("/api")
	api.Use(namedMiddleware("child", &calls))
	api.Get("/", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api", nil))

	want := []string{
		"parent before",
		"child before",
		"handler",
		"child after",
		"parent after",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestSubRouterFallbacksRunParentAndChildMiddleware(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("parent", &calls))

	api := r.SubRouter("/api")
	api.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "not found")
		w.WriteHeader(http.StatusTeapot)
	}))
	api.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "method not allowed")
		w.WriteHeader(http.StatusConflict)
	}))
	api.Use(namedMiddleware("child", &calls))
	api.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/missing", nil))

	assertStatus(t, notFound, http.StatusTeapot)
	assertStrings(t, calls, []string{
		"parent before",
		"child before",
		"not found",
		"child after",
		"parent after",
	})

	calls = nil
	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/known", nil))

	assertStatus(t, methodNotAllowed, http.StatusConflict)
	assertStrings(t, calls, []string{
		"parent before",
		"child before",
		"method not allowed",
		"child after",
		"parent after",
	})
}

func TestSubRouterSnapshotsParentMiddlewareAtRegistration(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("before", &calls))
	api := r.SubRouter("/api")
	r.Use(namedMiddleware("after", &calls))
	api.Get("/", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api", nil))

	assertStrings(t, calls, []string{"before before", "handler", "before after"})
}

func TestSubRouterDoesNotRewriteRequestPath(t *testing.T) {
	var paths []string
	r := New()
	r.Use(pathMiddleware("parent", &paths))

	api := r.SubRouter("/api")
	api.Use(pathMiddleware("child", &paths))
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, "handler:"+req.URL.Path)
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
	assertStrings(t, paths, []string{
		"parent:/api/users/42",
		"child:/api/users/42",
		"handler:/api/users/42",
	})
}

func TestNestedSubRoutersMergeParams(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	accounts := api.SubRouter("/accounts/{account}")
	accounts.Get("/users/{user}", func(w http.ResponseWriter, req *http.Request) {
		want := map[string]string{
			"version": "v1",
			"account": "acme",
			"user":    "42",
		}
		for key, val := range want {
			if got := req.PathValue(key); got != val {
				t.Fatalf("PathValue(%s) = %q, want %q", key, got, val)
			}
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acme/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterMatchesEscapedSlashWithinSegment(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1/beta" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "v1/beta")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1%2Fbeta/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterMatchesEscapedSlashWithDecodedStaticSegment(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Get("/meta data", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1/beta" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "v1/beta")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1%2Fbeta/meta%20data", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterNormalizesEscapedSlashStaticPattern(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/v1%2Fbeta")
	api.Get("/meta data", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1%2Fbeta/meta%20data", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterNormalizesPercentEncodedStaticPattern(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/meta%20data")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta%20data/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterChildRouteEnablesEscapedSlashMatching(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("name"); got != "a/b" {
			t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/files/a%2Fb", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterCatchAllMount(t *testing.T) {
	r := New()
	files := r.SubRouter("/files/{*path}")
	files.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("path"); got != "css/app.css" {
			t.Fatalf("PathValue(path) = %q, want %q", got, "css/app.css")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/css/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterCatchAllMountRejectsEmptyRemainder(t *testing.T) {
	r := New()
	files := r.SubRouter("/files/{*path}")
	files.Get("/", writeStatus(http.StatusAccepted))

	for _, path := range []string{"/files", "/files/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			assertStatus(t, rec, http.StatusNotFound)
		})
	}
}

func TestSubRouterChoosesLongestMount(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	v1 := r.SubRouter("/api/v1")

	api.Get("/users/{id}", writeStatus(http.StatusAccepted))
	v1.Get("/users/{id}", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusCreated)
}

func TestSubRouterUsesStaticIndexAfterManyStaticMounts(t *testing.T) {
	r := New()
	for _, mount := range []string{"/a", "/b", "/c", "/d", "/e", "/f"} {
		sub := r.SubRouter(mount)
		sub.Get("/", writeStatus(http.StatusNoContent))
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/f", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestSubRouterMountWithSegmentAffixes(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/v{version}.json")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "1" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "1")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1.json/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterRejectsAmbiguousMounts(t *testing.T) {
	r := New()
	r.SubRouter("/api/{name}.json")

	defer func() {
		if recover() == nil {
			t.Fatal("SubRouter did not panic")
		}
	}()

	r.SubRouter("/api/v{version}.json")
}

func TestTrySubRouterReturnsMatchErrors(t *testing.T) {
	r := New()
	if child, err := r.TrySubRouter("/users/{}"); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("TrySubRouter invalid param child, error = %v, %v; want ErrInvalidParam", child, err)
	}
	if r.hasSubRouters {
		t.Fatal("router hasSubRouters = true after failed first TrySubRouter, want false")
	}

	api, err := r.TrySubRouter("/api/{name}.json")
	if err != nil {
		t.Fatalf("TrySubRouter valid mount error = %v", err)
	}
	api.Get("/", writeStatus(http.StatusAccepted))

	var conflict *match.ConflictError
	if child, err := r.TrySubRouter("/api/v{version}.json"); !errors.As(err, &conflict) {
		t.Fatalf("TrySubRouter conflict child, error = %v, %v; want *match.ConflictError", child, err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foo.json", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestFailedTrySubRouterDoesNotConfigureChild(t *testing.T) {
	r := New()
	configured := 0
	r.Use(func(next http.Handler) http.Handler {
		configured++
		return next
	})
	configured = 0

	if child, err := r.TrySubRouter("/users/{}"); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("TrySubRouter invalid param child, error = %v, %v; want ErrInvalidParam", child, err)
	}
	if configured != 0 {
		t.Fatalf("middleware configured %d times after failed TrySubRouter, want 0", configured)
	}
}

func TestTrySubRouterConflictDoesNotAttachExistingRouteEntry(t *testing.T) {
	r := New()
	r.HandleAll("/api/{name}.json", writeStatus(http.StatusAccepted))
	r.HandleAll("/api/v{version}.json/", writeStatus(http.StatusNoContent))

	var conflict *match.ConflictError
	if child, err := r.TrySubRouter("/api/{name}.json"); !errors.As(err, &conflict) {
		t.Fatalf("TrySubRouter conflict child, error = %v, %v; want *match.ConflictError", child, err)
	}
	if r.pathEntries["/api/{name}.json"].child != nil {
		t.Fatal("TrySubRouter attached child to existing route entry after failed registration")
	}
	if r.hasSubRouters {
		t.Fatal("router hasSubRouters = true after failed TrySubRouter, want false")
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foo.json", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterBacktracksAcrossParamMounts(t *testing.T) {
	r := New()
	foo := r.SubRouter("/{section}/foo")
	bar := r.SubRouter("/{section}/bar")

	foo.Get("/", writeStatus(http.StatusAccepted))
	bar.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("section"); got != "api" {
			t.Fatalf("PathValue(section) = %q, want %q", got, "api")
		}
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/bar", nil))

	assertStatus(t, rec, http.StatusCreated)
}

func TestSubRouterBacktracksAcrossDifferentlyNamedParamMounts(t *testing.T) {
	r := New()
	foo := r.SubRouter("/{id}/foo")
	bar := r.SubRouter("/{name}/bar")

	foo.Get("/", writeStatus(http.StatusAccepted))
	bar.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("name"); got != "abc" {
			t.Fatalf("PathValue(name) = %q, want %q", got, "abc")
		}
		if got := req.PathValue("id"); got != "" {
			t.Fatalf("PathValue(id) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/abc/bar", nil))

	assertStatus(t, rec, http.StatusCreated)
}

func TestMountDispatchesHandlerWithRemainingPathAndParams(t *testing.T) {
	r := New()
	r.Mount("/tenants/{tenant}/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Path; got != "/css/app.css" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/css/app.css")
		}
		if got := req.URL.Query().Get("v"); got != "1" {
			t.Fatalf("query v = %q, want %q", got, "1")
		}
		if got := req.PathValue("tenant"); got != "acme" {
			t.Fatalf("PathValue(tenant) = %q, want %q", got, "acme")
		}
		if got := req.PathValue("tenant"); got != "acme" {
			t.Fatalf("req.PathValue(tenant) = %q, want %q", got, "acme")
		}
		if got := req.Pattern; got != "/tenants/{tenant}/assets" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/tenants/{tenant}/assets")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tenants/acme/assets/css/app.css?v=1", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountMatchesEscapedSlashWithinSegment(t *testing.T) {
	r := New()
	r.Mount("/assets/{name}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("name"); got != "a/b" {
			t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
		}
		if got := req.URL.Path; got != "/app.css" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/app.css")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/a%2Fb/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountNormalizesEscapedSlashStaticPattern(t *testing.T) {
	r := New()
	r.Mount("/assets/a%2Fb", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Path; got != "/meta data" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/meta data")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/a%2Fb/meta%20data", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountNormalizesPercentEncodedStaticPattern(t *testing.T) {
	r := New()
	r.Mount("/assets/meta%20data", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Path; got != "/app.css" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/app.css")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/meta%20data/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountPatternIncludesSubRouterPatterns(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Mount("/tenants/{tenant}/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/api/{version}/tenants/{tenant}/assets" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/api/{version}/tenants/{tenant}/assets")
		}
		if got := req.URL.Path; got != "/css/app.css" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/css/app.css")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/assets/css/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountArcRouterPreservesMountParamsWithInnerRouteParams(t *testing.T) {
	r := New()
	inner := New()
	inner.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Path; got != "/users/42" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/users/42")
		}
		if got := req.PathValue("version"); got != "v1" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "v1")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Mount("/api/{version}", inner)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountArcRouterPreservesEscapedSlashRawPath(t *testing.T) {
	r := New()
	inner := New()
	inner.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.URL.Path; got != "/files/a/b" {
			t.Fatalf("req.URL.Path = %q, want %q", got, "/files/a/b")
		}
		if got := req.URL.RawPath; got != "/files/a%2Fb" {
			t.Fatalf("req.URL.RawPath = %q, want %q", got, "/files/a%2Fb")
		}
		if got := req.PathValue("name"); got != "a/b" {
			t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Mount("/api", inner)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/files/a%2Fb", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountRootPaths(t *testing.T) {
	for _, path := range []string{"/assets", "/assets/"} {
		t.Run(path, func(t *testing.T) {
			r := New()
			r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if got := req.URL.Path; got != "/" {
					t.Fatalf("req.URL.Path = %q, want /", got)
				}
				w.WriteHeader(http.StatusCreated)
			}))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			assertStatus(t, rec, http.StatusCreated)
		})
	}
}

func TestMountDoesNotMatchPartialSegment(t *testing.T) {
	r := New()
	r.Mount("/assets", writeStatus(http.StatusCreated))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assetsx/app.css", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestMountRunsParentMiddlewareBeforeRewritingPath(t *testing.T) {
	var paths []string
	r := New()
	r.Use(pathMiddleware("parent", &paths))
	r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, "handler:"+req.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))

	assertStatus(t, rec, http.StatusAccepted)
	assertStrings(t, paths, []string{
		"parent:/assets/app.css",
		"handler:/app.css",
	})
}

func TestMountSnapshotsParentMiddlewareAtRegistration(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("before", &calls))
	r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	}))
	r.Use(namedMiddleware("after", &calls))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))

	assertStrings(t, calls, []string{"before before", "handler", "before after"})
}

func TestMountNilHandlerUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.Mount("/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestTryMountReturnsMatchErrors(t *testing.T) {
	r := New()
	if err := r.TryMount("/users/{}", writeStatus(http.StatusNoContent)); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("TryMount invalid param error = %v, want ErrInvalidParam", err)
	}
	if r.hasSubRouters {
		t.Fatal("router hasSubRouters = true after failed first TryMount, want false")
	}

	if err := r.TryMount("/api/{name}.json", writeStatus(http.StatusAccepted)); err != nil {
		t.Fatalf("TryMount valid mount error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.TryMount("/api/v{version}.json", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("TryMount conflict error = %v, want *match.ConflictError", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foo.json", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountConflictsWithSubRouter(t *testing.T) {
	r := New()
	r.SubRouter("/api/{name}.json")

	var conflict *match.ConflictError
	if err := r.TryMount("/api/v{version}.json", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("TryMount conflict error = %v, want *match.ConflictError", err)
	}
}

func TestParentRouteWinsOverMountUnderSamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		register func(*Router)
	}{
		{
			name: "mount first",
			register: func(r *Router) {
				r.Mount("/api", writeStatus(http.StatusGone))
				r.Get("/api/healthz", writeStatus(http.StatusNoContent))
			},
		},
		{
			name: "route first",
			register: func(r *Router) {
				r.Get("/api/healthz", writeStatus(http.StatusNoContent))
				r.Mount("/api", writeStatus(http.StatusGone))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New()
			tt.register(r)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))

			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestHostRouterMatchesAndMergesParams(t *testing.T) {
	r := New()
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("tenant"); got != "acme" {
			t.Fatalf("PathValue(tenant) = %q, want %q", got, "acme")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "http://acme.example.com/users/42", nil)
	req.Host = "ACME.example.com:8080"

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestHostRouterParamsCaptureOneDNSLabel(t *testing.T) {
	r := New()
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/", writeStatus(http.StatusNoContent))
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://a.b.example.com/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterMatchesMultipleParameterizedLabels(t *testing.T) {
	r := New()
	host := r.Host("{region}.{tenant}.example.com")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("region"); got != "us-west" {
			t.Fatalf("PathValue(region) = %q, want %q", got, "us-west")
		}
		if got := req.PathValue("tenant"); got != "acme" {
			t.Fatalf("PathValue(tenant) = %q, want %q", got, "acme")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://us-west.acme.example.com/", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterMatchesAffixedParameterizedLabel(t *testing.T) {
	r := New()
	host := r.Host("api-{region}.example.com")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("region"); got != "us-west" {
			t.Fatalf("PathValue(region) = %q, want %q", got, "us-west")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api-us-west.example.com/", nil))
	assertStatus(t, rec, http.StatusNoContent)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://web-us-west.example.com/", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterCatchAllCapturesLeadingLabels(t *testing.T) {
	r := New()
	host := r.Host("{*subdomain}.example.com")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("subdomain"); got != "api.us-west" {
			t.Fatalf("PathValue(subdomain) = %q, want %q", got, "api.us-west")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.us-west.example.com/", nil))
	assertStatus(t, rec, http.StatusNoContent)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterCatchAllCanCaptureWholeHost(t *testing.T) {
	r := New()
	host := r.Host("{*host}")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("host"); got != "api.example.com" {
			t.Fatalf("PathValue(host) = %q, want %q", got, "api.example.com")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterMatchesPrefixedCatchAll(t *testing.T) {
	r := New()
	host := r.Host("api-{*subdomain}.example.com")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("subdomain"); got != "us-west.internal" {
			t.Fatalf("PathValue(subdomain) = %q, want %q", got, "us-west.internal")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api-us-west.internal.example.com/", nil))
	assertStatus(t, rec, http.StatusNoContent)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://web-us-west.internal.example.com/", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterSharesParamBranchAcrossDisjointHosts(t *testing.T) {
	r := New()
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/", writeStatus(http.StatusAccepted))
	account := r.Host("{account}.api.com")
	account.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("account"); got != "acme" {
			t.Fatalf("PathValue(account) = %q, want %q", got, "acme")
		}
		if got := req.PathValue("tenant"); got != "" {
			t.Fatalf("PathValue(tenant) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://acme.api.com/", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterDiscardsParamsFromFailedStaticBranch(t *testing.T) {
	r := New()
	deeper := r.Host("api.{unused}.internal.example.com")
	deeper.Get("/", writeStatus(http.StatusAccepted))
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("tenant"); got != "api" {
			t.Fatalf("PathValue(tenant) = %q, want %q", got, "api")
		}
		if got := req.PathValue("unused"); got != "" {
			t.Fatalf("PathValue(unused) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterNormalizesTrailingDot(t *testing.T) {
	r := New()
	api := r.Host("api.example.com.")
	api.Get("/", writeStatus(http.StatusNoContent))

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil)
	req.Host = "API.example.com."
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterNormalizesIDNA(t *testing.T) {
	r := New()
	host := r.Host("bücher.example.com")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.Host; got != "XN--BCHER-KVA.example.com" {
			t.Fatalf("req.Host = %q, want original Host header", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://xn--bcher-kva.example.com/", nil)
	req.Host = "XN--BCHER-KVA.example.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusNoContent)
}

func TestHostRouterPreservesParamNameCase(t *testing.T) {
	r := New()
	tenant := r.Host("{Tenant}.Example.COM")
	tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("Tenant"); got != "acme" {
			t.Fatalf("PathValue(Tenant) = %q, want %q", got, "acme")
		}
		if got := req.PathValue("tenant"); got != "" {
			t.Fatalf("PathValue(tenant) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "http://acme.example.com/", nil)
	req.Host = "ACME.EXAMPLE.COM"

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestHostRouterChildRouteEnablesEscapedSlashMatching(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	api.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("name"); got != "a/b" {
			t.Fatalf("PathValue(name) = %q, want %q", got, "a/b")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/files/a%2Fb", nil)
	req.Host = "api.example.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterPrefersStaticHost(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	api.Get("/", writeStatus(http.StatusAccepted))
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterPrefersStaticHostOverCatchAll(t *testing.T) {
	r := New()
	wildcard := r.Host("{*subdomain}.example.com")
	wildcard.Get("/", writeStatus(http.StatusNoContent))
	www := r.Host("www.example.com")
	www.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://www.example.com/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestTryHostReturnsMatchErrors(t *testing.T) {
	r := New()
	if child, err := r.TryHost("{}.example.com"); !errors.Is(err, ErrInvalidHostPattern) {
		t.Fatalf("TryHost invalid param child, error = %v, %v; want ErrInvalidHostPattern", child, err)
	}
	if r.hasHosts {
		t.Fatal("router hasHosts = true after failed first TryHost, want false")
	}

	api, err := r.TryHost("api.example.com")
	if err != nil {
		t.Fatalf("TryHost valid host error = %v", err)
	}
	api.Get("/", writeStatus(http.StatusAccepted))

	var conflict *match.ConflictError
	if child, err := r.TryHost("api.example.com"); !errors.As(err, &conflict) {
		t.Fatalf("TryHost duplicate child, error = %v, %v; want *match.ConflictError", child, err)
	}

	tenant, err := r.TryHost("{tenant}.example.com")
	if err != nil {
		t.Fatalf("TryHost dynamic host error = %v", err)
	}
	tenant.Get("/tenant", writeStatus(http.StatusNoContent))
	if child, err := r.TryHost("{account}.example.com"); !errors.As(err, &conflict) {
		t.Fatalf("TryHost duplicate dynamic child, error = %v, %v; want *match.ConflictError", child, err)
	}
	if child, err := r.TryHost("{*subdomain}.example.com"); !errors.As(err, &conflict) {
		t.Fatalf("TryHost catch-all conflict child, error = %v, %v; want *match.ConflictError", child, err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestFailedTryHostDoesNotConfigureChild(t *testing.T) {
	r := New()
	configured := 0
	r.Use(func(next http.Handler) http.Handler {
		configured++
		return next
	})
	configured = 0

	if child, err := r.TryHost("{}.example.com"); !errors.Is(err, ErrInvalidHostPattern) {
		t.Fatalf("TryHost invalid param child, error = %v, %v; want ErrInvalidHostPattern", child, err)
	}
	if configured != 0 {
		t.Fatalf("middleware configured %d times after failed TryHost, want 0", configured)
	}
}

func TestTryHostRejectsInvalidHostPatterns(t *testing.T) {
	tests := []string{
		"",
		".",
		":80",
		"api..example.com",
		".example.com",
		"bad_host.example.com",
		"-bad.example.com",
		"bad-.example.com",
		"{tenant}{region}.example.com",
		"api.{*tenant}.example.com",
		"api-{*tenant}x.example.com",
		"api.example.com:8080",
		"api.example.com:http",
	}

	for _, pattern := range tests {
		t.Run(pattern, func(t *testing.T) {
			r := New()
			child, err := r.TryHost(pattern)
			if !errors.Is(err, ErrInvalidHostPattern) {
				t.Fatalf("TryHost(%q) child, error = %v, %v; want ErrInvalidHostPattern", pattern, child, err)
			}
			if child != nil {
				t.Fatalf("TryHost(%q) child = %v, want nil", pattern, child)
			}
			if r.hasHosts {
				t.Fatalf("TryHost(%q) set hasHosts = true, want false", pattern)
			}
		})
	}
}

func TestHostRouterFallsThroughToRootWhenHostDoesNotMatch(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	api.Get("/", writeStatus(http.StatusNoContent))
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://www.example.com/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterFallsThroughToRootWhenHostIsEmpty(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	api.Get("/", writeStatus(http.StatusNoContent))
	r.Get("/", writeStatus(http.StatusAccepted))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterNormalizesIPv6HostWithPort(t *testing.T) {
	r := New()
	local := r.Host("::1")
	local.Get("/", writeStatus(http.StatusAccepted))

	req := httptest.NewRequest(http.MethodGet, "http://[::1]/", nil)
	req.Host = "[::1]:8080"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterNormalizesColonFormPatternCase(t *testing.T) {
	r := New()
	local := r.Host("FE80::1")
	local.Get("/", writeStatus(http.StatusAccepted))

	req := httptest.NewRequest(http.MethodGet, "http://[fe80::1]/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterNormalizesBracketedIPv6HostWithoutPort(t *testing.T) {
	r := New()
	local := r.Host("::1")
	local.Get("/", writeStatus(http.StatusAccepted))

	req := httptest.NewRequest(http.MethodGet, "http://[::1]/", nil)
	req.Host = "[::1]"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterParamCapturesIPv6Literal(t *testing.T) {
	r := New()
	host := r.Host("{host}")
	host.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("host"); got != "::1" {
			t.Fatalf("host param = %q, want ::1", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://[::1]/", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestNormalizeHostPatternLowercasesColonForm(t *testing.T) {
	got, err := normalizeHostPattern("FE80::1")
	if err != nil {
		t.Fatalf("normalizeHostPattern(%q) error = %v", "FE80::1", err)
	}
	if got != "fe80::1" {
		t.Fatalf("normalizeHostPattern(%q) = %q, want %q", "FE80::1", got, "fe80::1")
	}
}

func TestNormalizeRequestHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "lowercase_ascii",
			host: "api.example.com",
			want: "api.example.com",
		},
		{
			name: "uppercase_ascii",
			host: "API.example.com",
			want: "api.example.com",
		},
		{
			name: "trailing_dot",
			host: "api.example.com.",
			want: "api.example.com",
		},
		{
			name: "port",
			host: "api.example.com:8080",
			want: "api.example.com",
		},
		{
			name: "trailing_dot_with_port",
			host: "api.example.com.:8080",
			want: "api.example.com",
		},
		{
			name: "invalid_port",
			host: "api.example.com:http",
			want: "",
		},
		{
			name: "bracketed_ipv6",
			host: "[::1]",
			want: "::1",
		},
		{
			name: "unicode",
			host: "BÜCHER.example.com.",
			want: "xn--bcher-kva.example.com",
		},
		{
			name: "uppercase_punycode",
			host: "XN--BCHER-KVA.example.com",
			want: "xn--bcher-kva.example.com",
		},
		{
			name: "invalid_dns_name",
			host: "bad_host.example.com",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRequestHost(tt.host); got != tt.want {
				t.Fatalf("normalizeRequestHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestNormalizeRequestHostLowercaseASCIIFastPathAllocatesZero(t *testing.T) {
	host := "api.example.com"

	allocs := testing.AllocsPerRun(1000, func() {
		normalizeHostSink = normalizeRequestHost(host)
	})

	if normalizeHostSink != host {
		t.Fatalf("normalizeRequestHost(%q) = %q, want %q", host, normalizeHostSink, host)
	}
	if allocs != 0 {
		t.Fatalf("normalizeRequestHost(%q) allocated %v times, want 0", host, allocs)
	}
}

func TestHostRouterInheritsStrictSlashSetting(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	api := r.Host("api.example.com")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/users/42/", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterSnapshotsStrictSlashSetting(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	r.SetStrictSlash(false)
	api.Get("/users/{id}", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/users/42/", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestHostRouterRunsParentAndChildMiddleware(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("parent", &calls))

	api := r.Host("api.example.com")
	api.Use(namedMiddleware("child", &calls))
	api.Get("/", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	})

	req := httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	want := []string{
		"parent before",
		"child before",
		"handler",
		"child after",
		"parent after",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestHostRouterFallbacksRunParentAndChildMiddleware(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("parent", &calls))

	api := r.Host("api.example.com")
	api.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "not found")
		w.WriteHeader(http.StatusTeapot)
	}))
	api.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "method not allowed")
		w.WriteHeader(http.StatusConflict)
	}))
	api.Use(namedMiddleware("child", &calls))
	api.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "http://api.example.com/missing", nil))

	assertStatus(t, notFound, http.StatusTeapot)
	assertStrings(t, calls, []string{
		"parent before",
		"child before",
		"not found",
		"child after",
		"parent after",
	})

	calls = nil
	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "http://api.example.com/known", nil))

	assertStatus(t, methodNotAllowed, http.StatusConflict)
	assertStrings(t, calls, []string{
		"parent before",
		"child before",
		"method not allowed",
		"child after",
		"parent after",
	})
}

func TestHostRouterSnapshotsParentMiddlewareAtRegistration(t *testing.T) {
	var calls []string
	r := New()
	r.Use(namedMiddleware("before", &calls))
	api := r.Host("api.example.com")
	r.Use(namedMiddleware("after", &calls))
	api.Get("/", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))

	assertStrings(t, calls, []string{"before before", "handler", "before after"})
}

func TestRouteParamsAreRequestPathValues(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("req.PathValue(id) = %q, want %q", got, "42")
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
}

func TestRequestPatternSetForStaticRoute(t *testing.T) {
	r := New()
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/healthz" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/healthz")
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
}

func TestRequestPatternSetForParameterizedRoute(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/users/{id}" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/users/{id}")
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
}

func TestRequestPatternIncludesSubRouterPatterns(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	accounts := api.SubRouter("/accounts/{account}")
	accounts.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/api/{version}/accounts/{account}/users/{id}" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/api/{version}/accounts/{account}/users/{id}")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acme/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRequestPatternVisibleToRouteMiddleware(t *testing.T) {
	r := New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if got := req.Pattern; got != "/users/{id}" {
				t.Fatalf("middleware req.Pattern = %q, want %q", got, "/users/{id}")
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRequestPatternVisibleToSubRouterRouteMiddleware(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if got := req.Pattern; got != "/api/{version}/users/{id}" {
				t.Fatalf("middleware req.Pattern = %q, want %q", got, "/api/{version}/users/{id}")
			}
			next.ServeHTTP(w, req)
		})
	})
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRequestPatternVisibleToInheritedSubRouterMiddleware(t *testing.T) {
	r := New()
	parentCalled := false
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			parentCalled = true
			if got := req.Pattern; got != "/api/{version}/users/{id}" {
				t.Fatalf("parent middleware req.Pattern = %q, want %q", got, "/api/{version}/users/{id}")
			}
			if got := req.PathValue("version"); got != "v1" {
				t.Fatalf("parent middleware req.PathValue(version) = %q, want %q", got, "v1")
			}
			if got := req.PathValue("id"); got != "42" {
				t.Fatalf("parent middleware req.PathValue(id) = %q, want %q", got, "42")
			}
			next.ServeHTTP(w, req)
		})
	})
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
	if !parentCalled {
		t.Fatal("parent middleware was not called")
	}
}

func TestRequestPatternVisibleToInheritedHostRouterMiddleware(t *testing.T) {
	r := New()
	parentCalled := false
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			parentCalled = true
			if got := req.Pattern; got != "/users/{id}" {
				t.Fatalf("parent middleware req.Pattern = %q, want %q", got, "/users/{id}")
			}
			if got := req.PathValue("tenant"); got != "api" {
				t.Fatalf("parent middleware req.PathValue(tenant) = %q, want %q", got, "api")
			}
			if got := req.PathValue("id"); got != "42" {
				t.Fatalf("parent middleware req.PathValue(id) = %q, want %q", got, "42")
			}
			next.ServeHTTP(w, req)
		})
	})
	api := r.Host("{tenant}.example.com")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
	if !parentCalled {
		t.Fatal("parent middleware was not called")
	}
}

func TestRequestPatternVisibleToInheritedSubRouterMethodNotAllowedMiddleware(t *testing.T) {
	r := New()
	parentCalled := false
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			parentCalled = true
			if got := req.Pattern; got != "/api/{version}/users/{id}" {
				t.Fatalf("parent middleware req.Pattern = %q, want %q", got, "/api/{version}/users/{id}")
			}
			next.ServeHTTP(w, req)
		})
	})
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if !parentCalled {
		t.Fatal("parent middleware was not called")
	}
}

func TestRequestPatternSetForMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/users/{id}" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/users/{id}")
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
}

func TestRequestPatternIncludesSubRouterPatternsForMethodNotAllowed(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", writeStatus(http.StatusNoContent))
	called := false
	api.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		if got := req.Pattern; got != "/api/{version}/users/{id}" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/api/{version}/users/{id}")
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if !called {
		t.Fatal("method not allowed handler was not called")
	}
}

func TestRequestPatternEmptyForNotFound(t *testing.T) {
	r := New()
	r.Get("/users/{id}", writeStatus(http.StatusNoContent))
	r.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "" {
			t.Fatalf("req.Pattern = %q, want empty", got)
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req.Pattern = "/previous"
	r.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusNotFound)
}

func TestPathValueReturnsZeroValueWhenNoParams(t *testing.T) {
	r := New()
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("missing"); got != "" {
			t.Fatalf("PathValue(missing) = %q, want empty", got)
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestPathValueMergePrecedence(t *testing.T) {
	r := New()
	host := r.Host("{id}.example.com")
	sub := host.SubRouter("/{id}")
	sub.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "route" {
			t.Fatalf("req.PathValue(id) = %q, want %q", got, "route")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://host.example.com/sub/route", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRequestPathValueVisibleToSubRouterMiddleware(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if got := req.PathValue("version"); got != "v1" {
				t.Fatalf("req.PathValue(version) = %q, want %q", got, "v1")
			}
			next.ServeHTTP(w, req)
		})
	})
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("req.PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterDispatchSurvivesMiddlewareContextWrap(t *testing.T) {
	type contextKey struct{}

	r := New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), contextKey{}, "set")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	api := r.SubRouter("/api/{version}")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "v1" {
			t.Fatalf("PathValue(version) = %q, want %q", got, "v1")
		}
		if got := req.PathValue("id"); got != "42" {
			t.Fatalf("PathValue(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterServesConcurrentRequestsAfterRegistration(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var wg sync.WaitGroup
	statuses := make(chan int, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
			statuses <- rec.Code
		}()
	}
	wg.Wait()
	close(statuses)

	for got := range statuses {
		if got != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", got, http.StatusNoContent)
		}
	}
}

func writeStatus(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(status)
	}
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d", rec.Code, want)
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func namedMiddleware(name string, calls *[]string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			*calls = append(*calls, name+" before")
			next.ServeHTTP(w, req)
			*calls = append(*calls, name+" after")
		})
	}
}

func pathMiddleware(name string, paths *[]string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			*paths = append(*paths, name+":"+req.URL.Path)
			next.ServeHTTP(w, req)
		})
	}
}
