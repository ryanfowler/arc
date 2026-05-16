# arc

[![Go Reference](https://pkg.go.dev/badge/github.com/ryanfowler/arc.svg)](https://pkg.go.dev/github.com/ryanfowler/arc)

`arc` is a minimal, high-performance HTTP router for Go applications that want to stay close to
`net/http`.

Use it when you want route parameters, method routing, middleware groups,
subrouters, mounted handlers, and host-based routing without adopting a web
framework. Handlers are ordinary `http.Handler` and `http.HandlerFunc` values,
middleware is normal handler wrapping, and the router itself can be passed
directly to `http.ListenAndServe` or `http.Server`.

Path and host matching are powered by
[`github.com/ryanfowler/match`](https://github.com/ryanfowler/match).

## Install

```sh
go get github.com/ryanfowler/arc
```

## Start an Application

Create a router during application startup, register your routes, and pass the
router to `net/http`.

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ryanfowler/arc"
)

func main() {
	r := arc.New()

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := arc.Param(req, "id")
		fmt.Fprintf(w, "user %s\n", id)
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

`arc.New()` returns an `*arc.Router`, which implements `http.Handler`. Build it
once, then serve requests with it. After registration is complete, the router is
safe for concurrent requests.

## Register Routes

Most applications use the method helpers:

```go
r.Get("/users/{id}", getUser)
r.Post("/users", createUser)
r.Put("/users/{id}", updateUser)
r.Delete("/users/{id}", deleteUser)
```

The helpers accept `http.HandlerFunc`. If you already have an `http.Handler`,
use `HandleMethod`:

```go
r.HandleMethod(http.MethodGet, "/status", statusHandler)
```

Use `Handle` or `HandleFunc` for a route that should accept any method:

```go
r.Handle("/healthz", http.HandlerFunc(health))
```

When a path exists but the method does not match, `arc` returns
`405 Method Not Allowed` and sets the `Allow` header.

## Write Route Patterns

Route patterns use the `match` route grammar:

- `/users/{id}` captures one non-empty path segment.
- `/assets/{*path}` captures the non-empty remainder of the path.
- Literal paths are preferred over parameter paths.
- Catch-all parameters must appear at the end of the pattern.

```go
r.Get("/users/me", currentUser)
r.Get("/users/{id}", getUser)
r.Get("/assets/{*path}", serveAsset)
```

In this example, `/users/me` uses `currentUser`, while `/users/42` uses
`getUser`.

Trailing slashes are significant by default. A request for `/users/42/` does
not match `/users/{id}` unless you relax slash matching:

```go
r := arc.New()
r.SetStrictSlash(false)
r.Get("/users/{id}", getUser) // matches /users/42 and /users/42/
```

Exact matches still win. If both `/users/{id}` and `/users/{id}/` are
registered, `/users/42/` uses the explicit trailing-slash route.

`GET` routes handle `HEAD` requests by default when there is no explicit `HEAD`
or any-method route for that path. Disable that if your application needs exact
method matching:

```go
r := arc.New()
r.SetImplicitHead(false)
r.Get("/users/{id}", getUser) // HEAD /users/42 returns 405
```

## Read Request Parameters

Use `arc.Param` when you need one parameter:

```go
r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	id := arc.Param(req, "id")
	fmt.Fprintln(w, id)
})
```

Use `arc.Params` when you need the full parameter set:

```go
params := arc.Params(req)
id, ok := params.TryGet("id")
```

`arc.Params(req)` returns `arc.RequestParams`, an alias of `match.Params`, so
the `match.Params` methods are available directly: `Len`, `At`, `Get`,
`TryGet`, `Seq`, `AppendTo`, and `All`.

By default, parameters are available through `arc.Param` and `arc.Params` only.
If your handlers or middleware expect standard library path values, enable that
compatibility option during startup. Do it before creating subrouters or host
routers that should inherit the setting:

```go
r := arc.New()
r.SetRequestPathValues(true)
```

Then `req.PathValue("id")` returns the same value as `arc.Param(req, "id")`.

When the same name is captured at multiple levels, the most specific match
wins:

```text
host params < subrouter params < route params
```

## Add Middleware

Middleware in `arc` is the same shape used throughout `net/http`: a function
that receives one handler and returns another.

```go
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("%s %s", req.Method, req.URL.Path)
		next.ServeHTTP(w, req)
	})
}
```

Register middleware with `Use`:

```go
r := arc.New()
r.Use(logging)
r.Get("/healthz", health)
```

Middleware applies to routes, subrouters, host routers, and mounted handlers
registered after the `Use` call. Fallback handlers use the router's current
middleware stack. Middleware runs in the order it is registered.

This makes it easy to build application sections with different middleware:

```go
r.Get("/healthz", health) // no auth middleware

r.Use(requireAuth)
r.Get("/account", account) // uses requireAuth
```

## Group Application Routes

Use `SubRouter` when a section of your application shares a path prefix,
middleware, or configuration.

```go
r := arc.New()

api := r.SubRouter("/api/{version}")
api.Use(requireAuth)

api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	version := arc.Param(req, "version")
	id := arc.Param(req, "id")
	fmt.Fprintf(w, "%s user %s\n", version, id)
})
```

A subrouter matches the remaining path after the mount point. A child mounted
at `/api` receives `/users` for a request to `/api/users`. Both `/api` and
`/api/` are dispatched to the child router's `/` route.

The original request URL is not rewritten for subrouters. Middleware and
handlers still see the original `req.URL.Path`.

## Mount Existing Handlers

Use `Mount` when another `http.Handler` should own everything below a path.
This is useful for file servers and other routers.

```go
r := arc.New()
r.Mount("/assets", http.FileServerFS(assets))
```

Mounted handlers receive the remaining path as `req.URL.Path`. For example, a
handler mounted at `/assets` receives `/app.css` for `/assets/app.css`, while
both `/assets` and `/assets/` are dispatched as `/`.

Mount parameters are available with `arc.Param`, and with `req.PathValue` when
request path values are enabled.

## Route by Host

Use `Host` when different domains or subdomains should have different routes.

```go
r := arc.New()

api := r.Host("api.example.com")
api.Get("/users/{id}", getUser)

tenant := r.Host("{tenant}.example.com")
tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "tenant %s\n", arc.Param(req, "tenant"))
})
```

Host matching is case-insensitive. If `Request.Host` includes a port, the port
is ignored before matching. Brackets around IPv6 literals are also ignored, so
`[::1]` and `[::1]:8080` match the host pattern `::1`.

If no host pattern matches, `arc` continues dispatching on the parent router's
subrouters and routes.

## Customize Fallbacks

By default:

- unmatched requests use `http.NotFoundHandler`;
- paths registered for a different method receive `405 Method Not Allowed`;
- the `Allow` header lists the effective methods for that path;
- when implicit `HEAD` matching is enabled, `Allow` includes `HEAD` for paths
  with a `GET` route.

Customize the fallback handlers during startup:

```go
r := arc.New()
r.SetNotFound(http.HandlerFunc(notFound))
r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))
```

Fallback handlers also receive matched parameters when the host, subrouter, or
route pattern captured any. They run through middleware for the router that
owns the fallback; a subrouter or host router fallback runs through parent
middleware that wrapped the child plus the child router's own middleware.

## Handle Registration Errors

The common registration methods panic when a pattern is invalid, duplicated, or
ambiguous. That is convenient for applications that register fixed routes at
startup:

```go
r.Get("/users/{id}", getUser)
```

Use the `Err` variants when routes come from configuration, plugins, or another
runtime source:

```go
if err := r.HandleMethodErr(http.MethodGet, "/users/{id}", http.HandlerFunc(getUser)); err != nil {
	return err
}

api, err := r.SubRouterErr("/api/{version}")
if err != nil {
	return err
}

if err := r.MountErr("/assets", http.FileServerFS(assets)); err != nil {
	return err
}

tenant, err := r.HostErr("{tenant}.example.com")
if err != nil {
	return err
}
```

Errors come from `github.com/ryanfowler/match`, including invalid parameter
syntax and `*match.ConflictError`.

## Configure Before Creating Children

Subrouters and host routers copy the parent router's current settings when they
are created. Configure shared behavior first:

```go
r := arc.New()
r.SetStrictSlash(false)
r.SetImplicitHead(false)
r.SetRequestPathValues(true)
r.SetNotFound(http.HandlerFunc(notFound))
r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))

api := r.SubRouter("/api")
api.Get("/users/{id}", getUser)
```

Later changes on the parent do not affect existing children. Middleware follows
the same registration-order model for routes, subrouters, host routers, and
mounted handlers. Fallback handlers use the current middleware stack on the
router that owns the fallback.

## Path Matching Details

`arc` normally matches `req.URL.Path`, as parsed by `net/http`.

When `req.URL.RawPath` preserves an escaped slash (`%2F` or `%2f`), `arc`
matches an internal decoded path where the escaped slash stays inside its path
segment. Captured parameters are restored before your handlers read them.

For example, `/files/a%2Fb` matches `/files/{id}` and captures `a/b`, but it
does not match the static route `/files/a/b`.

`arc` does not clean request paths or issue `ServeMux`-style redirects for `.`
segments, `..` segments, or repeated slashes. Those separators are matched as
they appear in the request path, apart from the optional single trailing slash
relaxation controlled by `SetStrictSlash(false)`.

## Concurrency

Register routes and configure the router before serving requests.

A router is safe for concurrent serving after registration is complete.
Registration and configuration methods are not safe to call concurrently with
`ServeHTTP` or with each other.
