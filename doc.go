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
// read with Params or Param.
package arc
