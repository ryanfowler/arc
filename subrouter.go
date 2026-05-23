package arc

import (
	"net/http"
	"slices"
	"strings"
)

// SubRouter registers and returns a child router mounted at pattern.
//
// Use a subrouter when several routes share a path prefix, middleware, fallback
// handlers, or slash/method settings:
//
//	api := r.SubRouter("/api/{version}")
//	api.Use(requireAuth)
//	api.Get("/users/{id}", getUser)
//
// The pattern follows Arc's path pattern syntax. Parameters captured by the
// mount pattern are available to child middleware and handlers through
// [http.Request.PathValue].
//
// The child matches against the remaining path after the mount point. For
// example, a child mounted at /api matches /users for a request to /api/users,
// while both /api and /api/ are dispatched to the child's / route. The request
// URL is not rewritten; middleware and handlers still see the original
// req.URL.Path.
//
// Subrouters and direct parent routes share one path matcher. The most specific
// path wins, so a parent route such as /api/healthz handles that exact path even
// when /api is a subrouter. Other paths under the subrouter prefix are owned by
// the child, including child not-found and method-not-allowed handling. Register
// routes such as /api/healthz on the child as /healthz when they should use the
// child's middleware and fallback settings.
//
// Middleware already registered on the parent is inherited by the child router.
// It runs after the child selects its final route or fallback, so route and
// method-not-allowed middleware can read the final Request.Pattern. Middleware
// added to the child applies only inside the child router, including child
// fallback handlers. The child copies the parent's current strict slash,
// implicit HEAD, and fallback handler settings when it is created.
//
// An empty pattern is treated as "/"; non-root patterns have trailing slashes
// trimmed before registration. Invalid, duplicate, or ambiguous mount patterns
// panic. Use [Router.TrySubRouter] to receive the registration error instead.
func (r *Router) SubRouter(pattern string) *Router {
	child, err := r.TrySubRouter(pattern)
	if err != nil {
		panic(err)
	}
	return child
}

// TrySubRouter registers and returns a child router mounted at pattern and
// returns registration errors.
//
// An empty pattern is treated as "/"; non-root patterns have trailing slashes
// trimmed before registration. Registration errors include non-absolute path
// patterns, invalid parameter syntax, duplicate parameter names within the
// pattern, duplicate mounts, and ambiguous mount patterns that could match the
// same requests.
func (r *Router) TrySubRouter(pattern string) (*Router, error) {
	child := newChildRouter(r)
	pattern = cleanMountPattern(pattern)
	if err := validateHTTPPathPattern(pattern); err != nil {
		return nil, err
	}
	matchPattern := normalizePercentEncodedPattern(pattern)

	if err := validateUniqueParamNames(matchPattern); err != nil {
		return nil, err
	}
	if err := r.insertChildPathEntries(childPathRegistrations(matchPattern, child)); err != nil {
		return nil, err
	}

	child.router.patternPrefix = joinPatterns(r.patternPrefix, pattern)
	r.hasSubRouters = true
	return child.router, nil
}

// Mount registers h below pattern and lets that handler own the remaining path.
//
// Use Mount for file servers, another router, or any existing [http.Handler]
// that should handle everything below a path:
//
//	r.Mount("/assets", http.FileServerFS(assets))
//
// The pattern follows Arc's path pattern syntax. Parameters captured by the
// mount pattern are available to middleware and the mounted handler through
// [http.Request.PathValue].
//
// The mounted handler receives the remaining path after the mount point as
// req.URL.Path. For example, a handler mounted at /assets receives /app.css for
// a request to /assets/app.css, while both /assets and /assets/ are dispatched
// as /. Middleware already registered on the parent sees the original request
// path and wraps the mounted handler.
//
// Mounted handlers and direct parent routes share one path matcher. The most
// specific path wins, so a parent route below the mounted prefix, such as
// /assets/healthz, handles that exact path. Other paths under the mounted prefix
// are owned by the mounted handler.
//
// An empty pattern is treated as "/"; non-root patterns have trailing slashes
// trimmed before registration. Invalid, duplicate, or ambiguous mount patterns
// panic. Use [Router.TryMount] to receive the registration error instead.
func (r *Router) Mount(pattern string, h http.Handler) {
	if err := r.TryMount(pattern, h); err != nil {
		panic(err)
	}
}

// TryMount registers h below pattern and returns registration errors.
//
// An empty pattern is treated as "/"; non-root patterns have trailing slashes
// trimmed before registration. Registration errors include non-absolute path
// patterns, invalid parameter syntax, duplicate parameter names within the
// pattern, duplicate mounts, and ambiguous mount patterns that could match the
// same requests. A nil handler is treated as [http.NotFoundHandler].
func (r *Router) TryMount(pattern string, h http.Handler) error {
	if h == nil {
		h = defaultNotFoundHandler{}
	}

	pattern = cleanMountPattern(pattern)
	if err := validateHTTPPathPattern(pattern); err != nil {
		return err
	}
	child := &childRouter{
		router:     r,
		handler:    h,
		middleware: slices.Clone(r.middleware),
		mounted:    true,
		pattern:    joinPatterns(r.patternPrefix, pattern),
	}
	matchPattern := normalizePercentEncodedPattern(pattern)
	if err := validateUniqueParamNames(matchPattern); err != nil {
		return err
	}
	if err := r.insertChildPathEntries(childPathRegistrations(matchPattern, child)); err != nil {
		return err
	}

	r.hasSubRouters = true
	return nil
}

func cleanMountPattern(pattern string) string {
	if pattern == "" {
		return "/"
	}
	if pattern != "/" {
		pattern = strings.TrimRight(pattern, "/")
		if pattern == "" {
			return "/"
		}
	}
	return pattern
}

func childPathRegistrations(pattern string, child *childRouter) []childPathRegistration {
	regs := []childPathRegistration{{
		pattern: pattern,
		child:   child,
	}}
	if patternHasFinalCatchAll(pattern) {
		return regs
	}

	if pattern != "/" {
		regs = append(regs, childPathRegistration{
			pattern: pattern + "/",
			child:   child,
		})
	}

	return regs
}
