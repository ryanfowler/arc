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

	middleware []Middleware

	notFound         http.Handler
	methodNotAllowed http.Handler
}

type route struct {
	handler http.Handler
}

type subRouter struct {
	router  *Router
	handler http.Handler
}

type hostRouter struct {
	router  *Router
	handler http.Handler
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
	if r.serveHost(w, req) {
		return
	}
	if r.serveSubRouter(w, req) {
		return
	}
	if r.serveRoute(w, req) {
		return
	}

	if _, _, ok := r.anyRoutes.Match(req.URL.Path); ok {
		r.methodNotAllowed.ServeHTTP(w, req)
		return
	}

	r.notFound.ServeHTTP(w, req)
}

func (r *Router) serveHost(w http.ResponseWriter, req *http.Request) bool {
	host := normalizeHost(req.Host)
	if host == "" {
		return false
	}

	router, params, ok := r.hostRoutes.Match(host)
	if !ok {
		return false
	}

	router.handler.ServeHTTP(w, withParams(req, params))
	return true
}

func (r *Router) serveSubRouter(w http.ResponseWriter, req *http.Request) bool {
	sub, params, ok := r.subExact.Match(req.URL.Path)
	if ok {
		nextReq := withParams(req, params)
		nextReq = withPath(nextReq, "/")
		sub.handler.ServeHTTP(w, nextReq)
		return true
	}

	sub, params, ok = r.subPrefix.Match(req.URL.Path)
	if !ok {
		return false
	}

	rest := params.Get(subRouterRestParam)
	nextReq := withParams(req, withoutParam(params, subRouterRestParam))
	nextReq = withPath(nextReq, restPath(rest))
	sub.handler.ServeHTTP(w, nextReq)
	return true
}

func (r *Router) serveRoute(w http.ResponseWriter, req *http.Request) bool {
	routes := r.routes[req.Method]
	if routes == nil {
		return false
	}

	route, params, ok := routes.Match(req.URL.Path)
	if !ok {
		return false
	}

	route.handler.ServeHTTP(w, withParams(req, params))
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
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

func withPath(req *http.Request, path string) *http.Request {
	clone := new(http.Request)
	*clone = *req
	url := *req.URL
	url.Path = path
	url.RawPath = ""
	clone.URL = &url
	return clone
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

func withParams(req *http.Request, params match.Params) *http.Request {
	if params.Len() == 0 {
		return req
	}

	existing := Params(req)
	if existing.Len() > 0 {
		params = mergeParams(existing, params)
	}

	return req.WithContext(context.WithValue(req.Context(), requestParamsKey, params))
}

func mergeParams(base, overlay match.Params) match.Params {
	if base.Len() == 1 && overlay.Len() == 1 {
		baseParam := base.At(0)
		overlayParam := overlay.At(0)
		if baseParam.Key == overlayParam.Key {
			return match.ParamsOf(overlayParam)
		}
		return match.ParamsOf(baseParam, overlayParam)
	}

	out := make([]match.Param, 0, base.Len()+overlay.Len())

	for i := 0; i < base.Len(); i++ {
		param := base.At(i)
		if _, ok := overlay.TryGet(param.Key); ok {
			continue
		}
		out = append(out, param)
	}

	out = overlay.AppendTo(out)
	return match.ParamsOf(out...)
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
