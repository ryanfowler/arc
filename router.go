package arc

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/ryanfowler/match"
)

const (
	escapedSlashMarker = byte(0)
	escapedSlashCode   = byte('s')
)

// ErrDuplicateParamName reports a single registered pattern that captures the
// same parameter name more than once.
//
// A duplicate name is invalid even when the captures come from different
// segments or when one capture is a catch-all parameter.
var ErrDuplicateParamName = fmt.Errorf("%w: duplicate parameter names are not allowed within one pattern", match.ErrInvalidParam)

// ErrInvalidPathPattern reports a route, subrouter, or mount path pattern that
// is not an absolute HTTP path.
//
// Path patterns registered with [Router.Handle], [Router.HandleAll],
// [Router.SubRouter], and [Router.Mount] must begin with "/". SubRouter and
// Mount treat an empty pattern as "/"; route registrations do not.
var ErrInvalidPathPattern = errors.New("path patterns must begin with /")

// Middleware wraps an HTTP handler on a [Router].
//
// Middleware uses the same shape as standard net/http middleware. If a router
// uses middleware a, b, and c, requests flow through:
//
//	a -> b -> c -> matched handler or fallback handler
type Middleware func(http.Handler) http.Handler

// Router is an [http.Handler] that dispatches application requests by host,
// path, and method.
//
// Build a router during startup by configuring fallback handlers and
// registering middleware, host routers, subrouters, mounted handlers, and
// routes. After it is built, pass it to [http.Server] or
// [http.ListenAndServe].
//
// A Router is safe for concurrent serving after registration is complete. The
// registration and configuration methods are not safe to call concurrently with
// ServeHTTP or with other registration and configuration methods.
type Router struct {
	pathRoutes             match.Router[*pathEntry]
	pathEntries            map[string]*pathEntry
	pathPatterns           []string
	hostRoutes             match.Router[*childRouter]
	hasDynamicPathPatterns bool
	hasHosts               bool
	hasRoutes              bool
	hasSubRouters          bool

	middleware []Middleware

	notFound                http.Handler
	methodNotAllowed        http.Handler
	notFoundHandler         http.Handler
	methodNotAllowedHandler http.Handler

	strictSlash   bool
	implicitHead  bool
	patternPrefix string
}

type pathEntry struct {
	methods *routeMethods
	child   *childRouter
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
	get               *route
	post              *route
	put               *route
	patch             *route
	delete            *route
	head              *route
	options           *route
	anyRoute          *route
	allow             string
	allowImplicitHead string
	pattern           string
}

type childRouter struct {
	router     *Router
	handler    http.Handler
	middleware []Middleware
	mounted    bool
	pattern    string
}

// New creates a [Router] with defaults suitable for a typical net/http
// application.
//
// By default:
//
//   - unmatched requests use [http.NotFoundHandler];
//   - paths registered for a different method receive 405 Method Not Allowed;
//   - GET routes handle HEAD requests unless an explicit HEAD or any-method
//     route matches.
//
// Child routers and host routers copy the parent router's current settings when
// they are created.
func New() *Router {
	r := &Router{
		pathEntries:      make(map[string]*pathEntry),
		notFound:         http.NotFoundHandler(),
		methodNotAllowed: http.HandlerFunc(defaultMethodNotAllowed),
		strictSlash:      true,
		implicitHead:     true,
	}
	r.compileFallbacks()
	return r
}

// SetNotFound sets the handler used when no host router, subrouter, mounted
// handler, or route matches a request.
//
// The handler runs through the router's current middleware stack.
//
// Passing nil leaves the router's existing not-found handler unchanged.
func (r *Router) SetNotFound(h http.Handler) {
	if h != nil {
		r.notFound = h
		r.compileFallbacks()
	}
}

// SetMethodNotAllowed sets the handler used when a request path matches a route
// pattern, but the request method was not registered for that pattern.
//
// Arc sets the Allow header before calling the handler. The handler runs
// through the router's current middleware stack.
//
// Passing nil leaves the router's existing method-not-allowed handler
// unchanged.
func (r *Router) SetMethodNotAllowed(h http.Handler) {
	if h != nil {
		r.methodNotAllowed = h
		r.compileFallbacks()
	}
}

// SetImplicitHead controls whether HEAD requests may use GET routes when no
// explicit HEAD or any-method route matches the same path.
//
// Implicit HEAD matching is enabled by default. Explicit HEAD and any-method
// routes take precedence when present.
func (r *Router) SetImplicitHead(enabled bool) {
	r.implicitHead = enabled
}

// SetStrictSlash controls whether route matching treats a trailing slash as
// significant.
//
// Strict slash matching is enabled by default. When disabled, a request path
// ending in "/" may match a route registered without that final slash:
//
//	r.SetStrictSlash(false)
//	r.Get("/users/{id}", getUser) // matches /users/42 and /users/42/
//
// Exact route matches still take precedence.
//
// Subrouters and host routers copy this setting when they are created. Later
// changes on the parent do not affect existing children.
func (r *Router) SetStrictSlash(strict bool) {
	r.strictSlash = strict
}

func defaultMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// Use appends middleware to the router.
//
// Middleware applies only to routes, subrouters, host routers, and mounted
// handlers registered after the call to Use. Fallback handlers use the router's
// current middleware stack. This lets applications build separate sections of a
// router with different middleware stacks:
//
//	r.Get("/healthz", health) // no auth middleware
//
//	r.Use(requireAuth)
//	r.Get("/account", account) // uses requireAuth
//
// Middleware is executed in the order it is added. Use panics if any middleware
// is nil.
func (r *Router) Use(mw ...Middleware) {
	for _, m := range mw {
		if m == nil {
			panic("arc: nil middleware")
		}
		r.middleware = append(r.middleware, m)
	}
	r.compileFallbacks()
}

// ServeHTTP dispatches req to the best matching host router, route, subrouter,
// or mounted handler.
//
// Dispatch checks host routers first. Inside a host or ordinary router, routes,
// subrouters, and mounted handlers share one path matcher. The most specific
// path wins, so a direct route can handle a path below a subrouter or mounted
// prefix; other paths below that prefix are still owned by the child, including
// not-found and method-not-allowed handling.
//
// Route and subrouter matching uses req.URL.Path unless req.URL.RawPath
// preserves an escaped slash. In that case, Arc matches an internal decoded
// path where the escaped slash stays inside its segment and restores captured
// parameters before exposing them through [http.Request.PathValue]. Arc does
// not perform net/http.ServeMux path cleaning redirects.
//
// ServeHTTP satisfies [http.Handler]. It should usually be called by net/http
// rather than directly.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	decodeParams := false
	if hasEscapedSlash(req.URL.RawPath) {
		path, decodeParams = escapedSlashMatchPath(req)
	}
	r.serve(w, req, path, match.Params{}, decodeParams)
}

func (r *Router) serve(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) {
	if r.serveHost(w, req, path, params, decodeParams) {
		return
	}
	if r.servePath(w, req, path, params, decodeParams) {
		return
	}

	r.notFoundHandler.ServeHTTP(w, requestForHandler(req, params, ""))
}

func (r *Router) serveHost(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) bool {
	if !r.hasHosts {
		return false
	}

	host := normalizeRequestHost(req.Host)
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

func (r *Router) servePath(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) bool {
	var entry *pathEntry
	var pathParams match.Params
	var ok bool

	if r.hasRoutes {
		entry, pathParams, ok = r.pathRoutes.Match(path)
		if ok && entry.methods != nil {
			if decodeParams {
				pathParams = restoreParams(pathParams)
			}
			r.serveRouteMethods(w, req, entry.methods, mergeParams(params, pathParams))
			return true
		}

		if relaxedPath, relaxed := r.relaxedSlashPath(path); relaxed {
			relaxedEntry, relaxedParams, relaxedOK := r.pathRoutes.Match(relaxedPath)
			if relaxedOK && relaxedEntry.methods != nil && !routePatternEndsInSlash(relaxedEntry.methods.pattern) {
				if decodeParams {
					relaxedParams = restoreParams(relaxedParams)
				}
				r.serveRouteMethods(w, req, relaxedEntry.methods, mergeParams(params, relaxedParams))
				return true
			}
		}
	}

	if ok && entry.child != nil {
		if decodeParams {
			pathParams = restoreParams(pathParams)
		}
		entry.child.serve(w, req, "/", mergeParams(params, pathParams), decodeParams)
		return true
	}

	if !r.hasSubRouters {
		return false
	}

	child, childPath, childParams, childOK := r.matchChildPrefix(path)
	if !childOK {
		return false
	}
	if decodeParams {
		childParams = restoreParams(childParams)
	}
	child.serve(w, req, childPath, mergeParams(params, childParams), decodeParams)
	return true
}

func (r *Router) matchChildPrefix(path string) (*childRouter, string, match.Params, bool) {
	mount, ok := r.pathRoutes.MatchPrefix(path)
	if ok && mount.Value.child != nil {
		return mount.Value.child, mount.Rest, mount.Params, true
	}

	for end := len(path); ; {
		slash := strings.LastIndexByte(path[:end], '/')
		if slash < 0 {
			return nil, "", match.Params{}, false
		}

		prefix := path[:slash]
		if slash == 0 {
			prefix = "/"
		}

		entry, params, ok := r.pathRoutes.Match(prefix)
		if ok && entry.child != nil {
			return entry.child, childPrefixRest(path, prefix), params, true
		}

		if slash == 0 {
			return nil, "", match.Params{}, false
		}
		end = slash
	}
}

func childPrefixRest(path, prefix string) string {
	if prefix == "/" {
		return remainingChildPath(path, 1)
	}
	if len(prefix) >= len(path) {
		return "/"
	}
	if path[len(prefix)] != '/' {
		return "/"
	}
	return remainingChildPath(path, len(prefix)+1)
}

func remainingChildPath(path string, index int) string {
	if index < 0 || index > len(path) || index == len(path) {
		return "/"
	}
	if path[index] == '/' {
		if index == 1 && len(path) > 1 && path[0] == '/' {
			return "/" + path[index+1:]
		}
		return path[index:]
	}
	if index == 0 {
		return path
	}
	return path[index-1:]
}

func (r *Router) serveRouteMethods(w http.ResponseWriter, req *http.Request, methods *routeMethods, params match.Params) {
	route := methods.routeFor(req.Method, r.implicitHead)
	if route == nil {
		w.Header().Set("Allow", methods.allowHeader(r.implicitHead))
		r.methodNotAllowedHandler.ServeHTTP(w, requestForHandler(req, params, methods.pattern))
		return
	}

	route.handler.ServeHTTP(w, requestForHandler(req, params, route.pattern))
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
	return insertRouteRegistration(r, reg)
}

func insertRouteRegistration(r *Router, reg routeRegistration) error {
	if err := validateUniqueParamNames(reg.pattern); err != nil {
		return err
	}

	entry := r.pathEntries[reg.pattern]
	if entry == nil {
		if r.pathEntries == nil {
			r.pathEntries = make(map[string]*pathEntry)
		}
		entry = &pathEntry{}
		if err := r.pathRoutes.TryInsert(reg.pattern, entry); err != nil {
			return err
		}
		r.pathEntries[reg.pattern] = entry
		r.pathPatterns = append(r.pathPatterns, reg.pattern)
		if isDynamicMatchPattern(reg.pattern) {
			r.hasDynamicPathPatterns = true
		}
	}

	if entry.methods == nil {
		entry.methods = &routeMethods{pattern: reg.fullPattern}
	}

	if err := entry.methods.addRoute(reg); err != nil {
		return err
	}

	r.hasRoutes = true
	return nil
}

type childPathRegistration struct {
	pattern string
	child   *childRouter
}

type pendingChildPathEntry struct {
	pattern string
	child   *childRouter
	entry   *pathEntry
	new     bool
}

func (r *Router) insertChildPathEntries(regs []childPathRegistration) error {
	if r.canInsertStaticChildPathEntries(regs) {
		return r.insertStaticChildPathEntries(regs)
	}

	return r.insertTransactionalChildPathEntries(regs)
}

func (r *Router) canInsertStaticChildPathEntries(regs []childPathRegistration) bool {
	if r.hasDynamicPathPatterns {
		return false
	}
	for _, reg := range regs {
		if isDynamicMatchPattern(reg.pattern) {
			return false
		}
	}
	return true
}

// When the matcher contains only literal patterns, new literal patterns can
// only conflict by exact duplicate, so the static path can validate with
// pathEntries and commit directly. Dynamic patterns use the cloned matcher
// transaction below.
func (r *Router) insertStaticChildPathEntries(regs []childPathRegistration) error {
	seen := make(map[string]struct{}, len(regs))
	pending := make([]pendingChildPathEntry, 0, len(regs))

	for _, reg := range regs {
		if _, ok := seen[reg.pattern]; ok {
			return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
		}
		seen[reg.pattern] = struct{}{}

		entry := r.pathEntries[reg.pattern]
		if entry != nil {
			if entry.child != nil {
				return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
			}
			pending = append(pending, pendingChildPathEntry{
				pattern: reg.pattern,
				child:   reg.child,
				entry:   entry,
			})
			continue
		}

		pending = append(pending, pendingChildPathEntry{
			pattern: reg.pattern,
			entry: &pathEntry{
				child: reg.child,
			},
			new: true,
		})
	}

	if r.pathEntries == nil {
		r.pathEntries = make(map[string]*pathEntry, len(pending))
	}
	for _, entry := range pending {
		if entry.new {
			if err := r.pathRoutes.TryInsert(entry.pattern, entry.entry); err != nil {
				return err
			}
			r.pathEntries[entry.pattern] = entry.entry
			r.pathPatterns = append(r.pathPatterns, entry.pattern)
			continue
		}
		entry.entry.child = entry.child
	}

	return nil
}

func (r *Router) insertTransactionalChildPathEntries(regs []childPathRegistration) error {
	seen := make(map[string]struct{}, len(regs))
	pending := make([]pendingChildPathEntry, 0, len(regs))
	var nextRoutes match.Router[*pathEntry]
	routesCloned := false

	for _, reg := range regs {
		if _, ok := seen[reg.pattern]; ok {
			return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
		}
		seen[reg.pattern] = struct{}{}

		entry := r.pathEntries[reg.pattern]
		if entry != nil {
			if entry.child != nil {
				return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
			}
			pending = append(pending, pendingChildPathEntry{
				pattern: reg.pattern,
				child:   reg.child,
				entry:   entry,
			})
			continue
		}

		entry = &pathEntry{
			child: reg.child,
		}
		if !routesCloned {
			nextRoutes = r.pathRoutes.Clone()
			routesCloned = true
		}
		if err := nextRoutes.TryInsert(reg.pattern, entry); err != nil {
			return err
		}
		pending = append(pending, pendingChildPathEntry{
			pattern: reg.pattern,
			entry:   entry,
			new:     true,
		})
	}

	if routesCloned {
		r.pathRoutes = nextRoutes
	}
	if r.pathEntries == nil {
		r.pathEntries = make(map[string]*pathEntry, len(pending))
	}

	for _, entry := range pending {
		if entry.new {
			r.pathEntries[entry.pattern] = entry.entry
			r.pathPatterns = append(r.pathPatterns, entry.pattern)
			if isDynamicMatchPattern(entry.pattern) {
				r.hasDynamicPathPatterns = true
			}
			continue
		}
		entry.entry.child = entry.child
	}

	return nil
}

func isDynamicMatchPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "{}")
}

func (m *routeMethods) addRoute(reg routeRegistration) error {
	if reg.anyMethod {
		if m.anyRoute != nil {
			return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
		}
		m.anyRoute = reg.route
		return nil
	}

	if m.routeForMethod(reg.method) != nil {
		return &match.ConflictError{Route: reg.pattern, With: reg.pattern}
	}

	m.setRouteForMethod(reg.method, reg.route)
	m.add(reg.method)
	return nil
}

func (m *routeMethods) routeFor(method string, implicitHead bool) *route {
	if route := m.routeForMethod(method); route != nil {
		return route
	}
	if m.anyRoute != nil {
		return m.anyRoute
	}
	if method == http.MethodHead && implicitHead {
		return m.get
	}
	return nil
}

func (m *routeMethods) routeForMethod(method string) *route {
	switch method {
	case http.MethodGet:
		return m.get
	case http.MethodPost:
		return m.post
	case http.MethodPut:
		return m.put
	case http.MethodPatch:
		return m.patch
	case http.MethodDelete:
		return m.delete
	case http.MethodHead:
		return m.head
	case http.MethodOptions:
		return m.options
	default:
		return m.routes[method]
	}
}

func (m *routeMethods) setRouteForMethod(method string, rt *route) {
	switch method {
	case http.MethodGet:
		m.get = rt
	case http.MethodPost:
		m.post = rt
	case http.MethodPut:
		m.put = rt
	case http.MethodPatch:
		m.patch = rt
	case http.MethodDelete:
		m.delete = rt
	case http.MethodHead:
		m.head = rt
	case http.MethodOptions:
		m.options = rt
	default:
		if m.routes == nil {
			m.routes = make(map[string]*route)
		}
		m.routes[method] = rt
	}
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

func serveWithMiddleware(w http.ResponseWriter, req *http.Request, h http.Handler, middleware []Middleware) {
	if len(middleware) == 0 {
		h.ServeHTTP(w, req)
		return
	}
	// Mount dispatch state is carried on the call stack, so the final handler is
	// request-specific.
	compose(h, middleware).ServeHTTP(w, req)
}

func (r *Router) compileFallbacks() {
	r.notFoundHandler = compose(r.notFound, r.middleware)
	r.methodNotAllowedHandler = compose(r.methodNotAllowed, r.middleware)
}

func newChildRouter(parent *Router) *childRouter {
	r := New()
	r.SetNotFound(parent.notFound)
	r.SetMethodNotAllowed(parent.methodNotAllowed)
	r.SetStrictSlash(parent.strictSlash)
	r.SetImplicitHead(parent.implicitHead)
	r.patternPrefix = parent.patternPrefix
	r.middleware = slices.Clone(parent.middleware)
	r.compileFallbacks()
	return &childRouter{
		router: r,
	}
}

func (c *childRouter) serve(w http.ResponseWriter, req *http.Request, path string, params match.Params, decodeParams bool) {
	if params.Len() != 0 {
		setPathValues(req, params)
	}

	if c.mounted {
		req.Pattern = c.pattern
		mountPath := path
		rawMountPath := ""
		if decodeParams {
			rawMountPath = escapedSlashRawPath(mountPath)
			mountPath = decodedMatchPath(mountPath)
		}
		if len(c.middleware) == 0 {
			c.handler.ServeHTTP(w, requestWithURLPath(req, mountPath, rawMountPath))
			return
		}
		final := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c.handler.ServeHTTP(w, requestWithURLPath(req, mountPath, rawMountPath))
		})
		serveWithMiddleware(w, req, final, c.middleware)
		return
	}

	c.router.serve(w, req, path, params, decodeParams)
}

func normalizeRequestHost(host string) string {
	if isLowercaseASCIIHost(host) {
		return host
	}
	return strings.ToLower(normalizeHostAddress(hostWithoutPort(host)))
}

func isLowercaseASCIIHost(host string) bool {
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c >= 'A' && c <= 'Z' {
			return false
		}
		if c == ':' || c == '[' || c == ']' || c >= 0x80 {
			return false
		}
	}
	return true
}

func normalizeHostPattern(pattern string) string {
	return lowercasePatternLiterals(normalizeHostAddress(hostWithoutPort(pattern)))
}

func hostWithoutPort(host string) string {
	if host == "" {
		return ""
	}

	if i := strings.LastIndexByte(host, ':'); i > 0 && strings.IndexByte(host[:i], ':') == -1 {
		return host[:i]
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func normalizeHostAddress(host string) string {
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' && strings.IndexByte(host, ':') != -1 {
		host = host[1 : len(host)-1]
	}
	return host
}

func lowercasePatternLiterals(pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern))

	literalStart := 0
	for i := 0; i < len(pattern); {
		if pattern[i] == '{' {
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i += 2
				continue
			}

			end, err := findPatternParamEnd(pattern, i+1)
			if err == nil {
				b.WriteString(strings.ToLower(pattern[literalStart:i]))
				b.WriteString(pattern[i : end+1])
				i = end + 1
				literalStart = i
				continue
			}
		}

		i++
	}
	b.WriteString(strings.ToLower(pattern[literalStart:]))

	return b.String()
}

func requestForHandler(req *http.Request, params match.Params, pattern string) *http.Request {
	req.Pattern = pattern
	if params.Len() != 0 {
		setPathValues(req, params)
	}
	return req
}

func requestWithURLPath(req *http.Request, path, rawPath string) *http.Request {
	if req.URL.Path == path && (rawPath == "" || req.URL.RawPath == rawPath) {
		return req
	}

	next := new(http.Request)
	*next = *req
	url := *req.URL
	url.Path = path
	url.RawPath = rawPath
	next.URL = &url
	return next
}

func escapedSlashMatchPath(req *http.Request) (string, bool) {
	return markEscapedSlashes(req.URL.Path, req.URL.EscapedPath())
}

func hasEscapedSlash(path string) bool {
	for i := 0; i+2 < len(path); i++ {
		if path[i] == '%' && path[i+1] == '2' && (path[i+2] == 'f' || path[i+2] == 'F') {
			return true
		}
	}
	return false
}

func markEscapedSlashes(decoded, escaped string) (string, bool) {
	var b strings.Builder
	decodedStart := 0
	decodedIndex := 0
	marked := false
	escapeMarkers := false

	for i := 0; i < len(escaped); {
		if escaped[i] != '%' || i+2 >= len(escaped) {
			decodedIndex++
			i++
			continue
		}

		if escaped[i+1] == '0' && escaped[i+2] == '0' {
			escapeMarkers = true
		}
		if escaped[i+1] == '2' && (escaped[i+2] == 'f' || escaped[i+2] == 'F') {
			if !marked {
				b.Grow(len(decoded) + 1)
				marked = true
			}
			writeDecodedMatchChunk(&b, decoded[decodedStart:decodedIndex], escapeMarkers)
			b.WriteByte(escapedSlashMarker)
			b.WriteByte(escapedSlashCode)
			decodedIndex++
			decodedStart = decodedIndex
		} else {
			decodedIndex++
		}
		i += 3
	}

	if !marked {
		return decoded, false
	}
	writeDecodedMatchChunk(&b, decoded[decodedStart:], escapeMarkers)
	return b.String(), true
}

func writeDecodedMatchChunk(b *strings.Builder, s string, escapeMarkers bool) {
	if !escapeMarkers {
		b.WriteString(s)
		return
	}
	for i := 0; i < len(s); i++ {
		if s[i] == escapedSlashMarker {
			b.WriteByte(escapedSlashMarker)
			b.WriteByte(escapedSlashMarker)
		} else {
			b.WriteByte(s[i])
		}
	}
}

// normalizePercentEncodedPattern decodes percent-encoded bytes in literal
// pattern text. Parameter syntax is router grammar, so escapes inside params are
// left alone; decoded slashes use the internal marker to stay within a segment.
func normalizePercentEncodedPattern(pattern string) string {
	if strings.IndexByte(pattern, '%') < 0 {
		return pattern
	}

	var b strings.Builder
	b.Grow(len(pattern))
	inParam := false
	decoded := false
	for i := 0; i < len(pattern); {
		if inParam {
			b.WriteByte(pattern[i])
			if pattern[i] == '{' && i+1 < len(pattern) && pattern[i+1] == '{' {
				b.WriteByte(pattern[i+1])
				i += 2
				continue
			}
			if pattern[i] == '}' {
				if i+1 < len(pattern) && pattern[i+1] == '}' {
					b.WriteByte(pattern[i+1])
					i += 2
					continue
				}
				inParam = false
			}
			i++
			continue
		}

		switch pattern[i] {
		case '{':
			b.WriteByte(pattern[i])
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				b.WriteByte(pattern[i+1])
				i += 2
				continue
			}
			inParam = true
			i++
		case '}':
			b.WriteByte(pattern[i])
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				b.WriteByte(pattern[i+1])
				i += 2
				continue
			}
			i++
		case '%':
			if i+2 >= len(pattern) {
				b.WriteByte(pattern[i])
				i++
				continue
			}
			hi, ok := fromHex(pattern[i+1])
			if !ok {
				b.WriteByte(pattern[i])
				i++
				continue
			}
			lo, ok := fromHex(pattern[i+2])
			if !ok {
				b.WriteByte(pattern[i])
				i++
				continue
			}
			decoded = true
			writePatternByte(&b, hi<<4|lo)
			i += 3
		default:
			if pattern[i] == escapedSlashMarker {
				b.WriteByte(escapedSlashMarker)
				b.WriteByte(escapedSlashMarker)
			} else {
				b.WriteByte(pattern[i])
			}
			i++
		}
	}
	if !decoded {
		return pattern
	}
	return b.String()
}

func validateUniqueParamNames(pattern string) error {
	var seenNames [8]string
	seenCount := 0
	var seenMap map[string]struct{}
	paramsInSegment := 0

	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '/':
			paramsInSegment = 0
			i++
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i += 2
				continue
			}

			end, err := findPatternParamEnd(pattern, i+1)
			if err != nil {
				return err
			}
			name := unescapePatternParamName(pattern[i+1 : end])
			if name == "" {
				return match.ErrInvalidParam
			}

			paramsInSegment++
			if paramsInSegment > 1 {
				return match.ErrInvalidParamSegment
			}

			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return match.ErrInvalidParam
				}
				if end+1 != len(pattern) {
					return match.ErrInvalidCatchAll
				}
			}

			if seenMap != nil {
				if _, ok := seenMap[name]; ok {
					return ErrDuplicateParamName
				}
				seenMap[name] = struct{}{}
			} else {
				for j := 0; j < seenCount; j++ {
					if seenNames[j] == name {
						return ErrDuplicateParamName
					}
				}
				if seenCount < len(seenNames) {
					seenNames[seenCount] = name
					seenCount++
				} else {
					seenMap = make(map[string]struct{}, seenCount+1)
					for _, seenName := range seenNames {
						seenMap[seenName] = struct{}{}
					}
					seenMap[name] = struct{}{}
				}
			}
			i = end + 1
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i += 2
				continue
			}
			return match.ErrInvalidParam
		default:
			i++
		}
	}

	return nil
}

func validateHTTPPathPattern(pattern string) error {
	if pattern == "" || pattern[0] != '/' {
		return ErrInvalidPathPattern
	}
	return nil
}

func patternHasFinalCatchAll(pattern string) bool {
	finalCatchAll := false
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i += 2
				continue
			}
			end, err := findPatternParamEnd(pattern, i+1)
			if err != nil {
				return false
			}
			name := unescapePatternParamName(pattern[i+1 : end])
			finalCatchAll = len(name) > 1 && name[0] == '*' && end+1 == len(pattern)
			i = end + 1
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i += 2
				continue
			}
			return false
		default:
			i++
		}
	}
	return finalCatchAll
}

func findPatternParamEnd(pattern string, start int) (int, error) {
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
				continue
			}
			if i == start || pattern[i-1] == '*' {
				return 0, match.ErrInvalidParam
			}
			return i, nil
		case '/':
			return 0, match.ErrInvalidParam
		case '*':
			if i != start {
				return 0, match.ErrInvalidParam
			}
			if i+1 == len(pattern) || pattern[i+1] == '}' {
				return 0, match.ErrInvalidParam
			}
		}
	}

	return 0, match.ErrInvalidParam
}

func unescapePatternParamName(s string) string {
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
			var b strings.Builder
			b.Grow(len(s) - 1)
			b.WriteString(s[:i])
			for ; i < len(s); i++ {
				if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
					b.WriteByte(s[i])
					i++
					continue
				}
				b.WriteByte(s[i])
			}
			return b.String()
		}
	}

	return s
}

func writePatternByte(b *strings.Builder, c byte) {
	switch c {
	case '{':
		b.WriteString("{{")
	case '}':
		b.WriteString("}}")
	default:
		writeMatchByte(b, c)
	}
}

func writeMatchByte(b *strings.Builder, c byte) {
	switch c {
	case '/':
		b.WriteByte(escapedSlashMarker)
		b.WriteByte(escapedSlashCode)
	case escapedSlashMarker:
		b.WriteByte(escapedSlashMarker)
		b.WriteByte(escapedSlashMarker)
	default:
		b.WriteByte(c)
	}
}

func fromHex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func restoreEscapedSlash(path string) string {
	restored, ok := restoreEscapedSlashValue(path)
	if !ok {
		return path
	}
	return restored
}

func restoreEscapedSlashValue(path string) (string, bool) {
	first := strings.IndexByte(path, escapedSlashMarker)
	if first < 0 {
		return path, false
	}

	return restoreEscapedSlashFrom(path, first), true
}

func restoreEscapedSlashFrom(path string, first int) string {
	var b strings.Builder
	b.Grow(len(path))
	b.WriteString(path[:first])
	for i := first; i < len(path); i++ {
		if path[i] != escapedSlashMarker {
			b.WriteByte(path[i])
			continue
		}
		if i+1 >= len(path) {
			b.WriteByte(path[i])
			continue
		}
		i++
		switch path[i] {
		case escapedSlashCode:
			b.WriteByte('/')
		case escapedSlashMarker:
			b.WriteByte(escapedSlashMarker)
		default:
			b.WriteByte(escapedSlashMarker)
			b.WriteByte(path[i])
		}
	}
	return b.String()
}

func restoreParams(params match.Params) match.Params {
	switch params.Len() {
	case 0:
		return params
	case 1:
		param := params.At(0)
		val, ok := restoreEscapedSlashValue(param.Val)
		if !ok {
			return params
		}
		param.Val = val
		return match.ParamsOf(param)
	case 2:
		p0 := params.At(0)
		p1 := params.At(1)
		ok := restoreParamValue(&p0)
		ok = restoreParamValue(&p1) || ok
		if !ok {
			return params
		}
		return match.ParamsOf(p0, p1)
	case 3:
		p0 := params.At(0)
		p1 := params.At(1)
		p2 := params.At(2)
		ok := restoreParamValue(&p0)
		ok = restoreParamValue(&p1) || ok
		ok = restoreParamValue(&p2) || ok
		if !ok {
			return params
		}
		return match.ParamsOf(p0, p1, p2)
	case 4:
		p0 := params.At(0)
		p1 := params.At(1)
		p2 := params.At(2)
		p3 := params.At(3)
		ok := restoreParamValue(&p0)
		ok = restoreParamValue(&p1) || ok
		ok = restoreParamValue(&p2) || ok
		ok = restoreParamValue(&p3) || ok
		if !ok {
			return params
		}
		return match.ParamsOf(p0, p1, p2, p3)
	}

	var restored []match.Param
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		if restoreParamValue(&param) && restored == nil {
			restored = params.AppendTo(nil)
			restored[i] = param
			continue
		}
		if restored != nil {
			restored[i] = param
		}
	}
	if restored == nil {
		return params
	}

	return match.ParamsOf(restored...)
}

func restoreParamValue(param *match.Param) bool {
	val, ok := restoreEscapedSlashValue(param.Val)
	if !ok {
		return false
	}
	param.Val = val
	return true
}

func decodedMatchPath(path string) string {
	return restoreEscapedSlash(path)
}

func escapedSlashRawPath(path string) string {
	if strings.IndexByte(path, escapedSlashMarker) < 0 {
		return ""
	}

	var b strings.Builder
	b.Grow(len(path) + 2)
	chunkStart := 0
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '/':
			writeEscapedPathChunk(&b, path[chunkStart:i])
			b.WriteByte('/')
			chunkStart = i + 1
		case escapedSlashMarker:
			writeEscapedPathChunk(&b, path[chunkStart:i])
			if i+1 >= len(path) {
				b.WriteString("%00")
				chunkStart = i + 1
				continue
			}
			i++
			switch path[i] {
			case escapedSlashCode:
				b.WriteString("%2F")
			case escapedSlashMarker:
				b.WriteString("%00")
			default:
				b.WriteString("%00")
				b.WriteString(url.PathEscape(path[i : i+1]))
			}
			chunkStart = i + 1
		}
	}
	writeEscapedPathChunk(&b, path[chunkStart:])
	return b.String()
}

func writeEscapedPathChunk(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	b.WriteString(url.PathEscape(s))
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
