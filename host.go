package arc

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
// Host patterns are DNS-label patterns matched against the whole normalized
// request host. For example, "api.example.com" matches one literal host and
// "{tenant}.example.com" captures exactly one DNS label before ".example.com"
// as "tenant". Host parameters can occupy an entire label or appear with
// literal text around them; "api-{region}.example.com" captures "us-west" from
// "api-us-west.example.com". Each host label can contain at most one parameter.
// A catch-all parameter such as "{*subdomain}.example.com" captures one or more
// leading labels and must appear in the leftmost label.
//
// Literal host labels are matched case-insensitively and are more specific than
// parameter labels. If "api.example.com" and "{tenant}.example.com" are both
// registered, "api.example.com" handles that host. Finite host patterns are
// more specific than catch-all host patterns. Overlapping host patterns with no
// deterministic winner are rejected as ambiguous.
//
// Request hosts are normalized before matching: trailing dots are ignored, IDNs
// are normalized to punycode, a numeric port in Request.Host is ignored, and
// brackets around colon-form hosts are ignored. Host patterns themselves must
// not include a port. IPv6 literals are matched as ordinary single-label hosts,
// so a pattern such as "{host}" can capture "::1".
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
// Registration errors include ErrInvalidHostPattern, invalid parameter syntax,
// duplicate parameter names within the pattern, duplicate host patterns, and
// ambiguous host patterns that could match the same requests.
func (r *Router) TryHost(pattern string) (*Router, error) {
	host, err := normalizeHostPattern(pattern)
	if err != nil {
		return nil, err
	}

	child := newChildRouter(r)
	if err := r.hostRoutes.tryInsertNormalized(host, child); err != nil {
		return nil, err
	}
	r.hasHosts = true
	return child.router, nil
}
