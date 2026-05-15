package arc

// Host registers and returns a child router for requests whose host matches
// pattern.
//
// Use Host when one application serves different routes for different domains
// or subdomains. Host patterns use the github.com/ryanfowler/match grammar, for
// example "api.example.com" or "{tenant}.example.com". Request hosts are
// matched case-insensitively, a port in Request.Host is ignored, and brackets
// around IPv6 literals are ignored.
//
// Parameters captured by the host pattern are available to handlers registered
// on the returned router. If no host pattern matches, dispatch falls through to
// the parent router's subrouters and routes.
//
// Middleware already registered on the parent wraps the host router.
// Middleware added to the returned router applies only inside that host router.
// The returned router copies the parent's current strict slash, implicit HEAD,
// request path value, and fallback handler settings when it is created.
//
// Invalid, duplicate, or ambiguous host patterns panic with the error returned
// by match. Use HostErr to receive the registration error instead.
func (r *Router) Host(pattern string) *Router {
	child, err := r.HostErr(pattern)
	if err != nil {
		panic(err)
	}
	return child
}

// HostErr registers and returns a child router for requests whose host matches
// pattern, and returns registration errors.
//
// Host patterns use the github.com/ryanfowler/match grammar. Registration
// errors include invalid parameter syntax and host conflicts reported by match.
func (r *Router) HostErr(pattern string) (*Router, error) {
	child := newChildRouter(r)
	if err := r.hostRoutes.TryInsert(normalizeHost(pattern), child); err != nil {
		return nil, err
	}
	r.hasHosts = true
	return child.router, nil
}
