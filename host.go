package arc

import "github.com/ryanfowler/match"

// Host registers and returns a child router for requests whose host matches
// pattern.
//
// Use Host when one application serves different routes for different domains
// or subdomains:
//
//	api := r.Host("api.example.com")
//	api.Get("/users/{id}", getUser)
//
//	tenant := r.Host("{tenant}.example.com")
//	tenant.Get("/", tenantHome)
//
// Host patterns use Arc's parameter syntax. For example, "api.example.com"
// matches one literal host and "{tenant}.example.com" captures the variable
// text before ".example.com" as "tenant". Request hosts are matched
// case-insensitively, a port in Request.Host is ignored, and brackets around
// IPv6 literals are ignored.
//
// Parameters captured by the host pattern are available to handlers registered
// on the returned router through [http.Request.PathValue]. If no host pattern
// matches, dispatch falls through to the parent router's subrouters and routes.
//
// Middleware already registered on the parent is inherited by the host router.
// It runs after the host router selects its final route or fallback, so route
// and method-not-allowed middleware can read the final Request.Pattern.
// Middleware added to the returned router applies only inside that host router,
// including host-router fallback handlers. The returned router copies the
// parent's current strict slash, implicit HEAD, and fallback handler settings
// when it is created.
//
// Invalid, duplicate, or ambiguous host patterns panic. Use [Router.TryHost] to
// receive the registration error instead.
func (r *Router) Host(pattern string) *Router {
	child, err := r.TryHost(pattern)
	if err != nil {
		panic(err)
	}
	return child
}

// TryHost registers and returns a child router for requests whose host matches
// pattern, and returns registration errors.
//
// Registration errors include empty normalized hosts, invalid parameter syntax,
// duplicate parameter names within the pattern, duplicate host patterns, and
// ambiguous host patterns that could match the same requests.
func (r *Router) TryHost(pattern string) (*Router, error) {
	child := newChildRouter(r)
	matchPattern := normalizeHostPattern(pattern)
	if matchPattern == "" {
		return nil, match.ErrInvalidParam
	}
	if err := validateUniqueParamNames(matchPattern); err != nil {
		return nil, err
	}
	if err := r.hostRoutes.TryInsert(matchPattern, child); err != nil {
		return nil, err
	}
	r.hasHosts = true
	return child.router, nil
}
