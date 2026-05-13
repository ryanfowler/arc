package arc

// Host registers and returns a child router that matches requests for pattern.
//
// Host patterns use the github.com/ryanfowler/match grammar, for example
// "api.example.com" or "{tenant}.example.com". Request hosts are matched
// case-insensitively, and a port in Request.Host is ignored.
//
// Parameters captured by the host pattern are available to handlers registered
// on the returned router. If no host pattern matches, dispatch falls through to
// the parent router's subrouters and routes.
//
// Middleware already registered on the parent wraps the host router.
// Middleware added to the returned router applies only inside that host router.
func (r *Router) Host(pattern string) *Router {
	child := newChildRouter(r)
	if err := r.hostRoutes.TryInsert(normalizeHost(pattern), child); err != nil {
		panic(err)
	}
	r.hasHosts = true
	return child.router
}
