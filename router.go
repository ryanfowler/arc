package arc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/ryanfowler/match"
)

const subRouterRestParam = "__arc_rest"

// Middleware wraps an HTTP handler.
//
// Middleware is composed in registration order. If a router uses middleware a,
// b, and c, requests flow through a, then b, then c, then the matched handler.
type Middleware func(http.Handler) http.Handler

// RequestParams contains parameters captured while matching a request.
//
// RequestParams aliases match.Params, so callers can use match's Len, At, Get,
// TryGet, Seq, AppendTo, and All methods directly.
type RequestParams = match.Params

// Router routes HTTP requests by host, path, and method.
//
// Router implements http.Handler. Build a router by registering middleware,
// host routers, subrouters, and routes, then pass it to http.Server or
// http.ListenAndServe.
//
// A Router is safe for concurrent serving after registration is complete. The
// registration methods are not safe to call concurrently with ServeHTTP or with
// other registration methods.
type Router struct {
	routes     map[string]*match.Router[*route]
	anyRoutes  match.Router[struct{}]
	subExact   match.Router[*subRouter]
	subPrefix  match.Router[*subRouter]
	hostRoutes match.Router[*hostRouter]
	hasHosts   bool

	middleware []Middleware

	notFound         http.Handler
	methodNotAllowed http.Handler
}

type route struct {
	handler http.Handler
}

type subRouter struct {
	router     *Router
	handler    http.Handler
	middleware []Middleware
}

type hostRouter struct {
	router     *Router
	handler    http.Handler
	middleware []Middleware
}

// Option configures a Router when it is created with New.
type Option func(*Router)

// New returns an initialized Router.
//
// By default, unmatched requests use http.NotFoundHandler and requests whose
// path matches a route registered for a different method receive status 405.
func New(opts ...Option) *Router {
	r := &Router{
		routes:           make(map[string]*match.Router[*route]),
		notFound:         http.NotFoundHandler(),
		methodNotAllowed: http.HandlerFunc(defaultMethodNotAllowed),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithNotFound configures the handler used when no host, subrouter, or route
// matches a request.
//
// Passing nil leaves the router's existing not-found handler unchanged.
func WithNotFound(h http.Handler) Option {
	return func(r *Router) {
		if h != nil {
			r.notFound = h
		}
	}
}

// WithMethodNotAllowed configures the handler used when a request path matches
// a route pattern, but the request method was not registered for that pattern.
//
// Passing nil leaves the router's existing method-not-allowed handler
// unchanged.
func WithMethodNotAllowed(h http.Handler) Option {
	return func(r *Router) {
		if h != nil {
			r.methodNotAllowed = h
		}
	}
}

func defaultMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// Use appends middleware to the router.
//
// Middleware applies only to routes, subrouters, and host routers registered
// after the call to Use. This lets callers build separate sections of a router
// with different middleware stacks. Middleware is executed in the order it is
// added.
func (r *Router) Use(mw ...Middleware) {
	r.middleware = append(r.middleware, mw...)
}

// ServeHTTP dispatches req to the best matching host router, subrouter, or
// route.
//
// ServeHTTP satisfies http.Handler. It should usually be called by net/http
// rather than directly.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.serve(w, req, req.URL.Path, match.Params{})
}

func (r *Router) serve(w http.ResponseWriter, req *http.Request, path string, params match.Params) {
	if r.serveHost(w, req, path, params) {
		return
	}
	if r.serveSubRouter(w, req, path, params) {
		return
	}
	if r.serveRoute(w, req, path, params) {
		return
	}

	if _, _, ok := r.anyRoutes.Match(path); ok {
		r.methodNotAllowed.ServeHTTP(w, requestForHandler(req, path, params))
		return
	}

	r.notFound.ServeHTTP(w, requestForHandler(req, path, params))
}

func (r *Router) serveHost(w http.ResponseWriter, req *http.Request, path string, params match.Params) bool {
	if !r.hasHosts {
		return false
	}

	host := normalizeHost(req.Host)
	if host == "" {
		return false
	}

	router, hostParams, ok := r.hostRoutes.Match(host)
	if !ok {
		return false
	}

	nextParams := mergeParams(params, hostParams)
	if len(router.middleware) > 0 {
		router.handler.ServeHTTP(w, requestForHandler(req, path, nextParams))
		return true
	}

	router.router.serve(w, req, path, nextParams)
	return true
}

func (r *Router) serveSubRouter(w http.ResponseWriter, req *http.Request, path string, params match.Params) bool {
	sub, subParams, ok := r.subExact.Match(path)
	if ok {
		nextParams := mergeParams(params, subParams)
		if len(sub.middleware) > 0 {
			sub.handler.ServeHTTP(w, requestForHandler(req, "/", nextParams))
			return true
		}

		sub.router.serve(w, req, "/", nextParams)
		return true
	}

	sub, subParams, ok = r.subPrefix.Match(path)
	if !ok {
		return false
	}

	rest := subParams.Get(subRouterRestParam)
	nextPath := restPath(rest)
	nextParams := mergeParams(params, withoutParam(subParams, subRouterRestParam))
	if len(sub.middleware) > 0 {
		sub.handler.ServeHTTP(w, requestForHandler(req, nextPath, nextParams))
		return true
	}

	sub.router.serve(w, req, nextPath, nextParams)
	return true
}

func (r *Router) serveRoute(w http.ResponseWriter, req *http.Request, path string, params match.Params) bool {
	routes := r.routes[req.Method]
	if routes == nil {
		return false
	}

	route, routeParams, ok := routes.Match(path)
	if !ok {
		return false
	}

	route.handler.ServeHTTP(w, requestForHandler(req, path, mergeParams(params, routeParams)))
	return true
}

func (r *Router) methodRouter(method string) *match.Router[*route] {
	routes := r.routes[method]
	if routes == nil {
		routes = &match.Router[*route]{}
		r.routes[method] = routes
	}
	return routes
}

func compose(h http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func normalizeHost(host string) string {
	if host == "" {
		return ""
	}

	if i := strings.LastIndexByte(host, ':'); i > 0 && strings.IndexByte(host[:i], ':') == -1 {
		host = host[:i]
		return strings.ToLower(host)
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

func restPath(rest string) string {
	if rest == "" {
		return "/"
	}
	if strings.HasPrefix(rest, "/") {
		return rest
	}
	return "/" + rest
}

func requestForHandler(req *http.Request, path string, params match.Params) *http.Request {
	if params.Len() > 0 {
		existing := Params(req)
		if existing.Len() > 0 {
			params = mergeParams(existing, params)
		}
	}

	if req.URL.Path == path && params.Len() == 0 {
		return req
	}

	clone := req
	if params.Len() > 0 {
		clone = req.WithContext(context.WithValue(req.Context(), requestParamsKey, params))
	} else {
		clone = new(http.Request)
		*clone = *req
	}

	if req.URL.Path != path {
		url := *req.URL
		url.Path = path
		url.RawPath = ""
		clone.URL = &url
	}
	return clone
}

func ignoreDuplicate(err error) error {
	var conflict *match.ConflictError
	if errors.As(err, &conflict) && conflict.Route == conflict.With {
		return nil
	}
	return err
}

type paramsContextKey struct{}

var requestParamsKey paramsContextKey

// Params returns the parameters captured while matching req.
//
// The returned value is empty when the request did not match a parameterized
// host, subrouter, or route. If the same parameter name is captured at multiple
// levels, the more specific match wins: route parameters override subrouter
// parameters, and subrouter parameters override host parameters.
func Params(req *http.Request) RequestParams {
	params, _ := req.Context().Value(requestParamsKey).(RequestParams)
	return params
}

// Param returns the named parameter captured while matching req.
//
// Param returns an empty string when name was not captured. Use Params(req).
// TryGet when callers need to distinguish a missing parameter from a captured
// empty string, although arc's underlying matcher captures only non-empty
// values.
func Param(req *http.Request, name string) string {
	return Params(req).Get(name)
}

func mergeParams(base, overlay match.Params) match.Params {
	if base.Len() == 0 {
		return overlay
	}
	if overlay.Len() == 0 {
		return base
	}

	for i := 0; i < base.Len(); i++ {
		param := base.At(i)
		if _, ok := overlay.TryGet(param.Key); ok {
			return mergeParamsWithOverride(base, overlay, i)
		}
	}

	return match.Merge(base, overlay)
}

func mergeParamsWithOverride(base, overlay match.Params, conflict int) match.Params {
	if base.Len() == 1 {
		return overlay
	}

	filtered := make([]match.Param, 0, base.Len()-1)
	for i := 0; i < conflict; i++ {
		filtered = append(filtered, base.At(i))
	}
	for i := conflict + 1; i < base.Len(); i++ {
		param := base.At(i)
		if _, ok := overlay.TryGet(param.Key); ok {
			continue
		}
		filtered = append(filtered, param)
	}

	return match.Merge(match.ParamsOf(filtered...), overlay)
}

func withoutParam(params match.Params, name string) match.Params {
	if params.Len() == 0 {
		return params
	}

	switch params.Len() {
	case 1:
		param := params.At(0)
		if param.Key == name {
			return match.Params{}
		}
		return match.ParamsOf(param)
	case 2:
		first := params.At(0)
		second := params.At(1)
		if first.Key == name {
			return match.ParamsOf(second)
		}
		if second.Key == name {
			return match.ParamsOf(first)
		}
		return match.ParamsOf(first, second)
	}

	out := make([]match.Param, 0, params.Len())
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		if param.Key == name {
			continue
		}
		out = append(out, param)
	}
	return match.ParamsOf(out...)
}
