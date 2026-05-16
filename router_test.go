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

func TestRouterMatchesRouteAndParams(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRouterMatchesStaticBeforeParam(t *testing.T) {
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
		if got := Param(req, "path"); got != "css/app.css" {
			t.Fatalf("Param(path) = %q, want %q", got, "css/app.css")
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
		r.SetRequestPathValues(true)
		r.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
			if got := req.URL.Path; got != "/files/a/b" {
				t.Fatalf("req.URL.Path = %q, want %q", got, "/files/a/b")
			}
			if got := Param(req, "name"); got != "a/b" {
				t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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
			if got := Param(req, "path"); got != "a/b" {
				t.Fatalf("Param(path) = %q, want %q", got, "a/b")
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
			if got := Param(req, "name"); got != "a/b" {
				t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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
			if got := Param(req, "name"); got != "a/b" {
				t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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
			if got := Param(req, "name"); got != "a\x00b" {
				t.Fatalf("Param(name) = %q, want %q", got, "a\x00b")
			}
			if got := Param(req, "slug"); got != "c/d" {
				t.Fatalf("Param(slug) = %q, want %q", got, "c/d")
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

func TestRouterNormalizesEscapedSlashStaticPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
	}{
		{
			name:    "decoded space segment",
			pattern: "/files/a%2Fb/meta%20data",
			path:    "/files/a%2Fb/meta%20data",
		},
		{
			name:    "decoded literal braces segment",
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
		if got := Param(req, "query"); got != "what's up" {
			t.Fatalf("Param(query) = %q, want %q", got, "what's up")
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
		if got := Param(req, "query"); got != "what's up" {
			t.Fatalf("Param(query) = %q, want %q", got, "what's up")
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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

func TestRouterHandleMatchesAnyMethod(t *testing.T) {
	r := New()
	r.Handle("/resource", writeStatus(http.StatusNoContent))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(method, "/resource", nil))
			assertStatus(t, rec, http.StatusNoContent)
		})
	}
}

func TestRouterHandleUsesPathSpecificityBeforeMethod(t *testing.T) {
	r := New()
	r.Get("/users/{id}", writeStatus(http.StatusAccepted))
	r.Handle("/users/me", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))

	assertStatus(t, rec, http.StatusNoContent)
}

func TestRouterMethodRouteWinsOverAnyMethodForSamePattern(t *testing.T) {
	r := New()
	r.Handle("/resource", writeStatus(http.StatusNoContent))
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
	r.Handle("/resource", writeStatus(http.StatusNoContent))

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

func TestHandleErrReturnsMatchErrors(t *testing.T) {
	r := New()
	if err := r.HandleErr("/users/{}", writeStatus(http.StatusNoContent)); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("HandleErr invalid param error = %v, want ErrInvalidParam", err)
	}

	if err := r.HandleErr("/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("HandleErr valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.HandleErr("/users/{name}", writeStatus(http.StatusNoContent)); !errors.As(err, &conflict) {
		t.Fatalf("HandleErr conflict error = %v, want *match.ConflictError", err)
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
				return r.HandleErr("/{id}/{id}", handler)
			},
		},
		{
			name: "route catch-all",
			register: func(r *Router) error {
				return r.HandleErr("/{id}/{*id}", handler)
			},
		},
		{
			name: "subrouter",
			register: func(r *Router) error {
				_, err := r.SubRouterErr("/{id}/{id}")
				return err
			},
		},
		{
			name: "mount",
			register: func(r *Router) error {
				return r.MountErr("/{id}/{id}", handler)
			},
		},
		{
			name: "host",
			register: func(r *Router) error {
				_, err := r.HostErr("{id}/{id}.example.com")
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

func TestHandleMethodErrDoesNotPartiallyRegisterFailedRoute(t *testing.T) {
	r := New()
	if err := r.HandleMethodErr(http.MethodGet, "/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("HandleMethodErr valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.HandleMethodErr(http.MethodPost, "/users/{name}", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("HandleMethodErr conflict error = %v, want *match.ConflictError", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestHandleMethodErrDuplicateDoesNotCorruptRouteTables(t *testing.T) {
	r := New()
	if err := r.HandleMethodErr(http.MethodGet, "/resource", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("HandleMethodErr valid route error = %v", err)
	}
	if err := r.HandleMethodErr(http.MethodGet, "/resource", writeStatus(http.StatusAccepted)); err == nil {
		t.Fatal("HandleMethodErr duplicate route error = nil, want error")
	}
	if err := r.HandleMethodErr(http.MethodPost, "/resource", writeStatus(http.StatusCreated)); err != nil {
		t.Fatalf("HandleMethodErr post route error = %v", err)
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

	New().Handle("/users/{}", writeStatus(http.StatusNoContent))
}

func TestNilHandlerUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.Handle("/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestNilHandlerFuncUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.HandleFunc("/nil", nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nil", nil))

	assertStatus(t, rec, http.StatusNotFound)
}

func TestNilHandleMethodFuncUsesNotFoundHandler(t *testing.T) {
	r := New()
	r.HandleMethodFunc(http.MethodPost, "/nil", nil)

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
		if got := Param(req, "version"); got != "v1" {
			t.Fatalf("Param(version) = %q, want %q", got, "v1")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "__arc_rest"); got != "v1" {
			t.Fatalf("Param(__arc_rest) = %q, want %q", got, "v1")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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

func TestSubRouterShadowsParentRouteUnderSamePrefix(t *testing.T) {
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

			assertStatus(t, rec, http.StatusGone)
		})
	}
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
		if got := Param(req, "version"); got != "v1" {
			t.Fatalf("not found Param(version) = %q, want %q", got, "v1")
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "version"); got != "v1" {
			t.Fatalf("method not allowed Param(version) = %q, want %q", got, "v1")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("method not allowed Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
			if got := Param(req, key); got != val {
				t.Fatalf("Param(%s) = %q, want %q", key, got, val)
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
		if got := Param(req, "version"); got != "v1/beta" {
			t.Fatalf("Param(version) = %q, want %q", got, "v1/beta")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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
		if got := Param(req, "version"); got != "v1/beta" {
			t.Fatalf("Param(version) = %q, want %q", got, "v1/beta")
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

func TestSubRouterChildRouteEnablesEscapedSlashMatching(t *testing.T) {
	r := New()
	api := r.SubRouter("/api")
	api.Get("/files/{name}", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "name"); got != "a/b" {
			t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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
		if got := Param(req, "path"); got != "css/app.css" {
			t.Fatalf("Param(path) = %q, want %q", got, "css/app.css")
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
		if got := Param(req, "version"); got != "1" {
			t.Fatalf("Param(version) = %q, want %q", got, "1")
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

func TestSubRouterErrReturnsMatchErrors(t *testing.T) {
	r := New()
	if child, err := r.SubRouterErr("/users/{}"); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("SubRouterErr invalid param child, error = %v, %v; want ErrInvalidParam", child, err)
	}
	if r.hasSubRouters {
		t.Fatal("router hasSubRouters = true after failed first SubRouterErr, want false")
	}

	api, err := r.SubRouterErr("/api/{name}.json")
	if err != nil {
		t.Fatalf("SubRouterErr valid mount error = %v", err)
	}
	api.Get("/", writeStatus(http.StatusAccepted))

	var conflict *match.ConflictError
	if child, err := r.SubRouterErr("/api/v{version}.json"); !errors.As(err, &conflict) {
		t.Fatalf("SubRouterErr conflict child, error = %v, %v; want *match.ConflictError", child, err)
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
		if got := Param(req, "section"); got != "api" {
			t.Fatalf("Param(section) = %q, want %q", got, "api")
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
		if got := Param(req, "name"); got != "abc" {
			t.Fatalf("Param(name) = %q, want %q", got, "abc")
		}
		if got := Param(req, "id"); got != "" {
			t.Fatalf("Param(id) = %q, want empty", got)
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
		if got := Param(req, "tenant"); got != "acme" {
			t.Fatalf("Param(tenant) = %q, want %q", got, "acme")
		}
		if got := req.PathValue("tenant"); got != "acme" {
			t.Fatalf("req.PathValue(tenant) = %q, want %q", got, "acme")
		}
		if got := req.Pattern; got != "/tenants/{tenant}/assets" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/tenants/{tenant}/assets")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	r.SetRequestPathValues(true)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tenants/acme/assets/css/app.css?v=1", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountMatchesEscapedSlashWithinSegment(t *testing.T) {
	r := New()
	r.Mount("/assets/{name}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "name"); got != "a/b" {
			t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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
		if got := Param(req, "version"); got != "v1" {
			t.Fatalf("Param(version) = %q, want %q", got, "v1")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})
	r.Mount("/api/{version}", inner)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

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

func TestMountErrReturnsMatchErrors(t *testing.T) {
	r := New()
	if err := r.MountErr("/users/{}", writeStatus(http.StatusNoContent)); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("MountErr invalid param error = %v, want ErrInvalidParam", err)
	}
	if r.hasSubRouters {
		t.Fatal("router hasSubRouters = true after failed first MountErr, want false")
	}

	if err := r.MountErr("/api/{name}.json", writeStatus(http.StatusAccepted)); err != nil {
		t.Fatalf("MountErr valid mount error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.MountErr("/api/v{version}.json", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("MountErr conflict error = %v, want *match.ConflictError", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/foo.json", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestMountConflictsWithSubRouter(t *testing.T) {
	r := New()
	r.SubRouter("/api/{name}.json")

	var conflict *match.ConflictError
	if err := r.MountErr("/api/v{version}.json", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("MountErr conflict error = %v, want *match.ConflictError", err)
	}
}

func TestMountShadowsParentRouteUnderSamePrefix(t *testing.T) {
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

			assertStatus(t, rec, http.StatusGone)
		})
	}
}

func TestHostRouterMatchesAndMergesParams(t *testing.T) {
	r := New()
	tenant := r.Host("{tenant}.example.com")
	tenant.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "tenant"); got != "acme" {
			t.Fatalf("Param(tenant) = %q, want %q", got, "acme")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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

func TestHostRouterPreservesParamNameCase(t *testing.T) {
	r := New()
	tenant := r.Host("{Tenant}.Example.COM")
	tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "Tenant"); got != "acme" {
			t.Fatalf("Param(Tenant) = %q, want %q", got, "acme")
		}
		if got := Param(req, "tenant"); got != "" {
			t.Fatalf("Param(tenant) = %q, want empty", got)
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
		if got := Param(req, "name"); got != "a/b" {
			t.Fatalf("Param(name) = %q, want %q", got, "a/b")
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

func TestHostErrReturnsMatchErrors(t *testing.T) {
	r := New()
	if child, err := r.HostErr("{}.example.com"); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("HostErr invalid param child, error = %v, %v; want ErrInvalidParam", child, err)
	}
	if r.hasHosts {
		t.Fatal("router hasHosts = true after failed first HostErr, want false")
	}

	api, err := r.HostErr("api.example.com")
	if err != nil {
		t.Fatalf("HostErr valid host error = %v", err)
	}
	api.Get("/", writeStatus(http.StatusAccepted))

	var conflict *match.ConflictError
	if child, err := r.HostErr("api.example.com"); !errors.As(err, &conflict) {
		t.Fatalf("HostErr duplicate child, error = %v, %v; want *match.ConflictError", child, err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))
	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostErrRejectsEmptyNormalizedHost(t *testing.T) {
	tests := []string{
		"",
		":80",
	}

	for _, pattern := range tests {
		t.Run(pattern, func(t *testing.T) {
			r := New()
			child, err := r.HostErr(pattern)
			if !errors.Is(err, match.ErrInvalidParam) {
				t.Fatalf("HostErr(%q) child, error = %v, %v; want ErrInvalidParam", pattern, child, err)
			}
			if child != nil {
				t.Fatalf("HostErr(%q) child = %v, want nil", pattern, child)
			}
			if r.hasHosts {
				t.Fatalf("HostErr(%q) set hasHosts = true, want false", pattern)
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

func TestHostRouterInheritsStrictSlashSetting(t *testing.T) {
	r := New()
	r.SetStrictSlash(false)
	api := r.Host("api.example.com")
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
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

func TestParamsReturnsRequestParams(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		params := Params(req)
		var _ match.Params = params
		var _ RequestParams = params
		if params.Get("id") != "42" {
			t.Fatalf("Params(req).Get(id) = %q, want %q", params.Get("id"), "42")
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
}

func TestParamsDoNotSetRequestPathValuesByDefault(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("id"); got != "" {
			t.Fatalf("req.PathValue(id) = %q, want empty", got)
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
}

func TestParamsSetRequestPathValuesWhenEnabled(t *testing.T) {
	r := New()
	r.SetRequestPathValues(true)
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
	r.SetMethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Pattern; got != "/api/{version}/users/{id}" {
			t.Fatalf("req.Pattern = %q, want %q", got, "/api/{version}/users/{id}")
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
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

func TestParamsReturnsZeroValueWhenNoParams(t *testing.T) {
	r := New()
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		if got := Params(req).Len(); got != 0 {
			t.Fatalf("Params(req).Len() = %d, want 0", got)
		}
		if got := Param(req, "missing"); got != "" {
			t.Fatalf("Param(missing) = %q, want empty", got)
		}
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestPathValueMergePrecedence(t *testing.T) {
	r := New()
	r.SetRequestPathValues(true)
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

func TestSubRouterSnapshotsRequestPathValueSetting(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	r.SetRequestPathValues(true)
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "" {
			t.Fatalf("req.PathValue(version) = %q, want empty", got)
		}
		if got := req.PathValue("id"); got != "" {
			t.Fatalf("req.PathValue(id) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestHostRouterSnapshotsRequestPathValueSetting(t *testing.T) {
	r := New()
	api := r.Host("{tenant}.example.com")
	r.SetRequestPathValues(true)
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("tenant"); got != "" {
			t.Fatalf("req.PathValue(tenant) = %q, want empty", got)
		}
		if got := req.PathValue("id"); got != "" {
			t.Fatalf("req.PathValue(id) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://acme.example.com/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestSubRouterCanConfigureRequestPathValuesIndependently(t *testing.T) {
	r := New()
	r.SetRequestPathValues(true)
	api := r.SubRouter("/api/{version}")
	api.SetRequestPathValues(false)
	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := req.PathValue("version"); got != "" {
			t.Fatalf("req.PathValue(version) = %q, want empty", got)
		}
		if got := req.PathValue("id"); got != "" {
			t.Fatalf("req.PathValue(id) = %q, want empty", got)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRequestPathValueVisibleToSubRouterMiddleware(t *testing.T) {
	r := New()
	api := r.SubRouter("/api/{version}")
	api.SetRequestPathValues(true)
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
		if got := Param(req, "version"); got != "v1" {
			t.Fatalf("Param(version) = %q, want %q", got, "v1")
		}
		if got := Param(req, "id"); got != "42" {
			t.Fatalf("Param(id) = %q, want %q", got, "42")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/42", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestParamMergePrecedence(t *testing.T) {
	r := New()
	host := r.Host("{id}.example.com")
	sub := host.SubRouter("/{id}")
	sub.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		params := Params(req)
		if got := params.Len(); got != 1 {
			t.Fatalf("Params(req).Len() = %d, want 1", got)
		}
		if got := Param(req, "id"); got != "route" {
			t.Fatalf("Param(id) = %q, want %q", got, "route")
		}
		w.WriteHeader(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://host.example.com/sub/route", nil))

	assertStatus(t, rec, http.StatusAccepted)
}

func TestRouterServesConcurrentRequestsAfterRegistration(t *testing.T) {
	r := New()
	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		if got := Param(req, "id"); got == "" {
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
