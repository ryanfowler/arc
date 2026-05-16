// Package arc helps Go applications route net/http requests without adopting a
// larger web framework.
//
// The main type is Router. Create one during application startup, register
// routes and middleware on it, then pass it to http.ListenAndServe or
// http.Server:
//
//	r := arc.New()
//
//	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
//		id := arc.Param(req, "id")
//		fmt.Fprintln(w, id)
//	})
//
//	log.Fatal(http.ListenAndServe(":8080", r))
//
// Handlers are ordinary http.Handler and http.HandlerFunc values. Middleware is
// the standard net/http wrapper shape, so existing middleware can usually be
// used directly. Arc focuses on dispatch: method routing, route parameters,
// middleware groups, subrouters, mounted handlers, host routing, and fallback
// handlers. It does not provide response rendering, request binding, logging,
// validation, or other framework features.
//
// Route, subrouter, mount, and host patterns use the
// github.com/ryanfowler/match grammar. In paths, {name} captures one non-empty
// segment and {*name} captures the non-empty remainder of the path. Captured
// parameters are stored on the request and can be read with Params or Param.
// Use Router.SetRequestPathValues to also expose them through
// http.Request.PathValue.
//
// Use SubRouter to group a section of an application behind a shared path
// prefix, Mount to attach an existing http.Handler below a path, and Host to
// dispatch different domains or subdomains to different routers.
//
// Dispatch checks host routers first, then subrouters and mounted handlers, and
// then routes registered directly on the router. When a subrouter or mounted
// handler matches a prefix, it owns the request, including fallback handling;
// parent routes below that prefix are shadowed.
//
// Arc normally matches paths using req.URL.Path as parsed by net/http. When
// req.URL.RawPath preserves an escaped slash, Arc matches an internal decoded
// path where the escaped slash stays inside its segment, then restores captured
// parameters before exposing them. It does not perform net/http.ServeMux path
// cleaning redirects for dot segments or repeated slashes. GET routes handle
// HEAD requests by default when no explicit HEAD or any-method route matches;
// use Router.SetImplicitHead to disable that behavior.
package arc
