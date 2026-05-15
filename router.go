package arc

import (
	"context"
	"net"
	"net/http"
	"net/url"
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
// Router implements http.Handler. Build a router by configuring fallback
// handlers and registering middleware, host routers, subrouters, and routes,
// then pass it to http.Server or http.ListenAndServe.
//
// A Router is safe for concurrent serving after registration is complete. The
// registration and configuration methods are not safe to call concurrently with
// ServeHTTP or with other registration and configuration methods.
type Router struct {
	methodRoutes  match.Router[*routeMethods]
	routeMethods  map[string]*routeMethods
	subMounts     match.Router[*childRouter]
	hostRoutes    match.Router[*childRouter]
	hasHosts      bool
	hasSubRouters bool

	middleware []Middleware

	notFound         http.Handler
	methodNotAllowed http.Handler

	strictSlash       bool
	implicitHead      bool
	requestPathValues bool
	patternPrefix     string
	escapedPathMatch  bool
	parent            *Router
}

type route struct {
	handler http.Handler
	pattern string
}

type routeRegistration struct {
	method      string
	anyMethod   bool
	pattern     string
	fullPattern string
	route       *route
}

type routeMethods struct {
	methods           []string
	routes            map[string]*route
	anyRoute          *route
	allow             string
	allowImplicitHead string
	pattern           string
}

type childRouter struct {
	router  *Router
	handler http.Handler
	mounted bool
	pattern string
}

type routerHandler struct {
	router *Router
}

func (h routerHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path, params, decodeParams := dispatchState(req)
	h.router.serve(w, req, path, params, decodeParams)
}

// New returns an initialized Router.
//
// By default, unmatched requests use http.NotFoundHandler and requests whose
// path matches a route registered for a different method receive status 405.
// GET routes also handle HEAD requests unless an explicit HEAD or any-method
// route matches.
//
// Child routers and host routers copy the parent router's current settings when
// they are created.
func New() *Router {
	return &Router{
		routeMethods:     make(map[string]*routeMethods),
		notFound:         http.NotFoundHandler(),
		methodNotAllowed: http.HandlerFunc(defaultMethodNotAllowed),
		strictSlash:      true,
		implicitHead:     true,
	}
}

// SetNotFound configures the handler used when no host, subrouter, or route
// matches a request.
//
// Passing nil leaves the router's existing not-found handler unchanged.
func (r *Router) SetNotFound(h http.Handler) {
	if h != nil {
		r.notFound = h
	}
}

// SetMethodNotAllowed configures the handler used when a request path matches a
// route pattern, but the request method was not registered for that pattern.
//
// Passing nil leaves the router's existing method-not-allowed handler
// unchanged.
func (r *Router) SetMethodNotAllowed(h http.Handler) {
	if h != nil {
		r.methodNotAllowed = h
	}
}

// SetImplicitHead configures whether HEAD requests may use GET routes when no
// explicit HEAD or any-method route matches.
//
// Implicit HEAD matching is enabled by default. Explicit HEAD and any-method
// routes take precedence when present.
func (r *Router) SetImplicitHead(enabled bool) {
	r.implicitHead = enabled
}

// SetStrictSlash configures whether route matching treats a trailing slash as
// significant.
//
// Strict slash matching is enabled by default. When disabled, a request path
// ending in "/" may match a route registered without that final slash. Exact
// route matches still take precedence.
//
// Subrouters and host routers copy this setting when they are created. Later
// changes on the parent do not affect existing children.
func (r *Router) SetStrictSlash(strict bool) {
	r.strictSlash = strict
}

// SetRequestPathValues configures whether captured parameters are mirrored to
// http.Request.PathValue.
//
// Request path values are disabled by default. Enable this when middleware or
// handlers need to read route parameters with req.PathValue instead of arc.Param
// or arc.Params.
//
// Subrouters and host routers copy this setting when they are created. Later
// changes on the parent do not affect existing children.
func (r *Router) SetRequestPathValues(enabled bool) {
	r.requestPathValues = enabled
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
// Route and subrouter matching uses req.URL.Path unless req.URL.RawPath
// preserves an escaped slash. In that case, Arc matches the escaped path so the
// slash stays inside its segment and decodes captured params before exposing
// them. Arc does not perform net/http.ServeMux path cleaning redirects.
//
// ServeHTTP satisfies http.Handler. It should usually be called by net/http
// rather than directly.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	decodeParams := false
	if r.escapedPathMatch && hasEscapedSlash(req.URL.RawPath) {
		path, decodeParams = escapedSlashMatchPath(req)
	}
	r.serve(w, req, path, match.Params{}, decodeParams)
}

func (r *Router) serve(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) {
	if r.serveHost(w, req, path, params, decodeParams) {
		return
	}
	if r.serveSubRouter(w, req, path, params, decodeParams) {
		return
	}
	if r.serveRoute(w, req, path, params, decodeParams) {
		return
	}

	r.notFound.ServeHTTP(w, requestForHandler(req, params, "", r.requestPathValues))
}

func (r *Router) serveHost(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) bool {
	if !r.hasHosts {
		return false
	}

	host := normalizeHost(req.Host)
	if host == "" {
		return false
	}

	child, hostParams, ok := r.hostRoutes.Match(host)
	if !ok {
		return false
	}

	child.serve(w, req, path, mergeParams(params, hostParams), decodeParams)
	return true
}

func (r *Router) serveSubRouter(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) bool {
	if !r.hasSubRouters {
		return false
	}

	mount, ok := r.subMounts.MatchPrefix(path)
	if !ok {
		return false
	}

	mountParams := mount.Params
	if decodeParams {
		mountParams = restoreParams(mountParams)
	}
	mount.Value.serve(w, req, mount.Rest, mergeParams(params, mountParams), decodeParams)
	return true
}

func (r *Router) serveRoute(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) bool {
	methods, routeParams, ok := r.matchMethodRoute(path, decodeParams)
	if !ok {
		return false
	}
	route := methods.routeFor(req.Method, r.implicitHead)
	if route == nil {
		w.Header().Set("Allow", methods.allowHeader(r.implicitHead))
		r.methodNotAllowed.ServeHTTP(w, requestForHandler(req, mergeParams(params, routeParams), methods.pattern, r.requestPathValues))
		return true
	}

	route.handler.ServeHTTP(w, requestForHandler(req, mergeParams(params, routeParams), route.pattern, r.requestPathValues))
	return true
}

func (r *Router) matchMethodRoute(path string, decodeParams bool) (*routeMethods, match.Params, bool) {
	methods, params, ok := r.methodRoutes.Match(path)
	if ok {
		if decodeParams {
			params = restoreParams(params)
		}
		return methods, params, true
	}
	if path, ok = r.relaxedSlashPath(path); !ok {
		return nil, match.Params{}, false
	}
	methods, params, ok = r.methodRoutes.Match(path)
	if !ok || routePatternEndsInSlash(methods.pattern) {
		return nil, match.Params{}, false
	}
	if decodeParams {
		params = restoreParams(params)
	}
	return methods, params, true
}

func (r *Router) relaxedSlashPath(path string) (string, bool) {
	if r.strictSlash || len(path) <= 1 || path[len(path)-1] != '/' {
		return "", false
	}
	return path[:len(path)-1], true
}

func routePatternEndsInSlash(pattern string) bool {
	return len(pattern) > 0 && pattern[len(pattern)-1] == '/'
}

func joinPatterns(prefix, pattern string) string {
	if prefix == "" || prefix == "/" {
		return pattern
	}
	if pattern == "" {
		return prefix
	}
	if pattern == "/" {
		return prefix + "/"
	}
	if pattern[0] == '/' {
		return prefix + pattern
	}
	return prefix + "/" + pattern
}

func (r *Router) insertRoute(reg routeRegistration) error {
	return insertRouteRegistration(&r.methodRoutes, r.routeMethods, reg)
}

func insertRouteRegistration(methodRoutes *match.Router[*routeMethods], methodsByPattern map[string]*routeMethods, reg routeRegistration) error {
	methods := methodsByPattern[reg.pattern]
	if methods == nil {
		methods = &routeMethods{pattern: reg.fullPattern}
		if err := methodRoutes.TryInsert(reg.pattern, methods); err != nil {
			return err
		}
		methodsByPattern[reg.pattern] = methods
	}

	return methods.addRoute(reg)
}

func (m *routeMethods) addRoute(reg routeRegistration) error {
	if reg.anyMethod {
		if m.anyRoute != nil {
			return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
		}
		m.anyRoute = reg.route
		return nil
	}

	if m.routes == nil {
		m.routes = make(map[string]*route)
	}
	if m.routes[reg.method] != nil {
		return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
	}

	m.routes[reg.method] = reg.route
	m.add(reg.method)
	return nil
}

func (m *routeMethods) routeFor(method string, implicitHead bool) *route {
	if route := m.routes[method]; route != nil {
		return route
	}
	if m.anyRoute != nil {
		return m.anyRoute
	}
	if method == http.MethodHead && implicitHead {
		return m.routes[http.MethodGet]
	}
	return nil
}

func (m *routeMethods) add(method string) {
	i := sort.SearchStrings(m.methods, method)
	if i < len(m.methods) && m.methods[i] == method {
		return
	}

	m.methods = append(m.methods, "")
	copy(m.methods[i+1:], m.methods[i:])
	m.methods[i] = method
	m.updateAllowHeaders()
}

func (m *routeMethods) updateAllowHeaders() {
	m.allow = strings.Join(m.methods, ", ")
	m.allowImplicitHead = m.allow

	if !m.has(http.MethodGet) || m.has(http.MethodHead) {
		return
	}

	i := sort.SearchStrings(m.methods, http.MethodHead)
	methods := make([]string, len(m.methods)+1)
	copy(methods, m.methods[:i])
	methods[i] = http.MethodHead
	copy(methods[i+1:], m.methods[i:])
	m.allowImplicitHead = strings.Join(methods, ", ")
}

func (m *routeMethods) allowHeader(implicitHead bool) string {
	if implicitHead {
		return m.allowImplicitHead
	}
	return m.allow
}

func (m *routeMethods) has(method string) bool {
	i := sort.SearchStrings(m.methods, method)
	return i < len(m.methods) && m.methods[i] == method
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

func newChildRouter(parent *Router) *childRouter {
	r := New()
	r.SetNotFound(parent.notFound)
	r.SetMethodNotAllowed(parent.methodNotAllowed)
	r.SetStrictSlash(parent.strictSlash)
	r.SetImplicitHead(parent.implicitHead)
	r.SetRequestPathValues(parent.requestPathValues)
	r.patternPrefix = parent.patternPrefix
	r.parent = parent
	child := &childRouter{router: r}
	if len(parent.middleware) > 0 {
		child.handler = compose(routerHandler{router: r}, parent.middleware)
	}
	return child
}

func (r *Router) enableEscapedPathMatch() {
	for r != nil {
		r.escapedPathMatch = true
		r = r.parent
	}
}

func (c *childRouter) serve(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) {
	if c.mounted {
		c.handler.ServeHTTP(w, requestForMount(req, path, params, c.pattern, c.router.requestPathValues, decodeParams))
		return
	}
	if c.handler != nil {
		c.handler.ServeHTTP(w, requestForRouter(req, path, params, c.router.requestPathValues, decodeParams))
		return
	}
	c.router.serve(w, req, path, params, decodeParams)
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
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' && strings.IndexByte(host, ':') != -1 {
		host = host[1 : len(host)-1]
	}
	return strings.ToLower(host)
}

func requestForHandler(req *http.Request, params match.Params, pattern string, requestPathValues bool) *http.Request {
	if params.Len() == 0 {
		req.Pattern = pattern
		return req
	}

	return requestWithMatchState(req, requestMatchState{
		params:            params,
		pattern:           pattern,
		flags:             requestMatchPattern,
		requestPathValues: requestPathValues,
	})
}

func requestForRouter(req *http.Request, path string, params match.Params, requestPathValues bool, decodeParams bool) *http.Request {
	return requestWithMatchState(req, requestMatchState{
		params:            params,
		dispatchPath:      path,
		decodeParams:      decodeParams,
		flags:             requestMatchDispatchPath,
		requestPathValues: requestPathValues,
	})
}

func requestForMount(req *http.Request, path string, params match.Params, pattern string, requestPathValues bool, decodeParams bool) *http.Request {
	return requestWithMatchState(req, requestMatchState{
		params:            params,
		dispatchPath:      path,
		pattern:           pattern,
		decodeParams:      decodeParams,
		flags:             requestMatchDispatchPath | requestMatchPattern,
		requestPathValues: requestPathValues,
	})
}

func requestWithURLPath(req *http.Request, path string) *http.Request {
	if req.URL.Path == path {
		return req
	}

	next := new(http.Request)
	*next = *req
	url := *req.URL
	url.Path = path
	url.RawPath = ""
	next.URL = &url
	return next
}

type requestContextKey int

const (
	requestParamsKey requestContextKey = iota
	requestDispatchKey
	requestDecodeParamsKey
)

type requestMatchFlag uint8

const (
	requestMatchPattern requestMatchFlag = 1 << iota
	requestMatchDispatchPath
)

type requestMatchState struct {
	params            match.Params
	dispatchPath      string
	pattern           string
	decodeParams      bool
	flags             requestMatchFlag
	requestPathValues bool
}

// requestWithMatchState is the only place that decides whether a matched
// request needs arc state, a shallow request copy for PathValue updates, or no
// clone at all.
func requestWithMatchState(req *http.Request, state requestMatchState) *http.Request {
	if state.flags&requestMatchDispatchPath == 0 && state.params.Len() == 0 && !state.decodeParams {
		if state.flags&requestMatchPattern != 0 {
			req.Pattern = state.pattern
		}
		return req
	}

	paramsMatch := paramsEqual(Params(req), state.params)
	pathValuesMatch := requestPathValuesMatch(req, state.params, state.requestPathValues)
	dispatchPathMatch := true
	if state.flags&requestMatchDispatchPath != 0 {
		currentPath, ok := dispatchPath(req)
		dispatchPathMatch = ok && currentPath == state.dispatchPath
	}
	decodeParamsMatch := dispatchDecodeParams(req) == state.decodeParams

	if paramsMatch && pathValuesMatch && dispatchPathMatch && decodeParamsMatch {
		if state.flags&requestMatchPattern != 0 {
			req.Pattern = state.pattern
		}
		return req
	}

	next := requestWithState(req, requestState{
		params:       state.params,
		path:         state.dispatchPath,
		decodeParams: state.decodeParams,
		flags:        requestStateFlags(!paramsMatch, !dispatchPathMatch, !decodeParamsMatch && state.decodeParams),
		copyRequest:  !pathValuesMatch,
	})
	if !pathValuesMatch {
		setPathValues(next, state.params)
	}
	if state.flags&requestMatchPattern != 0 {
		next.Pattern = state.pattern
	}
	return next
}

type requestStateFlag uint8

const (
	requestStateParams requestStateFlag = 1 << iota
	requestStateDispatchPath
	requestStateDecodeParams
)

type requestState struct {
	params       match.Params
	path         string
	decodeParams bool
	flags        requestStateFlag
	copyRequest  bool
}

// requestStateContext keeps all arc request state in one wrapper. Flags mark
// which fields override any older arc state below it in the context chain.
type requestStateContext struct {
	context.Context
	params       match.Params
	path         string
	decodeParams bool
	flags        requestStateFlag
}

func (ctx *requestStateContext) Value(key any) any {
	switch key {
	case requestParamsKey:
		if ctx.flags&requestStateParams != 0 {
			return ctx.params
		}
	case requestDispatchKey:
		if ctx.flags&requestStateDispatchPath != 0 {
			return ctx.path
		}
	case requestDecodeParamsKey:
		if ctx.flags&requestStateDecodeParams != 0 {
			return ctx.decodeParams
		}
	}
	return ctx.Context.Value(key)
}

func requestStateFlags(withParams, withPath, withDecodeParams bool) requestStateFlag {
	var flags requestStateFlag
	if withParams {
		flags |= requestStateParams
	}
	if withPath {
		flags |= requestStateDispatchPath
	}
	if withDecodeParams {
		flags |= requestStateDecodeParams
	}
	return flags
}

func requestWithState(req *http.Request, state requestState) *http.Request {
	if state.flags == 0 {
		if state.copyRequest {
			return req.WithContext(req.Context())
		}
		return req
	}
	return req.WithContext(&requestStateContext{
		Context:      req.Context(),
		params:       state.params,
		path:         state.path,
		decodeParams: state.decodeParams,
		flags:        state.flags,
	})
}

func dispatchState(req *http.Request) (string, match.Params, bool) {
	path, ok := dispatchPath(req)
	decodeParams := dispatchDecodeParams(req)
	if !ok {
		path = req.URL.Path
		if hasEscapedSlash(req.URL.RawPath) {
			path, decodeParams = escapedSlashMatchPath(req)
		}
	}
	return path, Params(req), decodeParams
}

func escapedSlashMatchPath(req *http.Request) (string, bool) {
	path := req.URL.EscapedPath()
	if !hasEscapedSlash(path) {
		return req.URL.Path, false
	}
	return path, true
}

func hasEscapedSlash(path string) bool {
	for i := 0; i+2 < len(path); i++ {
		if path[i] == '%' && path[i+1] == '2' && (path[i+2] == 'f' || path[i+2] == 'F') {
			return true
		}
	}
	return false
}

func routePatternNeedsEscapedSlashMatch(pattern string) bool {
	return strings.Contains(pattern, "{") || hasEscapedSlash(pattern)
}

func restoreParams(params match.Params) match.Params {
	if params.Len() == 0 {
		return params
	}

	var restored []match.Param
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		val, err := url.PathUnescape(param.Val)
		if err != nil {
			val = param.Val
		}
		if val == param.Val && restored == nil {
			continue
		}
		if restored == nil {
			restored = params.AppendTo(nil)
		}
		restored[i].Val = val
	}
	if restored == nil {
		return params
	}
	return match.ParamsOf(restored...)
}

func decodedMatchPath(path string) string {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return decoded
}

func dispatchPath(req *http.Request) (string, bool) {
	return dispatchPathFromContext(req.Context())
}

func dispatchDecodeParams(req *http.Request) bool {
	decode, _ := decodeParamsFromContext(req.Context())
	return decode
}

// Params returns the parameters captured while matching req.
//
// The returned value is empty when the request did not match a parameterized
// host, subrouter, or route. If the same parameter name is captured at multiple
// levels, the more specific match wins: route parameters override subrouter
// parameters, and subrouter parameters override host parameters.
func Params(req *http.Request) RequestParams {
	params, _ := paramsFromContext(req.Context())
	return params
}

func paramsFromContext(ctx context.Context) (match.Params, bool) {
	for {
		switch current := ctx.(type) {
		case *requestStateContext:
			if current.flags&requestStateParams != 0 {
				return current.params, true
			}
			ctx = current.Context
			continue
		default:
			params, ok := ctx.Value(requestParamsKey).(RequestParams)
			return params, ok
		}
	}
}

func dispatchPathFromContext(ctx context.Context) (string, bool) {
	for {
		switch current := ctx.(type) {
		case *requestStateContext:
			if current.flags&requestStateDispatchPath != 0 {
				return current.path, true
			}
			ctx = current.Context
			continue
		default:
			path, ok := ctx.Value(requestDispatchKey).(string)
			return path, ok
		}
	}
}

func decodeParamsFromContext(ctx context.Context) (bool, bool) {
	for {
		switch current := ctx.(type) {
		case *requestStateContext:
			if current.flags&requestStateDecodeParams != 0 {
				return current.decodeParams, true
			}
			ctx = current.Context
			continue
		default:
			decode, ok := ctx.Value(requestDecodeParamsKey).(bool)
			return decode, ok
		}
	}
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

func requestPathValuesMatch(req *http.Request, params match.Params, enabled bool) bool {
	if !enabled {
		return true
	}
	return pathValuesEqual(req, params)
}

func pathValuesEqual(req *http.Request, params match.Params) bool {
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		if req.PathValue(param.Key) != param.Val {
			return false
		}
	}
	return true
}

func setPathValues(req *http.Request, params match.Params) {
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		req.SetPathValue(param.Key, param.Val)
	}
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
