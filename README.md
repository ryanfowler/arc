# arc

`arc` is a lightweight HTTP routing library for Go.

It is designed to stay close to `net/http`: handlers are standard
`http.Handler` and `http.HandlerFunc` values, middleware is ordinary handler
wrapping, and a router can be passed directly to `http.ListenAndServe`.

Path and host matching are powered by
[`github.com/ryanfowler/match`](https://github.com/ryanfowler/match).

## Install

```sh
go get github.com/ryanfowler/arc
```

## Quick Start

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
		fmt.Fprintf(w, "user %s\n", arc.Param(req, "id"))
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

## Route Patterns

`arc` uses the `match` route grammar:

- `/users/{id}` captures one non-empty path segment.
- `/assets/{*path}` captures the non-empty remainder of the path.
- Literal segments are preferred over parameter segments.
- Catch-all parameters must appear at the end of a route.

```go
r.Get("/users/me", currentUser)
r.Get("/users/{id}", getUser)
r.Get("/assets/{*path}", serveAsset)
```

Route matching is strict about trailing slashes by default, so `/users/42/`
does not match `/users/{id}`. Disable strict slash matching when you want a
single trailing slash to be accepted by routes registered without one:

```go
r := arc.New()
r.SetStrictSlash(false)
r.Get("/users/{id}", getUser) // matches /users/42 and /users/42/
```

Exact route matches still take precedence when strict slash matching is
disabled. If both `/users/{id}` and `/users/{id}/` are registered, a request for
`/users/42/` uses the explicit trailing-slash route.

### Path Matching Details

`arc` matches against `req.URL.Path` as parsed by `net/http`. It does not match
against `req.URL.RawPath`, `req.URL.EscapedPath()`, or `RequestURI`.

This differs from `net/http.ServeMux` in two important edge cases:

- Escaped slashes are already decoded in `req.URL.Path`, so `/files/a%2Fb` is
  dispatched as `/files/a/b`. A route like `/files/{id}` does not match that
  request, while `/files/{*path}` can capture `a/b`.
- `arc` does not clean request paths or issue `ServeMux`-style redirects for
  `.` segments, `..` segments, or repeated slashes. Those paths are matched as
  they appear in `req.URL.Path`, apart from the optional single trailing slash
  relaxation controlled by `SetStrictSlash(false)`.

## Request Parameters

Use `arc.Param` for a single parameter:

```go
r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	id := arc.Param(req, "id")
	fmt.Fprintln(w, id)
})
```

Use `arc.Params` when you want access to the underlying parameter value:

```go
params := arc.Params(req)
id, ok := params.TryGet("id")
```

`arc.Params(req)` returns `arc.RequestParams`, which is an alias of
`match.Params`. That means methods like `Len`, `At`, `Get`, `TryGet`, `Seq`,
`AppendTo`, and `All` are available directly.

Captured parameters are not mirrored to the standard library request path
values by default. Enable that compatibility path when middleware or handlers
need `req.PathValue`:

```go
r := arc.New()
r.SetRequestPathValues(true)
```

With request path values enabled, `req.PathValue("id")` works with net/http
middleware that expects it.

If a parameter name is captured at multiple levels, the more specific match
wins:

```text
host params < subrouter params < route params
```

## Middleware

Middleware is a function that wraps an `http.Handler`.

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

Middleware applies to routes, subrouters, and host routers registered after the
call to `Use`. Middleware runs in registration order.

## Configuration Order

Use router setters for routing behavior that should apply consistently:

```go
r := arc.New()
r.SetStrictSlash(false)
r.SetRequestPathValues(true)
```

Subrouters and host routers snapshot parent settings when they are created.
Calls to `SetStrictSlash`, `SetRequestPathValues`, `SetNotFound`, or
`SetMethodNotAllowed` after creating a child do not affect that existing child.
Call those setters before `SubRouter` or `Host` when you want a child to start
with the same behavior.

Middleware follows the same registration-order model: `Use` only wraps routes,
subrouters, and host routers registered after the call.

## Subrouters

`SubRouter` returns another `*arc.Router` mounted below a pattern.

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

A subrouter matches using the remaining path after the mount point. For example,
`/api/users` is dispatched inside the child router as `/users`. Both `/api` and
`/api/` are dispatched to the child router's `/` route. The request URL is not
rewritten; middleware and handlers still see the original `req.URL.Path`.

## Mounted Handlers

`Mount` attaches any `http.Handler` below a pattern.

```go
r := arc.New()

r.Mount("/tenants/{tenant}/assets", http.FileServerFS(assets))
```

Mounted handlers receive the remaining path after the mount point as
`req.URL.Path`. For example, a handler mounted at `/assets` receives `/app.css`
for a request to `/assets/app.css`, while both `/assets` and `/assets/` are
dispatched as `/`. Mount parameters are available with `arc.Param`, and with
`req.PathValue` when request path values are enabled.

## Host Routing

`Host` returns a child router that only handles matching request hosts.

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

If no host pattern matches, dispatch falls through to the parent router's
subrouters and routes.

## Fallback Handlers

By default, unmatched requests use `http.NotFoundHandler`, and paths registered
for a different method receive status `405 Method Not Allowed` with an `Allow`
header listing the registered methods.

You can customize both:

```go
r := arc.New()
r.SetNotFound(http.HandlerFunc(notFound))
r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))
```

Subrouters inherit the parent router's fallback handlers when they are created,
and can also configure their own handlers.

## Registration Errors

Route, mount, subrouter, and host registration helpers panic if a pattern is
invalid, duplicated, or ambiguous:

```go
r.Get("/users/{id}", getUser)
```

Use `HandleErr`, `MountErr`, `SubRouterErr`, or `HostErr` when you want to handle
registration errors explicitly:

```go
if err := r.HandleErr(http.MethodGet, "/users/{id}", http.HandlerFunc(getUser)); err != nil {
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

## Concurrency

Build the router before serving requests.

A router is safe for concurrent serving after registration is complete.
Registration and configuration methods are not safe to call concurrently with
`ServeHTTP` or with other registration and configuration methods.
