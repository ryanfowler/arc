package arc

import (
	"strings"

	"github.com/ryanfowler/match"
)

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

	if err := r.subMounts.TryInsert(pattern, sub); err != nil {
		panic(err)
	}

	r.hasSubRouters = true
	return child
}

type mountMatcher struct {
	exact    match.Router[*subRouter]
	prefixes match.Router[*subRouter]
}

func (m *mountMatcher) TryInsert(pattern string, sub *subRouter) error {
	pattern = cleanMountPattern(pattern)
	if err := m.exact.TryInsert(pattern, sub); err != nil {
		return err
	}
	if slashPattern := slashMountPattern(pattern); slashPattern != "" {
		if err := m.exact.TryInsert(slashPattern, sub); err != nil {
			return err
		}
	}
	return m.prefixes.TryInsert(pattern, sub)
}

func (m *mountMatcher) Match(path string) (*subRouter, string, match.Params, bool) {
	if sub, params, ok := m.exact.Match(path); ok {
		return sub, "/", params, true
	}

	for i := len(path) - 1; i > 0; i-- {
		if path[i] != '/' {
			continue
		}
		if sub, params, ok := m.prefixes.Match(path[:i]); ok {
			return sub, remainingMountPath(path[i+1:]), params, true
		}
	}

	if path != "" && path != "/" {
		if sub, params, ok := m.prefixes.Match("/"); ok {
			return sub, path, params, true
		}
	}

	return nil, "", match.Params{}, false
}

func remainingMountPath(rest string) string {
	if rest == "" {
		return "/"
	}
	if strings.HasPrefix(rest, "/") {
		return rest
	}
	return "/" + rest
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

func slashMountPattern(pattern string) string {
	pattern = cleanMountPattern(pattern)
	if pattern == "/" {
		return ""
	}
	return pattern + "/"
}
