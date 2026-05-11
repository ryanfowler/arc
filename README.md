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
is ignored before matching.

If no host pattern matches, dispatch falls through to the parent router's
subrouters and routes.

## Fallback Handlers

By default, unmatched requests use `http.NotFoundHandler`, and paths registered
for a different method receive status `405 Method Not Allowed`.

You can customize both:

```go
r := arc.New(
	arc.WithNotFound(http.HandlerFunc(notFound)),
	arc.WithMethodNotAllowed(http.HandlerFunc(methodNotAllowed)),
)
```

## Registration Errors

The route helpers panic if a pattern is invalid, duplicated, or ambiguous:

```go
r.Get("/users/{id}", getUser)
```

Use `HandleErr` when you want to handle registration errors explicitly:

```go
if err := r.HandleErr(http.MethodGet, "/users/{id}", http.HandlerFunc(getUser)); err != nil {
	return err
}
```

Errors come from `github.com/ryanfowler/match`, including invalid parameter
syntax and `*match.ConflictError`.

## Concurrency

Build the router before serving requests.

A router is safe for concurrent serving after registration is complete.
Registration methods are not safe to call concurrently with `ServeHTTP` or with
other registration methods.
