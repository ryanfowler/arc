package arc

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
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

func TestRouterMethodNotAllowedAllowHeaderIncludesRegisteredMethods(t *testing.T) {
	r := New()
	r.Post("/resource/{id}", writeStatus(http.StatusCreated))
	r.Get("/resource/{id}", writeStatus(http.StatusAccepted))
	r.Delete("/resource/{id}", writeStatus(http.StatusNoContent))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/resource/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "DELETE, GET, POST"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestRouterMethodNotAllowedAllowHeaderWithCustomHandler(t *testing.T) {
	r := New(WithMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict))))
	r.Get("/known", writeStatus(http.StatusNoContent))
	r.Patch("/known", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/known", nil))

	assertStatus(t, rec, http.StatusConflict)
	if got, want := rec.Header().Get("Allow"), "GET, PATCH"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
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

func TestOptionsConfigureFallbackHandlers(t *testing.T) {
	r := New(
		WithNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot))),
		WithMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict))),
	)
	r.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/known", nil))
	assertStatus(t, methodNotAllowed, http.StatusConflict)
}

func TestHandleErrReturnsMatchErrors(t *testing.T) {
	r := New()
	if err := r.HandleErr(http.MethodGet, "/users/{}", writeStatus(http.StatusNoContent)); !errors.Is(err, match.ErrInvalidParam) {
		t.Fatalf("HandleErr invalid param error = %v, want ErrInvalidParam", err)
	}

	if err := r.HandleErr(http.MethodGet, "/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("HandleErr valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.HandleErr(http.MethodGet, "/users/{name}", writeStatus(http.StatusNoContent)); !errors.As(err, &conflict) {
		t.Fatalf("HandleErr conflict error = %v, want *match.ConflictError", err)
	}
}

func TestHandleErrDoesNotPartiallyRegisterFailedRoute(t *testing.T) {
	r := New()
	if err := r.HandleErr(http.MethodGet, "/users/{id}", writeStatus(http.StatusNoContent)); err != nil {
		t.Fatalf("HandleErr valid route error = %v", err)
	}

	var conflict *match.ConflictError
	if err := r.HandleErr(http.MethodPost, "/users/{name}", writeStatus(http.StatusCreated)); !errors.As(err, &conflict) {
		t.Fatalf("HandleErr conflict error = %v, want *match.ConflictError", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/users/42", nil))

	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", got, http.MethodGet)
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
	r := New(
		WithNotFound(http.HandlerFunc(writeStatus(http.StatusTeapot))),
		WithMethodNotAllowed(http.HandlerFunc(writeStatus(http.StatusConflict))),
	)
	api := r.SubRouter("/api")
	api.Get("/known", writeStatus(http.StatusNoContent))

	notFound := httptest.NewRecorder()
	r.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	assertStatus(t, notFound, http.StatusTeapot)

	methodNotAllowed := httptest.NewRecorder()
	r.ServeHTTP(methodNotAllowed, httptest.NewRequest(http.MethodPost, "/api/known", nil))
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

func TestHostRouterFallsThroughToRootWhenHostDoesNotMatch(t *testing.T) {
	r := New()
	api := r.Host("api.example.com")
	api.Get("/", writeStatus(http.StatusNoContent))
	r.Get("/", writeStatus(http.StatusAccepted))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://www.example.com/", nil))

	assertStatus(t, rec, http.StatusAccepted)
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
