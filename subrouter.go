package arc

import (
	"net/http"
	"strings"
)

// SubRouter registers and returns a child router mounted at pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Parameters
// captured by the mount pattern are available to child handlers.
//
// The child receives the remaining path after the mount point. For example, a
// child mounted at /api receives /users for a request to /api/users, while both
// /api and /api/ are dispatched to the child's / route.
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
		handler:    compose(http.HandlerFunc(child.ServeHTTP), r.middleware),
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
