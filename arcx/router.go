package arcx

import (
	"net/http"

	"github.com/ryanfowler/arc"
)

// Handler handles a request with an arcx Context and returns an error for the
// router's configured error handler.
type Handler func(*Context) error

// Router wraps an arc.Router with higher-level handler conveniences.
type Router struct {
	base *arc.Router
	cfg  *Config
}

// New creates a Router backed by a new arc.Router.
func New(opts ...Option) *Router {
	return Wrap(arc.New(), opts...)
}

// Wrap creates a Router around base.
//
// If base is nil, Wrap creates a new arc.Router.
func Wrap(base *arc.Router, opts ...Option) *Router {
	if base == nil {
		base = arc.New()
	}
	return &Router{
		base: base,
		cfg:  newConfig(opts...),
	}
}

func wrapChild(base *arc.Router, cfg *Config) *Router {
	return &Router{
		base: base,
		cfg:  cfg,
	}
}

// Base returns the underlying arc.Router.
func (r *Router) Base() *arc.Router {
	return r.base
}

// ServeHTTP dispatches req through the underlying arc.Router.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.base.ServeHTTP(w, req)
}

// Handle registers h for method and pattern.
func (r *Router) Handle(method, pattern string, h Handler) {
	r.base.Handle(method, pattern, r.adapt(h))
}

// TryHandle registers h for method and pattern and returns registration errors.
func (r *Router) TryHandle(method, pattern string, h Handler) error {
	return r.base.TryHandle(method, pattern, r.adapt(h))
}

// HandleAll registers h for pattern and lets it handle any request method.
func (r *Router) HandleAll(pattern string, h Handler) {
	r.base.HandleAll(pattern, r.adapt(h))
}

// TryHandleAll registers h for pattern and lets it handle any request method.
func (r *Router) TryHandleAll(pattern string, h Handler) error {
	return r.base.TryHandleAll(pattern, r.adapt(h))
}

// Get registers h for GET requests matching pattern.
func (r *Router) Get(pattern string, h Handler) {
	r.Handle(http.MethodGet, pattern, h)
}

// Post registers h for POST requests matching pattern.
func (r *Router) Post(pattern string, h Handler) {
	r.Handle(http.MethodPost, pattern, h)
}

// Put registers h for PUT requests matching pattern.
func (r *Router) Put(pattern string, h Handler) {
	r.Handle(http.MethodPut, pattern, h)
}

// Patch registers h for PATCH requests matching pattern.
func (r *Router) Patch(pattern string, h Handler) {
	r.Handle(http.MethodPatch, pattern, h)
}

// Delete registers h for DELETE requests matching pattern.
func (r *Router) Delete(pattern string, h Handler) {
	r.Handle(http.MethodDelete, pattern, h)
}

// Head registers h for HEAD requests matching pattern.
func (r *Router) Head(pattern string, h Handler) {
	r.Handle(http.MethodHead, pattern, h)
}

// Options registers h for OPTIONS requests matching pattern.
func (r *Router) Options(pattern string, h Handler) {
	r.Handle(http.MethodOptions, pattern, h)
}

// Use appends middleware to the underlying router.
func (r *Router) Use(mw ...arc.Middleware) {
	r.base.Use(mw...)
}

// SubRouter registers and returns a child router mounted at pattern.
func (r *Router) SubRouter(pattern string) *Router {
	return wrapChild(r.base.SubRouter(pattern), r.cfg)
}

// TrySubRouter registers and returns a child router mounted at pattern.
func (r *Router) TrySubRouter(pattern string) (*Router, error) {
	child, err := r.base.TrySubRouter(pattern)
	if err != nil {
		return nil, err
	}
	return wrapChild(child, r.cfg), nil
}

// Host registers and returns a child router for requests whose host matches
// pattern.
func (r *Router) Host(pattern string) *Router {
	return wrapChild(r.base.Host(pattern), r.cfg)
}

// TryHost registers and returns a child router for requests whose host matches
// pattern.
func (r *Router) TryHost(pattern string) (*Router, error) {
	child, err := r.base.TryHost(pattern)
	if err != nil {
		return nil, err
	}
	return wrapChild(child, r.cfg), nil
}

// Mount registers h below pattern on the underlying router.
func (r *Router) Mount(pattern string, h http.Handler) {
	r.base.Mount(pattern, h)
}

// TryMount registers h below pattern on the underlying router.
func (r *Router) TryMount(pattern string, h http.Handler) error {
	return r.base.TryMount(pattern, h)
}

// SetNotFound sets the handler used when no route matches.
func (r *Router) SetNotFound(h Handler) {
	if h != nil {
		r.base.SetNotFound(r.adapt(h))
	}
}

// SetMethodNotAllowed sets the handler used when a path matches but the method
// does not.
func (r *Router) SetMethodNotAllowed(h Handler) {
	if h != nil {
		r.base.SetMethodNotAllowed(r.adapt(h))
	}
}

// SetStrictSlash controls whether route matching treats a trailing slash as
// significant.
func (r *Router) SetStrictSlash(strict bool) {
	r.base.SetStrictSlash(strict)
}

// SetImplicitHead controls whether HEAD requests may use GET routes.
func (r *Router) SetImplicitHead(enabled bool) {
	r.base.SetImplicitHead(enabled)
}

func (r *Router) adapt(h Handler) http.HandlerFunc {
	if h == nil {
		return nil
	}
	return func(w http.ResponseWriter, req *http.Request) {
		c := &Context{
			ResponseWriter: w,
			Request:        req,
			cfg:            r.cfg,
		}
		if err := h(c); err != nil {
			r.cfg.errorHandler(c, err)
		}
	}
}
