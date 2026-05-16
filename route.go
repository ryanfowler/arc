package arc

import (
	"net/http"
)

// Handle registers h for pattern and lets it handle any request method.
//
// Use Handle for endpoints such as health checks or webhooks where the handler
// should decide which methods are acceptable. The pattern uses the
// github.com/ryanfowler/match route grammar; for example, /users/{id} captures
// one segment and /assets/{*path} captures the remaining path.
//
// Subrouters and mounted handlers are matched before routes on the same router.
// A parent route below a mounted prefix, such as /api/healthz when /api is a
// subrouter or mount, is not reached by requests under that prefix.
//
// Invalid, duplicate, or ambiguous patterns panic with the error returned by
// match. Use HandleErr to receive the registration error instead.
func (r *Router) Handle(pattern string, h http.Handler) {
	if err := r.HandleErr(pattern, h); err != nil {
		panic(err)
	}
}

// HandleErr registers h for pattern, lets it handle any request method, and
// returns registration errors.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Registration
// errors include invalid parameter syntax, duplicate parameter names within the
// pattern, and route conflicts reported by match. A nil handler is treated as
// http.NotFoundHandler.
//
// Subrouters and mounted handlers are matched before routes on the same router.
// A route below a mounted prefix is shadowed by that child for requests under
// the prefix.
func (r *Router) HandleErr(pattern string, h http.Handler) error {
	return r.handleErr("", pattern, h, true)
}

// HandleMethod registers h for one HTTP method and pattern.
//
// Use HandleMethod when you have an http.Handler value. For http.HandlerFunc
// handlers, the method helpers such as Get and Post are usually shorter.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Invalid,
// duplicate, or ambiguous patterns panic with the error returned by match. Use
// HandleMethodErr to receive the registration error instead.
func (r *Router) HandleMethod(method, pattern string, h http.Handler) {
	if err := r.HandleMethodErr(method, pattern, h); err != nil {
		panic(err)
	}
}

// HandleMethodErr registers h for one HTTP method and pattern and returns
// registration errors.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Registration
// errors include invalid parameter syntax, duplicate parameter names within the
// pattern, and route conflicts reported by match. A nil handler is treated as
// http.NotFoundHandler.
func (r *Router) HandleMethodErr(method, pattern string, h http.Handler) error {
	return r.handleErr(method, pattern, h, false)
}

func (r *Router) handleErr(method, pattern string, h http.Handler, anyMethod bool) error {
	if h == nil {
		h = http.NotFoundHandler()
	}

	compiled := compose(h, r.middleware)
	fullPattern := joinPatterns(r.patternPrefix, pattern)
	rt := &route{
		handler: compiled,
		pattern: fullPattern,
	}
	registration := routeRegistration{
		method:      method,
		anyMethod:   anyMethod,
		pattern:     normalizeEscapedSlashPattern(pattern),
		fullPattern: fullPattern,
		route:       rt,
	}

	if err := r.insertRoute(registration); err != nil {
		return err
	}

	return nil
}

// HandleFunc registers h for pattern and lets it handle any request method.
//
// HandleFunc is a convenience wrapper around Handle.
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.Handle(pattern, handlerFuncOrNil(h))
}

// HandleMethodFunc registers h for one HTTP method and pattern.
//
// HandleMethodFunc is a convenience wrapper around HandleMethod.
func (r *Router) HandleMethodFunc(method, pattern string, h http.HandlerFunc) {
	r.HandleMethod(method, pattern, handlerFuncOrNil(h))
}

func handlerFuncOrNil(h http.HandlerFunc) http.Handler {
	if h == nil {
		return nil
	}
	return h
}

// Get registers h for GET requests matching pattern.
//
// By default, the route also handles HEAD requests when no explicit HEAD or
// any-method route matches. Use Router.SetImplicitHead(false) to require an
// explicit HEAD route.
func (r *Router) Get(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodGet, pattern, h)
}

// Post registers h for POST requests matching pattern.
func (r *Router) Post(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodPost, pattern, h)
}

// Put registers h for PUT requests matching pattern.
func (r *Router) Put(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodPut, pattern, h)
}

// Patch registers h for PATCH requests matching pattern.
func (r *Router) Patch(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodPatch, pattern, h)
}

// Delete registers h for DELETE requests matching pattern.
func (r *Router) Delete(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodDelete, pattern, h)
}

// Head registers h for HEAD requests matching pattern.
func (r *Router) Head(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodHead, pattern, h)
}

// Options registers h for OPTIONS requests matching pattern.
func (r *Router) Options(pattern string, h http.HandlerFunc) {
	r.HandleMethodFunc(http.MethodOptions, pattern, h)
}
