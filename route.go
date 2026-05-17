package arc

import (
	"net/http"
)

// Handle registers h for one HTTP method and path pattern.
//
// Use Handle when you have an [http.Handler] value. For [http.HandlerFunc]
// handlers, the method helpers such as [Router.Get] and [Router.Post] are
// usually shorter.
//
// The pattern must begin with "/" and may contain named parameters such as
// "/users/{id}" or catch-all parameters such as "/assets/{*path}". Invalid,
// duplicate, or ambiguous patterns panic. Use [Router.TryHandle] to receive the
// registration error instead.
func (r *Router) Handle(method, pattern string, h http.Handler) {
	if err := r.TryHandle(method, pattern, h); err != nil {
		panic(err)
	}
}

// TryHandle registers h for one HTTP method and path pattern and returns
// registration errors.
//
// Registration errors include non-absolute path patterns, invalid parameter
// syntax, duplicate parameter names within the pattern, duplicate registrations,
// and ambiguous patterns that could match the same requests. A nil handler is
// treated as [http.NotFoundHandler].
func (r *Router) TryHandle(method, pattern string, h http.Handler) error {
	return r.tryHandle(method, pattern, h, false)
}

// HandleAll registers h for pattern and lets it handle any request method.
//
// Use HandleAll for endpoints such as health checks or webhooks where the
// handler should decide which methods are acceptable:
//
//	r.HandleAll("/healthz", http.HandlerFunc(health))
//
// The path pattern follows Arc's route syntax. For example, "/users/{id}"
// captures one segment and "/assets/{*path}" captures the remaining path.
//
// Routes, subrouters, and mounted handlers share one path matcher on the same
// router. The most specific path wins, so a direct route below a mounted prefix,
// such as /api/healthz when /api is a subrouter or mount, handles that path.
//
// Invalid, duplicate, or ambiguous patterns panic. Use [Router.TryHandleAll] to
// receive the registration error instead.
func (r *Router) HandleAll(pattern string, h http.Handler) {
	if err := r.TryHandleAll(pattern, h); err != nil {
		panic(err)
	}
}

// TryHandleAll registers h for pattern, lets it handle any request method, and
// returns registration errors.
//
// Registration errors include non-absolute path patterns, invalid parameter
// syntax, duplicate parameter names within the pattern, duplicate registrations,
// and ambiguous patterns that could match the same requests. A nil handler is
// treated as [http.NotFoundHandler].
//
// Routes, subrouters, and mounted handlers share one path matcher on the same
// router. The most specific path wins, so a direct route below a mounted prefix
// handles that path.
func (r *Router) TryHandleAll(pattern string, h http.Handler) error {
	return r.tryHandle("", pattern, h, true)
}

func (r *Router) tryHandle(method, pattern string, h http.Handler, anyMethod bool) error {
	if err := validateHTTPPathPattern(pattern); err != nil {
		return err
	}

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
		pattern:     normalizePercentEncodedPattern(pattern),
		fullPattern: fullPattern,
		route:       rt,
	}

	if err := r.insertRoute(registration); err != nil {
		return err
	}

	return nil
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
// any-method route matches the same path. Use [Router.SetImplicitHead] with
// false to require an explicit HEAD route.
func (r *Router) Get(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodGet, pattern, handlerFuncOrNil(h))
}

// Post registers h for POST requests matching pattern.
func (r *Router) Post(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodPost, pattern, handlerFuncOrNil(h))
}

// Put registers h for PUT requests matching pattern.
func (r *Router) Put(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodPut, pattern, handlerFuncOrNil(h))
}

// Patch registers h for PATCH requests matching pattern.
func (r *Router) Patch(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodPatch, pattern, handlerFuncOrNil(h))
}

// Delete registers h for DELETE requests matching pattern.
func (r *Router) Delete(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodDelete, pattern, handlerFuncOrNil(h))
}

// Head registers h for HEAD requests matching pattern.
func (r *Router) Head(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodHead, pattern, handlerFuncOrNil(h))
}

// Options registers h for OPTIONS requests matching pattern.
func (r *Router) Options(pattern string, h http.HandlerFunc) {
	r.Handle(http.MethodOptions, pattern, handlerFuncOrNil(h))
}
