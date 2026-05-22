# arc

[![Go Reference](https://pkg.go.dev/badge/github.com/ryanfowler/arc.svg)](https://pkg.go.dev/github.com/ryanfowler/arc)

`arc` is a minimal, high-performance HTTP router for Go applications that want
route parameters, middleware groups, subrouters, mounted handlers, host-based
routing, and clear method handling while staying close to
[`net/http`](https://pkg.go.dev/net/http).

Handlers are ordinary `http.Handler` and `http.HandlerFunc` values. Middleware
is normal handler wrapping. A router is itself an `http.Handler`, so it can be
passed directly to `http.ListenAndServe` or `http.Server`.

## Install

```sh
go get github.com/ryanfowler/arc
```

## Quick Start

Create a router during application startup, register routes on it, then serve
requests with `net/http`.

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
		fmt.Fprintf(w, "user %s\n", req.PathValue("id"))
	})

	log.Fatal(http.ListenAndServe(":8080", r))
}
```

Build the router once before serving. After registration is complete, a router
is safe for concurrent requests.

## Register Routes

Most applications use the method helpers. They accept `http.HandlerFunc`
handlers and register one HTTP method for one path pattern.

```go
r.Get("/users/{id}", getUser)
r.Post("/users", createUser)
r.Put("/users/{id}", updateUser)
r.Patch("/users/{id}", patchUser)
r.Delete("/users/{id}", deleteUser)
r.Head("/users/{id}", headUser)
r.Options("/users/{id}", optionsUser)
```

Use `Handle` when you already have an `http.Handler`.

```go
r.Handle(http.MethodGet, "/status", statusHandler)
```

Use `HandleAll` when the handler should receive every method and decide what is
acceptable.

```go
r.HandleAll("/healthz", http.HandlerFunc(health))
```

For the same path pattern, method-specific routes take precedence over an
any-method route. Path specificity is considered before method handling, so a
more specific path can win even when a less specific path has the exact method.

```go
r.Get("/users/{id}", getUser)
r.HandleAll("/users/me", currentUser)

// GET /users/me uses currentUser.
```

When a path matches but the method does not, Arc runs the method-not-allowed
handler, returns `405 Method Not Allowed` by default, and sets the `Allow`
header.

## Pattern Syntax

Route, subrouter, and mount path patterns must be absolute paths beginning with
`/`. Host patterns match `Request.Host` instead of `req.URL.Path` and use DNS
labels separated by `.`.

Literal text matches exactly. A named parameter, written `{name}`, captures one
non-empty path segment. The value is exposed through
[`req.PathValue`](https://pkg.go.dev/net/http#Request.PathValue).

```go
r.Get("/users/{id}", getUser)

// GET /users/42 captures id = "42".
// GET /users/ does not match because the segment is empty.
```

Parameters can appear inside a segment with literal text around them. Each
segment can contain at most one parameter.

```go
r.Get("/files/{name}.json", getJSON)

// GET /files/report.json captures name = "report".
```

A catch-all parameter, written `{*name}`, captures the non-empty remainder of
the path, including slashes. It must be at the end of the pattern.

```go
r.Get("/assets/{*path}", serveAsset)

// GET /assets/css/app.css captures path = "css/app.css".
// GET /assets does not match because the catch-all value would be empty.
```

Host patterns match the whole normalized request host, not a suffix. A host
parameter matches one non-empty DNS label, either as a whole label or with
literal text around it; when literal text is present, only the parameter part is
captured. Each host label can contain at most one parameter. A host catch-all
parameter captures one or more leading labels and must appear in the leftmost
label. IPv6 literals are matched as ordinary single-label hosts, so a pattern
such as `{host}` can capture `::1`.

```go
r.Host("{tenant}.example.com")
// Host acme.example.com captures tenant = "acme".
// Host a.b.example.com does not match.

r.Host("api-{region}.example.com")
// Host api-us-west.example.com captures region = "us-west".

r.Host("{*subdomain}.example.com")
// Host a.b.example.com captures subdomain = "a.b".
// Host example.com does not match because the catch-all value would be empty.
```

Literal braces are escaped by doubling them.

```go
r.Get("/files/{{name}}", literalName)

// GET /files/{name} uses literalName.
```

Parameter names must be non-empty. They cannot contain `/`, and `*` is only
valid at the start of a catch-all parameter. A single pattern cannot capture the
same name more than once.

```go
r.Get("/{tenant}/users/{id}", getTenantUser) // valid
r.Get("/{id}/users/{id}", bad)               // invalid
```

Percent escapes in literal pattern text are decoded at registration. That means
these two patterns describe the same literal path and conflict if both are
registered:

```go
r.Get("/files/meta data", getMeta)
r.Get("/files/meta%20data", getMeta) // same literal pattern
```

Escaped slashes are treated as data inside a segment, not as path separators.

```go
r.Get("/files/{name}", getFile)
r.Get("/files/a/b", getNestedFile)

// GET /files/a%2Fb uses getFile and captures name = "a/b".
// GET /files/a/b uses getNestedFile.
```

Arc does not clean request paths or issue `http.ServeMux`-style redirects for
`.` segments, `..` segments, or repeated slashes. They are matched as they
appear in the request path.

## Matching Order

Arc chooses the most specific registered pattern that can match the request.
Literal segments beat parameter segments. Parameter segments with more literal
text beat looser parameter segments. Catch-all patterns are considered last.
Ambiguous patterns are rejected at registration instead of being resolved by
registration order.

```go
r.Get("/users/me", currentUser)
r.Get("/users/{id}", getUser)
r.Get("/users/{*path}", usersCatchAll)

// GET /users/me uses currentUser.
// GET /users/42 uses getUser.
// GET /users/a/b uses usersCatchAll.
```

Routes, subrouters, and mounted handlers registered on the same router share
the same path matcher. A direct route can therefore handle a specific path below
a subrouter or mount, while the child still owns the rest of that prefix.

```go
api := r.SubRouter("/api")
api.Get("/users/{id}", getUser)

r.Get("/api/healthz", healthz)

// GET /api/healthz uses the parent route.
// GET /api/users/42 uses the subrouter route.
```

Host routers are checked before ordinary path dispatch. Literal host labels are
more specific than parameter labels, and catch-all host patterns are considered
after finite host patterns. Ambiguous host patterns that cannot be ordered
deterministically are rejected at registration. If no host pattern matches, Arc
falls through to the parent router's path routes.

## Request Parameters

Captured route, subrouter, mount, and host parameters are stored as request path
values.

```go
r.Host("{tenant}.example.com").
	SubRouter("/api/{version}").
	Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(w, req.PathValue("tenant"))
		fmt.Fprintln(w, req.PathValue("version"))
		fmt.Fprintln(w, req.PathValue("id"))
	})
```

When the same name is captured at multiple levels, the most specific value wins:

```text
host params < subrouter params < route params
```

For mounted Arc routers, parameters captured by the outer mount remain
available to the inner router and its handlers.

## Matched Patterns

Arc sets [`req.Pattern`](https://pkg.go.dev/net/http#Request) before calling a
matched route, mounted handler, or method-not-allowed fallback. The value is the
full path pattern, including subrouter or mount prefixes.

```go
api := r.SubRouter("/api/{version}")
api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	log.Print(req.Pattern) // "/api/{version}/users/{id}"
})
```

Host patterns are not included in `req.Pattern`; host captures are still
available through `req.PathValue`.

Middleware can read `req.Pattern` once the route, mount, or method-not-allowed
fallback it wraps has been selected. Middleware inherited by host routers and
subrouters runs after the child router selects its final route or
method-not-allowed fallback, so it sees the final path pattern.

Not-found fallback handlers receive an empty `req.Pattern`, even when a host or
subrouter prefix matched and contributed parameters.

## Method Handling

`GET` routes handle `HEAD` requests by default when no explicit `HEAD` route or
any-method route matches the same path.

```go
r.Get("/resource", getResource)

// HEAD /resource uses getResource by default.
```

Explicit `HEAD` routes and any-method routes take precedence over implicit
`GET` handling.

```go
r.Get("/resource", getResource)
r.Head("/resource", headResource)

// HEAD /resource uses headResource.
```

Disable implicit `HEAD` matching when your application needs exact method
matching.

```go
r := arc.New()
r.SetImplicitHead(false)
r.Get("/resource", getResource)

// HEAD /resource returns 405 Method Not Allowed.
```

For method-not-allowed responses, the `Allow` header lists the registered
methods for the matched path. When implicit `HEAD` matching is enabled, `HEAD`
is included for paths that have a `GET` route.

## Trailing Slashes

Trailing slashes are significant by default.

```go
r.Get("/users/{id}", getUser)

// GET /users/42 matches.
// GET /users/42/ does not match.
```

Use `SetStrictSlash(false)` to allow a request ending in `/` to match a route
registered without that final slash.

```go
r := arc.New()
r.SetStrictSlash(false)
r.Get("/users/{id}", getUser)

// GET /users/42 and GET /users/42/ both match.
```

Exact matches still win. If both `/resource` and `/resource/` are registered,
`/resource/` uses the explicit trailing-slash route.

## Middleware

Middleware has the standard `net/http` shape: a function that wraps one handler
with another.

```go
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("%s %s", req.Method, req.URL.Path)
		next.ServeHTTP(w, req)
	})
}
```

Register middleware with `Use`.

```go
r := arc.New()
r.Use(logging)
r.Get("/healthz", health)
```

Middleware runs in the order it is registered.

```text
first before -> second before -> handler -> second after -> first after
```

Middleware applies to routes, subrouters, host routers, and mounted handlers
registered after the `Use` call. Fallback handlers use the current middleware
stack for the router that owns the fallback.

```go
r.Get("/healthz", health) // no auth middleware

r.Use(requireAuth)
r.Get("/account", account) // uses requireAuth
```

Middleware registered on a parent before creating a child router wraps the
child. Middleware added to the child applies only inside that child.

## Subrouters

Use `SubRouter` when a section of an application shares a path prefix,
middleware, fallback handlers, or settings.

```go
r := arc.New()

api := r.SubRouter("/api/{version}")
api.Use(requireAuth)

api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "%s user %s\n",
		req.PathValue("version"),
		req.PathValue("id"),
	)
})
```

A subrouter matches the remaining path after its mount point. A child mounted at
`/api` matches `/users` for a request to `/api/users`. Both `/api` and `/api/`
are dispatched to the child's `/` route.

The original request URL is not rewritten for subrouters. Parent middleware,
child middleware, and child handlers all see the original `req.URL.Path`.

```go
api := r.SubRouter("/api")
api.Get("/", apiIndex)

// GET /api and GET /api/ use apiIndex.
```

Subrouter prefixes are matched on whole path segments. A subrouter mounted at
`/api` does not match `/apix`.

An empty subrouter pattern is treated as `/`; a non-root subrouter pattern has
trailing slashes trimmed before registration.

## Mounted Handlers

Use `Mount` when an existing `http.Handler` should own everything below a path.
This is useful for file servers, third-party handlers, and other routers.

```go
r := arc.New()
r.Mount("/assets", http.FileServerFS(assets))
```

Mounted handlers receive the remaining path as `req.URL.Path`. Parent
middleware sees the original request path before the mounted handler receives
the rewritten path.

```go
r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	log.Print(req.URL.Path)
}))

// GET /assets/app.css logs "/app.css".
// GET /assets and GET /assets/ log "/".
```

Mount parameters are available through `req.PathValue`.

```go
r.Mount("/tenants/{tenant}/assets", assetHandler)

// GET /tenants/acme/assets/app.css exposes tenant = "acme".
```

Like subrouters, mount prefixes are matched on whole path segments. A mount at
`/assets` does not match `/assets-old`.

## Host Routers

Use `Host` when one application serves different routes for different domains
or subdomains.

```go
r := arc.New()

api := r.Host("api.example.com")
api.Get("/users/{id}", getUser)

tenant := r.Host("{tenant}.example.com")
tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "tenant %s\n", req.PathValue("tenant"))
})
```

Host patterns are matched against the whole normalized host. A pattern such as
`example.com` does not match `www.example.com`, and `{tenant}.example.com`
captures exactly one DNS label before `.example.com`; use another parameter
label or a leftmost catch-all to match more subdomain levels. `api-{region}`
captures one label with a required literal prefix, while
`api-{*subdomain}.example.com` captures leading labels after the `api-` prefix.
Literal host text is matched case-insensitively, while parameter names keep
their original case. Captured host parameter values come from the normalized
host, so ASCII letters are lowercase and IDNs are punycode.

Trailing dots are ignored, IDNs are normalized to punycode, and a numeric port
in `Request.Host` is ignored before matching. Brackets around colon-form hosts
are also ignored, so `[::1]` and `[::1]:8080` match the host pattern `::1`.
Host patterns themselves must not include a port.

Literal labels are more specific than parameter labels. If both
`api.example.com` and `{tenant}.example.com` are registered, `api.example.com`
uses the literal host router. Finite host patterns are more specific than
catch-all host patterns. Overlapping dynamic patterns with no deterministic
winner, such as `{tenant}.example.com` and `{account}.example.com`, conflict.

If no host pattern matches, Arc continues dispatching through the parent
router's ordinary routes, subrouters, and mounts.

## Fallback Handlers

By default, unmatched requests use `http.NotFoundHandler`, and paths registered
for a different method receive `405 Method Not Allowed`.

Customize fallback handlers during startup.

```go
r := arc.New()
r.SetNotFound(http.HandlerFunc(notFound))
r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))
```

Fallback handlers receive any parameters captured before the fallback was
selected.

```go
api := r.SubRouter("/api/{version}")
api.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	log.Print(req.PathValue("version"))
	http.NotFound(w, req)
}))
```

Fallback handlers run through middleware for the router that owns the fallback.
For a subrouter or host router, that includes middleware inherited from the
parent plus middleware registered on the child.

Passing `nil` to `SetNotFound` or `SetMethodNotAllowed` leaves the existing
fallback handler unchanged.

## Registration Errors

The non-`Try` registration methods panic on invalid, duplicate, or ambiguous
patterns. That is convenient for fixed application routes registered at startup.

```go
r.Get("/users/{id}", getUser)
```

Use the `Try` variants when patterns come from configuration, plugins, or any
runtime source.

```go
if err := r.TryHandle(http.MethodGet, "/users/{id}", http.HandlerFunc(getUser)); err != nil {
	return err
}

api, err := r.TrySubRouter("/api/{version}")
if err != nil {
	return err
}

if err := r.TryMount("/assets", http.FileServerFS(assets)); err != nil {
	return err
}

tenant, err := r.TryHost("{tenant}.example.com")
if err != nil {
	return err
}
```

Route methods that are not valid HTTP tokens return `arc.ErrInvalidMethod`.
Extension methods are accepted and method matching is case-sensitive. Route,
subrouter, and mount path patterns that do not begin with `/` return
`arc.ErrInvalidPathPattern`. Empty host patterns, host patterns with invalid DNS
characters, host patterns with ports, invalid host catch-all placement, and
invalid host parameter syntax return `arc.ErrInvalidHostPattern`. Patterns that
capture the same parameter name more than once return
`arc.ErrDuplicateParamName`. Other registration errors include invalid
parameter syntax, duplicate registrations, and ambiguous patterns that could
match the same requests.

Arc uses [`github.com/ryanfowler/match`](https://github.com/ryanfowler/match)
for path pattern matching and its `dns` subpackage for host pattern matching,
including affixed label parameters and leftmost catch-all parameters. Arc still
normalizes request hosts and validates host literals as DNS names. Applications
usually do not need to use the low-level matchers directly, though advanced
callers may inspect the syntax and conflict errors returned by `Try`
registrations.

## Child Router Configuration

Subrouters and host routers copy the parent router's current settings when they
are created. Configure shared behavior first.

```go
r := arc.New()
r.SetStrictSlash(false)
r.SetImplicitHead(false)
r.SetNotFound(http.HandlerFunc(notFound))
r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))

api := r.SubRouter("/api")
api.Get("/users/{id}", getUser)
```

Later changes on the parent do not affect existing children. Middleware follows
the same registration-order model: middleware already registered on the parent
is inherited by the child, while later parent middleware is not.

Child routers can still be configured independently after creation.

```go
api := r.SubRouter("/api")
api.SetNotFound(http.HandlerFunc(apiNotFound))
api.SetStrictSlash(true)
```

## Concurrency

Register routes and configure the router before serving requests.

A router is safe for concurrent serving after registration is complete.
Registration and configuration methods are not safe to call concurrently with
`ServeHTTP` or with each other.
