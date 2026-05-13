package arc

import (
	"context"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/ryanfowler/match"
)

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
	routeRegistrations []routeRegistration
	routes             map[string]*match.Router[*route]
	methodRoutes       match.Router[*routeMethods]
	routeMethods       map[string]*routeMethods
	subMounts          mountMatcher
	hostRoutes         match.Router[*hostRouter]
	hasHosts           bool
	hasSubRouters      bool

	middleware []Middleware

	notFound         http.Handler
	methodNotAllowed http.Handler
}

type route struct {
	handler http.Handler
}

type routeRegistration struct {
	method  string
	pattern string
	route   *route
}

type routeMethods struct {
	methods []string
	allow   string
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

type routerHandler struct {
	router *Router
}

func (h routerHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path, params := dispatchState(req)
	h.router.serve(w, req, path, params)
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
		routeMethods:     make(map[string]*routeMethods),
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
// added. Use panics if any middleware is nil.
func (r *Router) Use(mw ...Middleware) {
	for _, m := range mw {
		if m == nil {
			panic("arc: nil middleware")
		}
		r.middleware = append(r.middleware, m)
	}
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

	if methods, _, ok := r.methodRoutes.Match(path); ok {
		w.Header().Set("Allow", methods.allowHeader())
		r.methodNotAllowed.ServeHTTP(w, requestForHandler(req, params))
		return
	}

	r.notFound.ServeHTTP(w, requestForHandler(req, params))
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
		router.handler.ServeHTTP(w, requestForRouter(req, path, nextParams))
		return true
	}

	router.router.serve(w, req, path, nextParams)
	return true
}

func (r *Router) serveSubRouter(w http.ResponseWriter, req *http.Request, path string, params match.Params) bool {
	if !r.hasSubRouters {
		return false
	}

	sub, nextPath, subParams, ok := r.subMounts.Match(path)
	if !ok {
		return false
	}

	nextParams := mergeParams(params, subParams)
	if len(sub.middleware) > 0 {
		sub.handler.ServeHTTP(w, requestForRouter(req, nextPath, nextParams))
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

	route.handler.ServeHTTP(w, requestForHandler(req, mergeParams(params, routeParams)))
	return true
}

func (r *Router) insertMethodRoute(reg routeRegistration) error {
	routes := r.routes[reg.method]
	if routes != nil {
		return routes.TryInsert(reg.pattern, reg.route)
	}

	routes = &match.Router[*route]{}
	if err := routes.TryInsert(reg.pattern, reg.route); err != nil {
		return err
	}
	r.routes[reg.method] = routes
	return nil
}

func (r *Router) rebuildRouteTables() {
	routes, methodRoutes, routeMethods, err := buildRouteTables(r.routeRegistrations)
	if err != nil {
		panic(err)
	}
	r.routes = routes
	r.methodRoutes = methodRoutes
	r.routeMethods = routeMethods
}

func buildRouteTables(registrations []routeRegistration) (map[string]*match.Router[*route], match.Router[*routeMethods], map[string]*routeMethods, error) {
	routes := make(map[string]*match.Router[*route])
	methodsByPattern := make(map[string]*routeMethods)
	var methodRoutes match.Router[*routeMethods]

	for _, reg := range registrations {
		methodRouter := routes[reg.method]
		if methodRouter == nil {
			methodRouter = &match.Router[*route]{}
			routes[reg.method] = methodRouter
		}
		if err := methodRouter.TryInsert(reg.pattern, reg.route); err != nil {
			return nil, match.Router[*routeMethods]{}, nil, err
		}

		methods := methodsByPattern[reg.pattern]
		if methods == nil {
			methods = &routeMethods{}
			if err := methodRoutes.TryInsert(reg.pattern, methods); err != nil {
				return nil, match.Router[*routeMethods]{}, nil, err
			}
			methodsByPattern[reg.pattern] = methods
		}
		methods.add(reg.method)
	}

	return routes, methodRoutes, methodsByPattern, nil
}

func (r *Router) addRouteMethod(pattern, method string) (*routeMethods, error) {
	methods := r.routeMethods[pattern]
	if methods != nil {
		methods.add(method)
		return methods, nil
	}

	methods = &routeMethods{}
	if err := r.methodRoutes.TryInsert(pattern, methods); err != nil {
		return nil, err
	}
	methods.add(method)
	r.routeMethods[pattern] = methods
	return methods, nil
}

func (m *routeMethods) add(method string) {
	i := sort.SearchStrings(m.methods, method)
	if i < len(m.methods) && m.methods[i] == method {
		return
	}

	m.methods = append(m.methods, "")
	copy(m.methods[i+1:], m.methods[i:])
	m.methods[i] = method
	m.allow = strings.Join(m.methods, ", ")
}

func (m *routeMethods) allowHeader() string {
	return m.allow
}

func compose(h http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		if middleware[i] == nil {
			continue
		}
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

func requestForHandler(req *http.Request, params match.Params) *http.Request {
	if params.Len() == 0 {
		return req
	}

	if paramsEqual(Params(req), params) {
		return req
	}

	return req.WithContext(context.WithValue(req.Context(), requestParamsKey, params))
}

func requestForRouter(req *http.Request, path string, params match.Params) *http.Request {
	currentParams := Params(req)
	currentPath, hasPath := dispatchPath(req)
	paramsMatch := paramsEqual(currentParams, params)
	if paramsMatch && hasPath && currentPath == path {
		return req
	}

	ctx := req.Context()
	if !paramsMatch {
		ctx = context.WithValue(ctx, requestParamsKey, params)
	}
	if !hasPath || currentPath != path {
		ctx = context.WithValue(ctx, requestDispatchKey, path)
	}
	if ctx == req.Context() {
		return req
	}

	return req.WithContext(ctx)
}

type requestContextKey int

const (
	requestParamsKey requestContextKey = iota
	requestDispatchKey
)

func dispatchState(req *http.Request) (string, match.Params) {
	path, ok := dispatchPath(req)
	if !ok {
		path = req.URL.Path
	}
	return path, Params(req)
}

func dispatchPath(req *http.Request) (string, bool) {
	path, ok := req.Context().Value(requestDispatchKey).(string)
	return path, ok
}

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

func paramsEqual(a, b match.Params) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if a.At(i) != b.At(i) {
			return false
		}
	}
	return true
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
	for i := range conflict {
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
