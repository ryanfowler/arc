// Package arc provides a small, net/http-compatible router built on
// github.com/ryanfowler/match.
//
// Arc focuses on request dispatch, middleware, subrouters, host routing, and
// route parameters. It does not wrap response writing, request binding,
// rendering, logging, or other framework concerns.
//
// Route, subrouter, and host patterns use match's route grammar: {name}
// captures one non-empty segment and {*name} captures the non-empty remainder
// of a path. Captured parameters are stored on the request context and can be
// read with Params or Param. Use Router.SetRequestPathValues to also mirror
// them to http.Request.PathValue.
//
// Arc normally matches paths using req.URL.Path as parsed by net/http. When
// req.URL.RawPath preserves an escaped slash, Arc matches req.URL.EscapedPath()
// so the escaped slash stays inside its segment and decodes captured params
// before exposing them. It does not perform net/http.ServeMux path cleaning
// redirects for dot segments or repeated slashes. GET routes handle HEAD
// requests by default when no explicit HEAD or any-method route matches; use
// Router.SetImplicitHead to disable that behavior.
package arc
