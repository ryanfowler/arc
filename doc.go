// Package arc provides a minimal, high-performance HTTP router for Go
// applications that want route parameters, middleware groups, subrouters,
// mounted handlers, host-based routing, and clear method handling while
// staying close to net/http.
//
// Handlers are ordinary http.Handler and http.HandlerFunc values. Middleware is
// normal handler wrapping. A Router is itself an http.Handler, so it can be
// passed directly to http.ListenAndServe or http.Server.
//
// # Quick Start
//
// Create a router during application startup, register routes on it, then serve
// requests with net/http:
//
//	r := arc.New()
//
//	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
//		fmt.Fprintln(w, "ok")
//	})
//
//	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
//		fmt.Fprintf(w, "user %s\n", req.PathValue("id"))
//	})
//
//	log.Fatal(http.ListenAndServe(":8080", r))
//
// Build the router once before serving. After registration is complete, a
// Router is safe for concurrent requests.
//
// # Registering Routes
//
// Most applications use the method helpers. They accept http.HandlerFunc
// handlers and register one HTTP method for one path pattern:
//
//	r.Get("/users/{id}", getUser)
//	r.Post("/users", createUser)
//	r.Put("/users/{id}", updateUser)
//	r.Patch("/users/{id}", patchUser)
//	r.Delete("/users/{id}", deleteUser)
//	r.Head("/users/{id}", headUser)
//	r.Options("/users/{id}", optionsUser)
//
// Use Router.Handle when you already have an http.Handler:
//
//	r.Handle(http.MethodGet, "/status", statusHandler)
//
// Use Router.HandleAll when the handler should receive every method and decide
// what is acceptable:
//
//	r.HandleAll("/healthz", http.HandlerFunc(health))
//
// For the same path pattern, method-specific routes take precedence over an
// any-method route. Path specificity is considered before method handling, so a
// more specific path can win even when a less specific path has the exact
// method.
//
//	r.Get("/users/{id}", getUser)
//	r.HandleAll("/users/me", currentUser)
//
//	// GET /users/me uses currentUser.
//
// When a path matches but the method does not, Arc runs the method-not-allowed
// handler, returns 405 Method Not Allowed by default, and sets the Allow header.
//
// # Pattern Syntax
//
// Route, subrouter, and mount path patterns must be absolute paths beginning
// with "/". Host patterns match Request.Host instead of req.URL.Path and use
// DNS labels separated by ".".
//
// Literal text matches exactly. A named parameter, written {name}, captures one
// non-empty path segment. The value is exposed through http.Request.PathValue.
//
//	r.Get("/users/{id}", getUser)
//
//	// GET /users/42 captures id = "42".
//	// GET /users/ does not match because the segment is empty.
//
// Parameters can appear inside a segment with literal text around them. Each
// segment can contain at most one parameter.
//
//	r.Get("/files/{name}.json", getJSON)
//
//	// GET /files/report.json captures name = "report".
//
// A catch-all parameter, written {*name}, captures the non-empty remainder of
// the path, including slashes. It must be at the end of the pattern.
//
//	r.Get("/assets/{*path}", serveAsset)
//
//	// GET /assets/css/app.css captures path = "css/app.css".
//	// GET /assets does not match because the catch-all value would be empty.
//
// Host parameters must occupy an entire DNS label and capture exactly one
// non-empty label. Host patterns do not support catch-all parameters.
//
//	r.Host("{tenant}.example.com")
//
//	// Host acme.example.com captures tenant = "acme".
//	// Host a.b.example.com does not match.
//
// Literal braces are escaped by doubling them:
//
//	r.Get("/files/{{name}}", literalName)
//
//	// GET /files/{name} uses literalName.
//
// Parameter names must be non-empty. They cannot contain "/", and "*" is only
// valid at the start of a catch-all parameter. A single pattern cannot capture
// the same name more than once.
//
//	r.Get("/{tenant}/users/{id}", getTenantUser) // valid
//	r.Get("/{id}/users/{id}", bad)               // invalid
//
// Percent escapes in literal pattern text are decoded at registration. The
// patterns "/files/meta data" and "/files/meta%20data" describe the same
// literal path and conflict if both are registered.
//
// Escaped slashes are treated as data inside a segment, not as path separators:
//
//	r.Get("/files/{name}", getFile)
//	r.Get("/files/a/b", getNestedFile)
//
//	// GET /files/a%2Fb uses getFile and captures name = "a/b".
//	// GET /files/a/b uses getNestedFile.
//
// Arc does not clean request paths or issue http.ServeMux-style redirects for
// "." segments, ".." segments, or repeated slashes. They are matched as they
// appear in the request path.
//
// # Matching Order
//
// Arc chooses the most specific registered pattern that can match the request.
// Literal segments beat parameter segments. Parameter segments with more
// literal text beat looser parameter segments. Catch-all patterns are
// considered last. Ambiguous patterns are rejected at registration instead of
// being resolved by registration order.
//
//	r.Get("/users/me", currentUser)
//	r.Get("/users/{id}", getUser)
//	r.Get("/users/{*path}", usersCatchAll)
//
//	// GET /users/me uses currentUser.
//	// GET /users/42 uses getUser.
//	// GET /users/a/b uses usersCatchAll.
//
// Routes, subrouters, and mounted handlers registered on the same Router share
// the same path matcher. A direct route can therefore handle a specific path
// below a subrouter or mount, while the child still owns the rest of that
// prefix.
//
//	api := r.SubRouter("/api")
//	api.Get("/users/{id}", getUser)
//
//	r.Get("/api/healthz", healthz)
//
//	// GET /api/healthz uses the parent route.
//	// GET /api/users/42 uses the subrouter route.
//
// Host routers are checked before ordinary path dispatch. If no host pattern
// matches, Arc falls through to the parent router's path routes.
//
// # Request Parameters
//
// Captured route, subrouter, mount, and host parameters are stored as request
// path values:
//
//	r.Host("{tenant}.example.com").
//		SubRouter("/api/{version}").
//		Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
//			fmt.Fprintln(w, req.PathValue("tenant"))
//			fmt.Fprintln(w, req.PathValue("version"))
//			fmt.Fprintln(w, req.PathValue("id"))
//		})
//
// When the same name is captured at multiple levels, the most specific value
// wins:
//
//	host params < subrouter params < route params
//
// For mounted Arc routers, parameters captured by the outer mount remain
// available to the inner router and its handlers.
//
// # Matched Patterns
//
// Arc sets http.Request.Pattern before calling a matched route, mounted handler,
// or method-not-allowed fallback. The value is the full path pattern, including
// subrouter or mount prefixes.
//
//	api := r.SubRouter("/api/{version}")
//	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
//		log.Print(req.Pattern) // "/api/{version}/users/{id}"
//	})
//
// Host patterns are not included in Request.Pattern; host captures are still
// available through Request.PathValue.
//
// Middleware can read Request.Pattern once the route, mount, or
// method-not-allowed fallback it wraps has been selected. Middleware inherited
// by host routers and subrouters runs after the child router selects its final
// route or method-not-allowed fallback, so it sees the final path pattern.
//
// Not-found fallback handlers receive an empty Request.Pattern, even when a
// host or subrouter prefix matched and contributed parameters.
//
// # Method Handling
//
// GET routes handle HEAD requests by default when no explicit HEAD route or
// any-method route matches the same path.
//
//	r.Get("/resource", getResource)
//
//	// HEAD /resource uses getResource by default.
//
// Explicit HEAD routes and any-method routes take precedence over implicit GET
// handling. Use Router.SetImplicitHead(false) when your application needs exact
// method matching.
//
//	r := arc.New()
//	r.SetImplicitHead(false)
//	r.Get("/resource", getResource)
//
//	// HEAD /resource returns 405 Method Not Allowed.
//
// For method-not-allowed responses, the Allow header lists the registered
// methods for the matched path. When implicit HEAD matching is enabled, HEAD is
// included for paths that have a GET route.
//
// # Trailing Slashes
//
// Trailing slashes are significant by default.
//
//	r.Get("/users/{id}", getUser)
//
//	// GET /users/42 matches.
//	// GET /users/42/ does not match.
//
// Use Router.SetStrictSlash(false) to allow a request ending in "/" to match a
// route registered without that final slash.
//
//	r := arc.New()
//	r.SetStrictSlash(false)
//	r.Get("/users/{id}", getUser)
//
//	// GET /users/42 and GET /users/42/ both match.
//
// Exact matches still win. If both "/resource" and "/resource/" are registered,
// "/resource/" uses the explicit trailing-slash route.
//
// # Middleware
//
// Middleware has the standard net/http shape: a function that wraps one handler
// with another.
//
//	func logging(next http.Handler) http.Handler {
//		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
//			log.Printf("%s %s", req.Method, req.URL.Path)
//			next.ServeHTTP(w, req)
//		})
//	}
//
// Register middleware with Router.Use:
//
//	r := arc.New()
//	r.Use(logging)
//	r.Get("/healthz", health)
//
// Middleware runs in the order it is registered and applies to routes,
// subrouters, host routers, and mounted handlers registered after the Use call.
// Fallback handlers use the current middleware stack for the router that owns
// the fallback.
//
//	r.Get("/healthz", health) // no auth middleware
//
//	r.Use(requireAuth)
//	r.Get("/account", account) // uses requireAuth
//
// Middleware registered on a parent before creating a child router is inherited
// by the child. Middleware added to the child applies only inside that child.
//
// # Subrouters
//
// Use Router.SubRouter when a section of an application shares a path prefix,
// middleware, fallback handlers, or settings.
//
//	r := arc.New()
//
//	api := r.SubRouter("/api/{version}")
//	api.Use(requireAuth)
//
//	api.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
//		fmt.Fprintf(w, "%s user %s\n",
//			req.PathValue("version"),
//			req.PathValue("id"),
//		)
//	})
//
// A subrouter matches the remaining path after its mount point. A child mounted
// at "/api" matches "/users" for a request to "/api/users". Both "/api" and
// "/api/" are dispatched to the child's "/" route.
//
// The original request URL is not rewritten for subrouters. Parent middleware,
// child middleware, and child handlers all see the original req.URL.Path.
//
// Subrouter prefixes are matched on whole path segments. A subrouter mounted at
// "/api" does not match "/apix".
//
// An empty subrouter pattern is treated as "/"; a non-root subrouter pattern has
// trailing slashes trimmed before registration.
//
// # Mounted Handlers
//
// Use Router.Mount when an existing http.Handler should own everything below a
// path. This is useful for file servers, third-party handlers, and other
// routers.
//
//	r := arc.New()
//	r.Mount("/assets", http.FileServerFS(assets))
//
// Mounted handlers receive the remaining path as req.URL.Path. Parent
// middleware sees the original request path before the mounted handler receives
// the rewritten path.
//
//	r.Mount("/assets", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
//		log.Print(req.URL.Path)
//	}))
//
//	// GET /assets/app.css logs "/app.css".
//	// GET /assets and GET /assets/ log "/".
//
// Mount parameters are available through Request.PathValue.
//
//	r.Mount("/tenants/{tenant}/assets", assetHandler)
//
//	// GET /tenants/acme/assets/app.css exposes tenant = "acme".
//
// Like subrouters, mount prefixes are matched on whole path segments. A mount at
// "/assets" does not match "/assets-old".
//
// # Host Routers
//
// Use Router.Host when one application serves different routes for different
// domains or subdomains.
//
//	r := arc.New()
//
//	api := r.Host("api.example.com")
//	api.Get("/users/{id}", getUser)
//
//	tenant := r.Host("{tenant}.example.com")
//	tenant.Get("/", func(w http.ResponseWriter, req *http.Request) {
//		fmt.Fprintf(w, "tenant %s\n", req.PathValue("tenant"))
//	})
//
// Host matching is case-insensitive for literal host text, while parameter
// names keep their original case. A pattern such as "{tenant}.example.com"
// captures one DNS label before ".example.com"; use another parameter label to
// match another subdomain level. Trailing dots are ignored, IDNs are normalized
// to punycode, and a port in Request.Host is ignored before matching. Brackets
// around IPv6 literals are also ignored, so "[::1]" and "[::1]:8080" match the
// host pattern "::1".
//
// If no host pattern matches, Arc continues dispatching through the parent
// router's ordinary routes, subrouters, and mounts.
//
// # Fallback Handlers
//
// By default, unmatched requests use http.NotFoundHandler, and paths registered
// for a different method receive 405 Method Not Allowed.
//
// Customize fallback handlers during startup:
//
//	r := arc.New()
//	r.SetNotFound(http.HandlerFunc(notFound))
//	r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))
//
// Fallback handlers receive any parameters captured before the fallback was
// selected.
//
//	api := r.SubRouter("/api/{version}")
//	api.SetNotFound(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
//		log.Print(req.PathValue("version"))
//		http.NotFound(w, req)
//	}))
//
// Fallback handlers run through middleware for the router that owns the
// fallback. For a subrouter or host router, that includes middleware inherited
// from the parent plus middleware registered on the child.
//
// Passing nil to Router.SetNotFound or Router.SetMethodNotAllowed leaves the
// existing fallback handler unchanged.
//
// # Registration Errors
//
// The non-Try registration methods panic on invalid, duplicate, or ambiguous
// patterns. That is convenient for fixed application routes registered at
// startup. Use the Try variants when patterns come from configuration, plugins,
// or any runtime source.
//
//	if err := r.TryHandle(http.MethodGet, "/users/{id}", http.HandlerFunc(getUser)); err != nil {
//		return err
//	}
//
//	api, err := r.TrySubRouter("/api/{version}")
//	if err != nil {
//		return err
//	}
//
//	if err := r.TryMount("/assets", http.FileServerFS(assets)); err != nil {
//		return err
//	}
//
//	tenant, err := r.TryHost("{tenant}.example.com")
//	if err != nil {
//		return err
//	}
//
// Route methods that are not valid HTTP tokens return ErrInvalidMethod.
// Extension methods are accepted and method matching is case-sensitive. Route,
// subrouter, and mount path patterns that do not begin with "/" return
// ErrInvalidPathPattern. Empty host patterns, host patterns with invalid DNS
// characters, and host patterns with non-label parameter syntax return
// ErrInvalidHostPattern. Patterns that capture the same parameter name more
// than once return ErrDuplicateParamName. Other registration errors include
// invalid parameter syntax, duplicate registrations, and ambiguous patterns
// that could match the same requests.
//
// # Child Router Configuration
//
// Subrouters and host routers copy the parent router's current settings when
// they are created. Configure shared behavior first.
//
//	r := arc.New()
//	r.SetStrictSlash(false)
//	r.SetImplicitHead(false)
//	r.SetNotFound(http.HandlerFunc(notFound))
//	r.SetMethodNotAllowed(http.HandlerFunc(methodNotAllowed))
//
//	api := r.SubRouter("/api")
//	api.Get("/users/{id}", getUser)
//
// Later changes on the parent do not affect existing children. Middleware
// follows the same registration-order model: middleware already registered on
// the parent is inherited by the child, while later parent middleware is not.
//
// Child routers can still be configured independently after creation.
//
//	api := r.SubRouter("/api")
//	api.SetNotFound(http.HandlerFunc(apiNotFound))
//	api.SetStrictSlash(true)
//
// # Concurrency
//
// Register routes and configure the router before serving requests. A Router is
// safe for concurrent serving after registration is complete. Registration and
// configuration methods are not safe to call concurrently with ServeHTTP or with
// each other.
package arc
