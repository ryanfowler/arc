package arc

import "strings"

// SubRouter registers and returns a child router mounted at pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Parameters
// captured by the mount pattern are available to child handlers.
//
// The child matches against the remaining path after the mount point. For
// example, a child mounted at /api matches /users for a request to /api/users,
// while both /api and /api/ are dispatched to the child's / route. The request
// URL is not rewritten; middleware and handlers still see the original
// req.URL.Path.
//
// Middleware already registered on the parent wraps the child router.
// Middleware added to the child applies only inside the child router.
func (r *Router) SubRouter(pattern string) *Router {
	child := New(
		WithNotFound(r.notFound),
		WithMethodNotAllowed(r.methodNotAllowed),
	)

	sub := &subRouter{
		router:     child,
		handler:    compose(routerHandler{router: child}, r.middleware),
		middleware: r.middleware,
	}

	if err := r.subExact.TryInsert(cleanMountPattern(pattern), sub); err != nil {
		panic(err)
	}
	if slashPattern := slashMountPattern(pattern); slashPattern != "" {
		if err := r.subExact.TryInsert(slashPattern, sub); err != nil {
			panic(err)
		}
	}
	if err := r.subPrefix.TryInsert(catchAllMountPattern(pattern), sub); err != nil {
		panic(err)
	}

	r.hasSubRouters = true
	return child
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

func catchAllMountPattern(pattern string) string {
	pattern = cleanMountPattern(pattern)
	if pattern == "/" {
		return "/{" + "*" + subRouterRestParam + "}"
	}
	return pattern + "/{" + "*" + subRouterRestParam + "}"
}

func slashMountPattern(pattern string) string {
	pattern = cleanMountPattern(pattern)
	if pattern == "/" {
		return ""
	}
	return pattern + "/"
}
