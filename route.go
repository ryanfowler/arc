package arc

import (
	"net/http"
)

// Handle registers h for method and pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. For example,
// /users/{id} captures one segment and /assets/{*path} captures the remaining
// path. Invalid, duplicate, or ambiguous patterns panic with the error returned
// by match. Use HandleErr to receive the registration error instead.
func (r *Router) Handle(method, pattern string, h http.Handler) {
	if err := r.HandleErr(method, pattern, h); err != nil {
		panic(err)
	}
}

// HandleErr registers h for method and pattern and returns registration errors.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Registration
// errors include invalid parameter syntax and route conflicts reported by
// match. A nil handler is treated as http.NotFoundHandler.
func (r *Router) HandleErr(method, pattern string, h http.Handler) error {
	if h == nil {
		h = http.NotFoundHandler()
	}

	compiled := compose(h, r.middleware)
	rt := &route{handler: compiled}
	registration := routeRegistration{
		method:  method,
		pattern: pattern,
		route:   rt,
	}

	if err := r.insertMethodRoute(registration); err != nil {
		return err
	}
	if _, err := r.addRouteMethod(pattern, method); err != nil {
		r.rebuildRouteTables()
		return err
	}

	r.routeRegistrations = append(r.routeRegistrations, registration)
	return nil
}

// HandleFunc registers h for method and pattern.
//
// HandleFunc is a convenience wrapper around Handle.
func (r *Router) HandleFunc(method, pattern string, h http.HandlerFunc) {
	r.Handle(method, pattern, h)
}

// Get registers h for GET requests matching pattern.
func (r *Router) Get(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodGet, pattern, h)
}

// Post registers h for POST requests matching pattern.
func (r *Router) Post(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodPost, pattern, h)
}

// Put registers h for PUT requests matching pattern.
func (r *Router) Put(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodPut, pattern, h)
}

// Patch registers h for PATCH requests matching pattern.
func (r *Router) Patch(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodPatch, pattern, h)
}

// Delete registers h for DELETE requests matching pattern.
func (r *Router) Delete(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodDelete, pattern, h)
}

// Head registers h for HEAD requests matching pattern.
func (r *Router) Head(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodHead, pattern, h)
}

// Options registers h for OPTIONS requests matching pattern.
func (r *Router) Options(pattern string, h http.HandlerFunc) {
	r.HandleFunc(http.MethodOptions, pattern, h)
}
